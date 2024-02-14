// Package resolve is a CoreDNS plugin that establishes a grpc endpoint listening for DnsResolver requests
// and reformats them into ordinary dns requests which in turn is sent to the ordinary CoreDNS endpoint.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metadata"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/rcode"
	"github.com/coredns/coredns/plugin/pkg/reuseport"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/miekg/dns"
	dnsresolverV1 "github.com/nlnwa/veidemann-api/go/dnsresolver/v1"
	"github.com/opentracing/opentracing-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

// Define log to be a logger with the plugin name in it. This way we can just use log.Info and
// friends to log.
var (
	log = clog.NewWithPlugin("resolve")
)

// Resolve is a plugin which converts requests from a DnsResolverServer (grpc) to an ordinary dns request.
type Resolve struct {
	Next plugin.Handler
	dnsresolverV1.UnimplementedDnsResolverServer

	*grpc.Server
	listenAddr net.Addr
	addr       string
}

// CollectionIdKey is used as context value key for collectionId
type CollectionIdKey struct{}
type ExecutionIdKey struct{}

// NewResolver returns a new instance of Resolve with the given address
func NewResolver(addr string) *Resolve {
	server := &Resolve{
		Server: grpc.NewServer(
			grpc.UnaryInterceptor(otgrpc.OpenTracingServerInterceptor(opentracing.GlobalTracer())),
		),
		addr: addr,
	}
	// initialize server API
	dnsresolverV1.RegisterDnsResolverServer(server, server)

	return server
}

// Resolve implements DnsResolverServer
func (e *Resolve) Resolve(ctx context.Context, request *dnsresolverV1.ResolveRequest) (*dnsresolverV1.ResolveReply, error) {
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn(request.GetHost()), dns.TypeA)
	req.SetEdns0(4096, false)

	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, errors.New("no peer in gRPC context")
	}

	a, ok := p.Addr.(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("no TCP peer in gRPC context: %v", p.Addr)
	}

	w := &gRPCresponse{localAddr: e.listenAddr, remoteAddr: a, Msg: req}

	ctx = context.WithValue(ctx, CollectionIdKey{}, request.GetCollectionRef().GetId())
	ctx = context.WithValue(ctx, ExecutionIdKey{}, request.GetExecutionId())
	// Usually this value is initialized by the server. When using the gRPC server
	// we need to initialize it ourselves.
	// See https://github.com/coredns/coredns/blob/8868454177bdd3e70e71bd52d3c0e38bcf0d77fd/core/dnsserver/server.go#L161
	ctx = context.WithValue(ctx, dnsserver.Key{}, &dnsserver.Server{
		Addr: e.addr,
	})
	ctx = context.WithValue(ctx, dnsserver.LoopKey{}, 0)
	// TODO
	//
	// When using metadata plugin to get the upstream proxy used by the forward plugin
	// the context is not initialized with metadata so we need to initialize
	// it ourselves.
	//
	// The metadata.ContextWithMetadata function is only exported for use by provider tests
	// so this is a bit of a HACK.
	//
	// It can be argued that the resolver plugin can be removed
	// and that veidemann-frontier should just use normal DNS resolution.
	// The downside of this is that it will not be able to cache
	// DNS resolution per collection or know which collection to write
	// DNS requests to. A possible solution is to just write all DNS resolutions
	// to a single "DNS" collection. This will simplify things and remove
	// the need for a dnsresolver API as well.
	ctx = metadata.ContextWithMetadata(ctx)

	if rc, err := plugin.NextOrFailure(e.Name(), e.Next, ctx, w, req); err != nil {
		err = &UnresolvableError{
			Host:  request.Host,
			Rcode: rc,
			Err:   err,
		}
		log.Error(err)
		return nil, err
	}
	msg := w.Msg

	if msg.Rcode != dns.RcodeSuccess {
		// TODO return error in resolveReply.Error
		// TODO frontier must be refactored to do this
		err := &UnresolvableError{
			Host:  request.Host,
			Rcode: msg.Rcode,
		}
		log.Debug(err)
		return nil, err
	}

	res := &dnsresolverV1.ResolveReply{
		Host: request.Host,
		Port: request.Port,
	}
out:
	for _, answer := range msg.Answer {
		switch v := answer.(type) {
		case *dns.A:
			res.TextualIp = v.A.String()
			res.RawIp = v.A.To4()
			// prefer A records
			break out
		case *dns.AAAA:
			res.TextualIp = v.AAAA.String()
			res.RawIp = v.AAAA.To16()
		}
	}
	if res.TextualIp != "" {
		log.Debugf("Resolved %v into %v", request.Host, res)
		return res, nil
	}

	err := &UnresolvableError{
		Host:  request.Host,
		Rcode: msg.Rcode,
	}
	log.Debug(err)
	return nil, err
}

// Name implements the Handler interface.
func (s *Resolve) Name() string { return "resolve" }

// UnresolvableError is sent as error for unresolvable DNS lookup
type UnresolvableError struct {
	Host  string
	Rcode int
	Err   error
}

func (e *UnresolvableError) Error() string {
	return fmt.Sprintf("Unresolvable host: %s: %s: %v", e.Host, rcode.ToString(e.Rcode), e.Err)
}

func (e *Resolve) OnStartup() error {
	// start server
	go func() {
		ln, err := reuseport.Listen("tcp", e.addr)
		if err != nil {
			log.Errorf("failed to listen on %s: %v", e.addr, err)
		} else {
			log.Infof("Listening on grpc://%s", e.addr)
			e.listenAddr = ln.Addr()
			if err := e.Server.Serve(ln); err != nil {
				log.Error(err)
			}
		}
	}()
	return nil
}

func (e *Resolve) OnStop() error {
	e.Server.GracefulStop()
	return nil
}

type gRPCresponse struct {
	localAddr  net.Addr
	remoteAddr net.Addr
	Msg        *dns.Msg
}

// Write is the hack that makes this work. It does not actually write the message
// but returns the bytes we need to write in r. We can then pick this up in Query
// and write a proper protobuf back to the client.
func (r *gRPCresponse) Write(b []byte) (int, error) {
	r.Msg = new(dns.Msg)
	return len(b), r.Msg.Unpack(b)
}

// These methods implement the dns.ResponseWriter interface from Go DNS.
func (r *gRPCresponse) Close() error              { return nil }
func (r *gRPCresponse) TsigStatus() error         { return nil }
func (r *gRPCresponse) TsigTimersOnly(b bool)     {}
func (r *gRPCresponse) Hijack()                   {}
func (r *gRPCresponse) LocalAddr() net.Addr       { return r.localAddr }
func (r *gRPCresponse) RemoteAddr() net.Addr      { return r.remoteAddr }
func (r *gRPCresponse) WriteMsg(m *dns.Msg) error { r.Msg = m; return nil }
