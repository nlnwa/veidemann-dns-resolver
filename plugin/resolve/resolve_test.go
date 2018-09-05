package resolve

import (
	"bytes"
	"context"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
	vm "github.com/nlnwa/veidemann-dns-resolver/veidemann_api"
	"strconv"
	"strings"
	"testing"
)

func TestExample(t *testing.T) {
	s := dnstest.NewServer(func(w dns.ResponseWriter, r *dns.Msg) {
		ret := new(dns.Msg)
		ret.SetReply(r)
		ret.Answer = append(ret.Answer, test.A("example.org. IN A 127.0.0.1"))
		w.WriteMsg(ret)
	})
	defer s.Close()

	server := NewServer(8053)
	upstreamPort, err := strconv.Atoi(s.Addr[strings.LastIndex(s.Addr, ":")+1:])
	if err != nil {
		t.Error(err)
	}
	server.upstreamPort = upstreamPort
	server.OnStartup()
	defer server.OnFinalShutdown()

	ctx := context.TODO()

	reply, err := server.Resolve(ctx, &vm.ResolveRequest{Host: "example.org", Port: 80})
	if err != nil {
		t.Error(err)
	}

	if reply.Host != "example.org" {
		t.Errorf("Expected Host to be example.org, got: %s", reply.Host)
	}
	if reply.Port != 80 {
		t.Errorf("Expected Port to be 80, got: %d", reply.Port)
	}
	if reply.TextualIp != "127.0.0.1" {
		t.Errorf("Expected TextualIp to be 127.0.0.1, got: %s", reply.TextualIp)
	}
	expectedBytes := []byte{127, 0, 0, 1}
	if bytes.Compare(reply.RawIp, expectedBytes) != 0 {
		t.Errorf("Expected RawIp to be %v, got: %v", expectedBytes, reply.RawIp)
	}
}
