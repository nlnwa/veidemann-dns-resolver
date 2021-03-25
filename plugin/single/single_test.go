package single

import (
	"context"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/pkg/rcode"
	"github.com/coredns/coredns/plugin/test"
	"testing"

	"github.com/miekg/dns"
)

type testPlugin struct{}

func (s testPlugin) Name() string { return "sleep" }

func (s testPlugin) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Rcode = dns.RcodeServerFailure
	_ = w.WriteMsg(m)
	return 0, nil
}

func TestSingle(t *testing.T) {
	single := Single{
		Next: testPlugin{},
	}
	ctx := context.Background()

	w := dnstest.NewRecorder(&test.ResponseWriter{})
	m := new(dns.Msg)
	m.SetQuestion("aaa.example.com.", dns.TypeTXT)

	code, err := single.ServeDNS(ctx, w, m)
	if err != nil {
		t.Errorf("Got unexpected error: %v", err)
	}
	if code != dns.RcodeServerFailure {
		t.Errorf("Expected ServeDNS to return: %s, got: %s", rcode.ToString(dns.RcodeServerFailure), rcode.ToString(code))
	}
}
