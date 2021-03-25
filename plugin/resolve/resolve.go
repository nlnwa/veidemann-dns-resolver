// Package resolve is a CoreDNS plugin that establishes a grpc endpoint listening for DnsResolver requests
// and reformats them into ordinary dns requests which in turn is sent to the ordinary CoreDNS endpoint.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/rcode"
	"github.com/coredns/coredns/plugin/pkg/reuseport"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/miekg/dns"
	dnsresolverV1 "github.com/nlnwa/veidemann-api/go/dnsresolver/v1"
	"github.com/opentracing/opentracing-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"net"
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
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(request.GetHost()), dns.TypeA)
	msg.SetEdns0(4096, false)

	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, errors.New("no peer in gRPC context")
	}

	a, ok := p.Addr.(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("no TCP peer in gRPC context: %v", p.Addr)
	}

	w := &gRPCresponse{localAddr: e.listenAddr, remoteAddr: a, Msg: msg}

	ctx = context.WithValue(ctx, CollectionIdKey{}, request.GetCollectionRef().GetId())
	ctx = context.WithValue(ctx, dnsserver.Key{}, &dnsserver.Server{
		Addr: e.addr,
	})
	ctx = context.WithValue(ctx, dnsserver.LoopKey{}, 0)

	rc, err := e.Next.ServeDNS(ctx, w, msg)
	if err != nil || rc != dns.RcodeSuccess {
		log.Debugf("Unresolvable: %s: %s: %v", request.Host, rcode.ToString(rc), err)
		return nil, &UnresolvableError{request.Host}
	}
	for _, answer := range w.Msg.Answer {
		switch v := answer.(type) {
		case *dns.A:
			res := &dnsresolverV1.ResolveReply{
				Host:      request.Host,
				Port:      request.Port,
				TextualIp: v.A.String(),
				RawIp:     v.A.To4(),
			}
			log.Debugf("Resolved %v into %v", request, res)
			return res, nil
		case *dns.AAAA:
			res := &dnsresolverV1.ResolveReply{
				Host:      request.Host,
				Port:      request.Port,
				TextualIp: v.AAAA.String(),
				RawIp:     v.AAAA.To16(),
			}
			log.Debugf("Resolved %v into %v", request, res)
			return res, nil
		default:
			log.Debugf("Unhandled record: %v", v)
		}
	}
	log.Debugf("Unresolvable: %s: %s: empty answer or no A or AAAA resources in response record", request.Host, rcode.ToString(rc))
	return nil, &UnresolvableError{request.Host}
}

// UnresolvableError is sent as error for unresolvable DNS lookup
type UnresolvableError struct {
	Host string
}

func (e *UnresolvableError) Error() string {
	return fmt.Sprintf("Unresolvable host: %s", e.Host)
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
