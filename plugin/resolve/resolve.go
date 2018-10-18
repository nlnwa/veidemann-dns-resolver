// Package resolve is a CoreDNS plugin that establishes a grpc endpoint listening for DnsResolver requests
// and reformats them into ordinary dns requests which in turn is sent to the ordinary CoreDNS endpoint.
package resolve

import (
	"fmt"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"context"
	"github.com/miekg/dns"
	vm "github.com/nlnwa/veidemann-dns-resolver/veidemann_api"
	"google.golang.org/grpc"
	"net"
	"net/http"
	"google.golang.org/grpc/peer"
	"errors"
)

// Define log to be a logger with the plugin name in it. This way we can just use log.Info and
// friends to log.
var (
	log = clog.NewWithPlugin("resolve")
)

// Resolve is a plugin which converts requests from a DnsResolverServer (grpc) to an ordinary dns request.
type Resolve struct {
	Next       plugin.Handler
	Port       int
	ln         net.Listener
	listenAddr net.Addr
	lnSetup    bool
	mux        *http.ServeMux
	addr       string
	server     vm.DnsResolverServer
}

// NewResolver returns a new instance of Resolve with the given address
func NewResolver(port int) *Resolve {
	met := &Resolve{
		Port: port,
		addr: fmt.Sprintf("0.0.0.0:%d", port),
	}

	return met
}

// Resolve implements DnsResolverServer
func (e *Resolve) Resolve(ctx context.Context, request *vm.ResolveRequest) (*vm.ResolveReply, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(request.GetHost()), dns.TypeA)
	m.SetEdns0(4096, false)

	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, errors.New("no peer in gRPC context")
	}

	a, ok := p.Addr.(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("no TCP peer in gRPC context: %v", p.Addr)
	}

	w := &resolveResponse{localAddr: e.listenAddr, remoteAddr: a, Msg: m}

	rcode, err := e.Next.ServeDNS(ctx, w, m)

	if rcode == dns.RcodeSuccess && err == nil && len(w.Msg.Answer) > 0 {
		for _, answer := range w.Msg.Answer {
			switch v := answer.(type) {
			case *dns.A:
				res := &vm.ResolveReply{}
				res.Host = request.Host
				res.Port = request.Port
				res.TextualIp = v.A.String()
				res.RawIp = v.A.To4()
				log.Debugf("Resolved %v into %v", request, res)
				return res, nil
			case *dns.AAAA:
				res := &vm.ResolveReply{}
				res.Host = request.Host
				res.Port = request.Port
				res.TextualIp = v.AAAA.String()
				res.RawIp = v.AAAA.To16()
				log.Debugf("Resolved %v into %v", request, res)
				return res, nil
			default:
				log.Debugf("Unhandled record: %v", v)
			}
		}
	}

	log.Debugf("Unresolvable: %v. Result: rcode: %v, err: %v, record: %v\n\n", request.Host, rcode, err, w.Msg)

	return nil, &UnresolvableError{request.Host}
}

// UnresolvableError is sent as error for unresolvable DNS lookup
type UnresolvableError struct {
	Host string
}

func (e *UnresolvableError) Error() string {
	return fmt.Sprintf("Unresolvable host: %s", e.Host)
}

// OnStartup sets up the grpc endpoint.
func (e *Resolve) OnStartup() error {
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", e.Port))
	if err != nil {
		log.Errorf("failed to start resolve handler: %v", err)
	}

	e.ln = ln
	e.listenAddr = ln.Addr()
	e.lnSetup = true

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	vm.RegisterDnsResolverServer(grpcServer, e)

	go func() {
		log.Debugf("Resolve listening on port: %d", e.Port)
		grpcServer.Serve(ln)
	}()
	return nil
}

// OnRestart stops the listener on reload.
func (e *Resolve) OnRestart() error {
	if !e.lnSetup {
		return nil
	}

	e.ln.Close()
	e.lnSetup = false
	return nil
}

// OnFinalShutdown tears down the metrics listener on shutdown and restart.
func (e *Resolve) OnFinalShutdown() error {
	// We allow prometheus statements in multiple Server Blocks, but only the first
	// will open the listener, for the rest they are all nil; guard against that.
	if !e.lnSetup {
		return nil
	}

	e.lnSetup = false
	return e.ln.Close()
}

type resolveResponse struct {
	localAddr  net.Addr
	remoteAddr net.Addr
	Msg        *dns.Msg
}

// Write is the hack that makes this work. It does not actually write the message
// but returns the bytes we need to to write in r. We can then pick this up in Query
// and write a proper protobuf back to the client.
func (r *resolveResponse) Write(b []byte) (int, error) {
	r.Msg = new(dns.Msg)
	return len(b), r.Msg.Unpack(b)
}

// These methods implement the dns.ResponseWriter interface from Go DNS.
func (r *resolveResponse) Close() error              { return nil }
func (r *resolveResponse) TsigStatus() error         { return nil }
func (r *resolveResponse) TsigTimersOnly(b bool)     { return }
func (r *resolveResponse) Hijack()                   { return }
func (r *resolveResponse) LocalAddr() net.Addr       { return r.localAddr }
func (r *resolveResponse) RemoteAddr() net.Addr      { return r.remoteAddr }
func (r *resolveResponse) WriteMsg(m *dns.Msg) error { r.Msg = m; return nil }
