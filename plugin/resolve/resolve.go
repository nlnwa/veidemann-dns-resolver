// Package resolve is a CoreDNS plugin that establishes a grpc endpoint listening for DnsResolver requests
// and reformats them into ordinary dns requests wich in turn is sent to the ordinary CoreDNS endpoint.
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
	"strconv"
)

// Define log to be a logger with the plugin name in it. This way we can just use log.Info and
// friends to log.
var (
	log = clog.NewWithPlugin("resolve")
)

// Resolve is a plugin which converts requests from a DnsResolverServer (grpc) to an ordinary dns request.
type Resolve struct {
	Next         plugin.Handler
	Port         int
	ln           net.Listener
	lnSetup      bool
	mux          *http.ServeMux
	addr         string
	server       vm.DnsResolverServer
	upstreamPort int
}

// NewResolver returns a new instance of Resolve with the given address
func NewResolver(port int) *Resolve {
	met := &Resolve{
		Port:         port,
		addr:         fmt.Sprintf("0.0.0.0:%d", port),
		upstreamPort: 53,
	}

	return met
}

// Resolve implements DnsResolverServer
func (e *Resolve) Resolve(ctx context.Context, request *vm.ResolveRequest) (*vm.ResolveReply, error) {
	log.Debugf("Got request: %v", request)
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(request.GetHost()), dns.TypeA)
	m.SetEdns0(4096, false)
	in, err := dns.Exchange(m, "127.0.0.1:"+strconv.Itoa(e.upstreamPort))
	if err != nil {
		log.Infof("Failed resolving %s: %v", request.GetHost(), err)
		return nil, err
	}

	for _, answer := range in.Answer {
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
