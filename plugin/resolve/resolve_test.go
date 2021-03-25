package resolve

import (
	"bytes"
	"context"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
	dnsresolverV1 "github.com/nlnwa/veidemann-api/go/dnsresolver/v1"
	"google.golang.org/grpc/peer"
	"net"
	"testing"
)

func TestExample(t *testing.T) {
	server := NewResolver("8053")
	defer func() { _ = server.OnStop() }()
	a, _ := net.ResolveTCPAddr("tcp", "127.0.0.1")
	p := &peer.Peer{Addr: a}
	ctx := peer.NewContext(context.TODO(), p)

	server.Next = MsgHandler(test.A("example.org. IN A 127.0.0.1"))
	reply, err := server.Resolve(ctx, &dnsresolverV1.ResolveRequest{Host: "example.org", Port: 80})
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

// MsgHandler returns a Handler that adds answer to request and writes it to w.
func MsgHandler(answer dns.RR) test.Handler {
	return test.HandlerFunc(func(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
		reply := new(dns.Msg)
		reply.SetReply(r)
		reply.Answer = append(reply.Answer, answer)
		w.WriteMsg(reply)
		return reply.Rcode, nil
	})
}
