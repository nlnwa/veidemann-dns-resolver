package test

import (
	"context"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/rcode"
	"github.com/google/uuid"
	"github.com/miekg/dns"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/archivingcache"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/pkg/transport"
	"github.com/coredns/coredns/plugin/test"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/forward"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/pkg/serviceconnections"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/resolve"
)

var (
	cache     *archivingcache.Cache
	ls        *LogServiceMock
	cws       *ContentWriterMock
	serverCtx context.Context
	p         plugin.Handler
)

func reset() {
	if err := cache.Reset(); err != nil {
		panic(err)
	}
	cws.Reset()
	ls.Reset()
}

var tests = map[string]test.Case{
	"example.org": {
		Qname: "example.org",
		Qtype: dns.TypeA,
		Answer: []dns.RR{
			test.A("example.org.	3601	IN	A	10.0.0.1"),
		},
	},
	"example2.org": {
		Qname: "example2.org",
		Qtype: dns.TypeA,
		Answer: []dns.RR{
			test.A("example2.org.	3601	IN	A	10.0.0.2"),
		},
	},
}

func TestMain(m *testing.M) {
	// setup mock dns server
	s := dnstest.NewServer(func(w dns.ResponseWriter, r *dns.Msg) {
		msg := new(dns.Msg)

		qname := strings.TrimSuffix(r.Question[0].Name, ".")
		if c, ok := tests[qname]; !ok {
			msg.SetRcode(r, dns.RcodeNameError)
		} else {
			msg.SetReply(r)
			msg.Answer = c.Answer
		}

		w.WriteMsg(msg)
	})

	// setup
	serverCtx = context.WithValue(context.Background(), dnsserver.Key{}, &dnsserver.Server{
		Addr: new(test.ResponseWriter).LocalAddr().String(),
	})

	// setup forward plugin with server as proxy
	next := forward.New()
	next.SetProxy(forward.NewProxy(s.Addr, transport.DNS))

	// setup content writer service mock
	cws = NewContentWriterServerMock()
	var contentWriterAddr *net.TCPAddr
	if listener, err := net.Listen("tcp", "localhost:0"); err != nil {
		panic(err)
	} else {
		contentWriterAddr = listener.Addr().(*net.TCPAddr)
		go func() {
			if err := cws.Serve(listener); err != nil {
				panic(err)
			}
		}()
	}

	// setup log service mock
	ls = NewLogServiceMock()
	var logServiceAddr *net.TCPAddr
	if listener, err := net.Listen("tcp", "localhost:0"); err != nil {
		panic(err)
	} else {
		logServiceAddr = listener.Addr().(*net.TCPAddr)
		go func() {
			if err := ls.Serve(listener); err != nil {
				panic(err)
			}
		}()
	}

	// setup cache
	var err error
	cache, err = archivingcache.NewCache(10*time.Second, 1024)
	if err != nil {
		panic(err)
	}

	// setup content writer client
	cw := archivingcache.NewContentWriterClient(
		serviceconnections.WithHost("localhost"),
		serviceconnections.WithPort(contentWriterAddr.Port),
	)

	// setup log writer client
	lw := archivingcache.NewLogWriterClient(
		serviceconnections.WithHost("localhost"),
		serviceconnections.WithPort(logServiceAddr.Port),
	)

	// setup archivingcache plugin
	a := archivingcache.NewArchivingCache(cache, lw, cw)
	a.Next = next
	if err := a.OnStartup(); err != nil {
		panic(err)
	}
	p = a

	code := m.Run()

	_ = a.OnShutdown()
	_ = next.OnShutdown()
	_ = lw.Close()
	_ = cw.Close()
	ls.Close()
	cws.Close()
	s.Close()

	os.Exit(code)
}

func TestNonExistentDomain(t *testing.T) {
	defer reset()

	rec := dnstest.NewRecorder(new(test.ResponseWriter))
	req := new(dns.Msg)
	req.SetQuestion("bogus.org.", dns.TypeA)
	ctx := context.WithValue(serverCtx, resolve.CollectionIdKey{}, uuid.NewString())

	_, err := p.ServeDNS(ctx, rec, req)
	if err != nil {
		t.Error("WTF", err)
	}
	if rec.Rcode != dns.RcodeNameError {
		t.Errorf("Expected %s, got %s", rcode.ToString(dns.RcodeNameError), rcode.ToString(rec.Rcode))
	}
}

func TestConcurrentRequests(t *testing.T) {
	defer reset()

	var cases []test.Case
	for _, c := range tests {
		cases = append(cases, c)
	}

	concurrency := 50
	ch := make(chan *dns.Msg, concurrency)
	rec := NewChannelRecorder(ch)

	// run a number of tests concurrently
	for i := 0; i < concurrency; i++ {
		rec.Add(1)
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			j := i % len(cases)
			t.Parallel()

			req := cases[j].Msg()
			ctx := context.WithValue(serverCtx, resolve.CollectionIdKey{}, strconv.Itoa(j))

			rc, err := p.ServeDNS(ctx, rec, req)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if rc != dns.RcodeSuccess {
				t.Errorf("Unexpected response code: %d %s", rc, rcode.ToString(rc))
			}
		})
	}

	t.Run("assert", func(t *testing.T) {
		t.Parallel()

		rec.Wait()
		close(ch)

		count := 0

		for msg := range ch {
			if len(msg.Answer) == 0 {
				t.Errorf("Expected answer, got zero")
			}
			count++
		}
		// assert number of crawl logs written equal the number of tests
		if ls.Len() != len(tests) {
			t.Errorf("Expected %d crawl logs, got %d", len(tests), ls.Len())
		}
		if count != concurrency {
			t.Errorf("Expected %d messages, got: %d", concurrency, count)
		}
	})
}

// ChannelRecorder is a type of ResponseWriter that sends the message over a channel.
type ChannelRecorder struct {
	dns.ResponseWriter
	ch chan *dns.Msg
	sync.WaitGroup
}

// NewChannelRecorder makes and returns a new ChannelRecorder.
func NewChannelRecorder(ch chan *dns.Msg) *ChannelRecorder {
	return &ChannelRecorder{
		ResponseWriter: &test.ResponseWriter{},
		ch:             ch,
	}
}

// WriteMsg rr the status code and calls the
// underlying ResponseWriter's WriteMsg method.
func (r *ChannelRecorder) WriteMsg(res *dns.Msg) error {
	defer r.Done()
	r.ch <- res
	return r.ResponseWriter.WriteMsg(res)
}
