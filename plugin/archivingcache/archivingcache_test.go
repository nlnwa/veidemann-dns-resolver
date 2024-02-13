package archivingcache

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin/metadata"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/pkg/rcode"
	"github.com/coredns/coredns/plugin/test"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	contentwriterV1 "github.com/nlnwa/veidemann-api/go/contentwriter/v1"
	logV1 "github.com/nlnwa/veidemann-api/go/log/v1"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/pkg/serviceconnections"
	util "github.com/nlnwa/veidemann-dns-resolver/plugin/pkg/test"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/resolve"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	cache *Cache
	ls    *util.LogServiceMock
	cws   *util.ContentWriterMock
	lw    *LogWriterClient
	cw    *ContentWriterClient
)

func reset() {
	if err := cache.Reset(); err != nil {
		panic(err)
	}
	cws.Reset()
	ls.Reset()
}

func TestMain(m *testing.M) {
	// setup content writer service mock
	cws = util.NewContentWriterServerMock()
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
	ls = util.NewLogServiceMock()
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
	cache, err = NewCache(10*time.Second, 1024)
	if err != nil {
		panic(err)
	}

	// setup content writer client
	cw = NewContentWriterClient(
		serviceconnections.WithHost("localhost"),
		serviceconnections.WithPort(contentWriterAddr.Port),
	)

	// setup log writer client
	lw = NewLogWriterClient(
		serviceconnections.WithHost("localhost"),
		serviceconnections.WithPort(logServiceAddr.Port),
	)

	code := m.Run()

	_ = lw.Close()
	_ = cw.Close()
	ls.Close()
	cws.Close()

	os.Exit(code)
}

func TestNextError(t *testing.T) {
	defer reset()
	a := NewArchivingCache(cache, nil, nil)
	a.Next = test.ErrorHandler()

	req := new(dns.Msg).SetQuestion("bogus.org.", dns.TypeA)

	rec := dnstest.NewRecorder(new(test.ResponseWriter))

	// run
	_, err := a.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if rec.Rcode != dns.RcodeServerFailure {
		t.Errorf("Expected %d %s, got %d %s",
			dns.RcodeServerFailure,
			rcode.ToString(dns.RcodeServerFailure),
			rec.Rcode,
			rcode.ToString(rec.Rcode))
	}
}

func TestNonExistentDomain(t *testing.T) {
	defer reset()

	a := NewArchivingCache(cache, nil, nil)
	a.Next = codeHandler(dns.RcodeNameError)

	rec := dnstest.NewRecorder(new(test.ResponseWriter))
	req := new(dns.Msg).SetQuestion("bogus.org.", dns.TypeA)

	// run
	rc, err := a.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if rc > dns.RcodeSuccess {
		t.Errorf("Unpexpected response code: %d %s", rc, rcode.ToString(rc))
	}
	if rec.Rcode != dns.RcodeNameError {
		t.Errorf("Expected %d %s, got %d %s", dns.RcodeNameError, rcode.ToString(dns.RcodeNameError), rec.Rcode, rcode.ToString(rec.Rcode))
	}
}

func TestNoCache(t *testing.T) {
	defer reset()

	tests := map[string]test.Case{
		"example.org": {
			Qname: "example.org",
			Qtype: dns.TypeA,
			Answer: []dns.RR{
				test.A("example.org.	3601	IN	A	127.0.0.1"),
			},
		},
		"example2.org": {
			Qname: "example2.org",
			Qtype: dns.TypeA,
			Answer: []dns.RR{
				test.A("example2.org.	3601	IN	A	127.0.0.2"),
			},
		},
		"errxample.com": {
			Qname: "errxample.com",
			Qtype: dns.TypeA,
			Rcode: dns.RcodeServerFailure,
		},
	}

	a := NewArchivingCache(cache, nil, nil)
	a.Next = testHandler(tests, "")

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// setup
			rec := dnstest.NewRecorder(&test.ResponseWriter{})
			req := tt.Msg()
			ctx := context.Background()

			// run
			_, err := a.ServeDNS(ctx, rec, req)
			if err != tt.Error {
				t.Errorf("Expected %v, got %v", tt.Error, err)
			}
			if err := test.SortAndCheck(rec.Msg, tt); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestCache(t *testing.T) {
	defer reset()

	tests := map[string]test.Case{
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

	a := NewArchivingCache(cache, lw, cw)
	a.Next = testHandler(tests, "127.0.0.1")
	if err := a.OnStartup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = a.OnShutdown()
	}()

	i := 0
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// setup
			rec := dnstest.NewRecorder(&test.ResponseWriter{})
			ctx := context.WithValue(context.Background(), dnsserver.Key{}, &dnsserver.Server{
				Addr: "127.0.0.1",
			})
			ctx = metadata.ContextWithMetadata(ctx)
			ctx = context.WithValue(ctx, resolve.CollectionIdKey{}, name)
			// run first time
			req := tt.Msg()
			t.Logf("req iD: %d", req.Id)
			_, err := a.ServeDNS(ctx, rec, req)
			if err != tt.Error {
				t.Errorf("Expected %v, got %v", tt.Error, err)
			}
			if err := test.SortAndCheck(rec.Msg, tt); err != nil {
				t.Error(err)
			}
			if req.Id != rec.Msg.Id {
				t.Errorf("A: Expected %d got %d", req.Id, rec.Msg.Id)
			}
			// run second time
			req = tt.Msg()
			_, err = a.ServeDNS(ctx, rec, req)
			if err != tt.Error {
				t.Errorf("Expected %v, got %v", tt.Error, err)
			}
			if err := test.SortAndCheck(rec.Msg, tt); err != nil {
				t.Error(err)
			}
			if req.Id != rec.Msg.Id {
				t.Errorf("B: Expected %d got %d", req.Id, rec.Msg.Id)
			}

			assertCrawlLog(t, ctx, tt.Qname)
			assertRecord(t, ctx, tt.Qname, test.A(tt.Answer[0].String()))
		})
		i++
	}
	if cache.Len() != len(tests) {
		t.Errorf("Expected %d, got %d", len(tests), cache.Len())
	}
	if ls.Len() != len(tests) {
		t.Errorf("Expected %d, got %d", len(tests), cache.Len())
	}
}

func assertRecord(t *testing.T, ctx context.Context, qname string, answer *dns.A) {
	var addr string
	srv, ok := ctx.Value(dnsserver.Key{}).(*dnsserver.Server)
	if ok {
		addr = srv.Addr
	}
	if cws.Meta.IpAddress != addr {
		t.Errorf("Expected %s, got: %s", addr, cws.Meta.IpAddress)
	}

	targetUri := "dns:" + qname
	if cws.Meta.TargetUri != targetUri {
		t.Errorf("Expected %s, got: %s", targetUri, cws.Meta.TargetUri)
	}
	if cws.Meta.RecordMeta[0].Type != contentwriterV1.RecordType_RESOURCE {
		t.Errorf("Expected %d, got: %d", contentwriterV1.RecordType_RESOURCE, cws.Meta.RecordMeta[0].Type)
	}
	if cws.Meta.RecordMeta[0].RecordContentType != "text/dns" {
		t.Errorf("Expected text/dns, got: %s", cws.Meta.RecordMeta[0].RecordContentType)
	}
	if cws.Meta.RecordMeta[0].RecordNum != 0 {
		t.Errorf("Expected 0, got: %d", cws.Meta.RecordMeta[0].Size)
	}
	collectionId := ctx.Value(resolve.CollectionIdKey{}).(string)
	if cws.Meta.CollectionRef.GetId() != collectionId {
		t.Errorf("Expected %s, got: %v", collectionId, cws.Meta.GetCollectionRef().GetId())
	}

	// Validate ContentWriters Payload archive
	ts := time.Now().UTC()

	expectedPayload := fmt.Sprintf("%d%02d%02d%02d%02d%02d\n%s\n",
		ts.Year(), ts.Month(), ts.Day(),
		ts.Hour(), ts.Minute(), ts.Second(), answer)

	if !bytes.Equal(cws.Payload.Data, []byte(expectedPayload)) {
		t.Errorf("Expected '%s', got: '%s'", expectedPayload, cws.Payload.Data)
	}

}

func assertCrawlLog(t *testing.T, ctx context.Context, qname string) {
	var addr string
	srv, ok := ctx.Value(dnsserver.Key{}).(*dnsserver.Server)
	if ok {
		addr = srv.Addr
	}
	collectionId := ctx.Value(resolve.CollectionIdKey{}).(string)
	expected := &logV1.CrawlLog{
		RecordType:          "resource",
		PayloadDigest:       "pd",
		WarcId:              "WarcId:" + collectionId,
		RequestedUri:        "dns:" + qname,
		IpAddress:           addr,
		StatusCode:          1,
		BlockDigest:         "bd",
		DiscoveryPath:       "P",
		StorageRef:          "ref",
		ContentType:         "text/dns",
		CollectionFinalName: "cfn",
	}
	got := ls.CrawlLog
	if got == nil {
		got = new(logV1.CrawlLog)
	}
	expected.Size = got.Size
	expected.TimeStamp = got.TimeStamp
	expected.FetchTimeMs = got.FetchTimeMs
	expected.FetchTimeStamp = got.FetchTimeStamp

	assertEqualProtoMsg(t, expected, got)
}

func assertEqualProtoMsg(t *testing.T, expected proto.Message, got proto.Message) {
	// convert to json for comparison
	a, err := protojson.Marshal(expected)
	if err != nil {
		t.Error(err)
	}
	b, err := protojson.Marshal(got)
	if err != nil {
		t.Error(err)
	}
	if string(a) != string(b) {
		t.Errorf("\n\tExpected:\n%s,\n\tGot:\n%s", a, b)
	}
}

func testHandler(cases map[string]test.Case, addr string) test.HandlerFunc {
	return func(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
		var msg *dns.Msg
		state := &request.Request{
			Req: r,
			W:   w,
		}
		metadata.SetValueFunc(ctx, "forward/upstream", func() string {
			return addr
		})

		for _, c := range cases {
			if dns.Fqdn(c.Qname) == state.QName() {
				if c.Error != nil {
					return c.Rcode, c.Error
				}
				msg = c.Msg().SetRcode(r, c.Rcode)
				msg.Answer = c.Answer
				msg.Extra = c.Extra
				msg.Ns = c.Ns
				break
			}
		}

		if msg == nil {
			msg = new(dns.Msg).SetRcode(r, dns.RcodeNameError)
		}

		_ = w.WriteMsg(msg)
		return 0, nil
	}
}

func codeHandler(rcode int) test.HandlerFunc {
	return func(_ context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
		_ = w.WriteMsg(new(dns.Msg).SetRcode(r, rcode))
		return 0, nil
	}
}

func TestParseProxyAddress(t *testing.T) {
	tests := []struct {
		HostPortOrIp string
		Expect       string
	}{
		{
			HostPortOrIp: "8.8.8.8:53",
			Expect:       "8.8.8.8",
		},
		{
			HostPortOrIp: "8.8.8.8",
			Expect:       "8.8.8.8",
		},
		{
			HostPortOrIp: "njet",
			Expect:       "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.HostPortOrIp, func(t *testing.T) {
			got, _ := parseHostPortOrIP(tt.HostPortOrIp)
			if tt.Expect != got {
				t.Errorf("Expected \"%s\" but got \"%s\"", tt.Expect, got)
			}
		})
	}
}
