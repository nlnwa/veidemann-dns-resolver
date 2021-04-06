package archivingcache

import (
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/transport"
	log2 "github.com/nlnwa/veidemann-api/go/log/v1"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/forward"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/resolve"
	"github.com/nlnwa/veidemann-log-service/pkg/logclient"
	"reflect"
	"sync"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	configV1 "github.com/nlnwa/veidemann-api/go/config/v1"
	contentwriterV1 "github.com/nlnwa/veidemann-api/go/contentwriter/v1"

	"bytes"
	"context"
	"fmt"
	"github.com/miekg/dns"
	"time"
)

func TestExample(t *testing.T) {
	s := dnstest.NewServer(func(w dns.ResponseWriter, r *dns.Msg) {
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.Answer = append(msg.Answer, test.A("example.org. IN A 127.0.0.1"))
		err := w.WriteMsg(msg)
		if err != nil {
			t.Errorf("failed to write reply: %v", err)
		}
	})
	defer s.Close()

	ctx := context.WithValue(context.Background(), dnsserver.Key{}, &dnsserver.Server{
		Addr: s.Addr,
	})

	// Setup forward plugin with server as proxy
	next := forward.New()
	next.SetProxy(forward.NewProxy(s.Addr, transport.DNS))

	// Setup contentWriter mock
	cws := NewContentWriterServerMock(5001)
	defer cws.Close()

	logServiceMock := NewLogServiceMock(5002)
	defer logServiceMock.Close()

	// Setup cache
	ca, err := NewCache(10*time.Second, 1024)
	if err != nil {
		t.Fatal(err)
	}
	// Setup contentWriter client
	cwc := NewContentWriterClient("localhost", 5001)

	logClient := logclient.New(logclient.WithPort(5002))

	// Setup plugin with cache, database mock and content writer mock
	a := NewArchivingCache(ca, logClient, cwc)
	a.Next = next
	err = a.OnStartup()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()

	req1 := new(dns.Msg)
	req1.SetQuestion("example.org.", dns.TypeA)
	rec1 := dnstest.NewRecorder(&test.ResponseWriter{})
	ctx1 := context.WithValue(ctx, resolve.CollectionIdKey{}, "collectionId1")

	// Call our plugin directly, and check the result.
	_, _ = a.ServeDNS(ctx1, rec1, req1)

	msg1 := rec1.Msg

	// expect answer message to have same id as the request
	if msg1.Id != req1.Id {
		t.Fatalf("Expected answer message to have same id as the request. Expected: %v, got: %v", req1.Id, msg1.Id)
	}
	// expect answer section with A archive in it
	if len(msg1.Answer) == 0 {
		t.Fatalf("Expected at least one RR in the answer section, got none: %s", msg1)
	}
	if msg1.Answer[0].Header().Rrtype != dns.TypeA {
		t.Errorf("Expected RR to A, got: %d", msg1.Answer[0].Header().Rrtype)
	}
	if msg1.Answer[0].(*dns.A).A.String() != "127.0.0.1" {
		t.Errorf("Expected 127.0.0.1, got: %s", msg1.Answer[0].(*dns.A).A.String())
	}

	// Validate ContentWriters Meta archive
	if cws.Meta.IpAddress != s.Addr {
		t.Errorf("Expected 127.0.0.1, got: %s", cws.Meta.IpAddress)
	}
	if cws.Meta.TargetUri != "dns:example.org" {
		t.Errorf("Expected dns:example.org, got: %s", cws.Meta.TargetUri)
	}
	if cws.Meta.RecordMeta[0].Type != contentwriterV1.RecordType_RESOURCE {
		t.Errorf("Expected %d, got: %d", contentwriterV1.RecordType_RESOURCE, cws.Meta.RecordMeta[0].Type)
	}
	if cws.Meta.RecordMeta[0].RecordContentType != "text/dns" {
		t.Errorf("Expected text/dns, got: %s", cws.Meta.RecordMeta[0].RecordContentType)
	}
	if cws.Meta.RecordMeta[0].Size != 48 {
		t.Errorf("Expected 48, got: %d", cws.Meta.RecordMeta[0].Size)
	}
	if cws.Meta.RecordMeta[0].RecordNum != 0 {
		t.Errorf("Expected 0, got: %d", cws.Meta.RecordMeta[0].Size)
	}
	if cws.Meta.CollectionRef.GetId() != "collectionId1" {
		t.Errorf("Expected collectionId1, got: %v", cws.Meta.GetCollectionRef().GetId())
	}
	// Validate ContentWriters Payload archive
	ts := time.Now().UTC()

	formattedTime := fmt.Sprintf("%d%02d%02d%02d%02d%02d",
		ts.Year(), ts.Month(), ts.Day(),
		ts.Hour(), ts.Minute(), ts.Second())

	expectedPayload := []byte(formattedTime + "\nexample.org.\t3600\tIN\tA\t127.0.0.1\n")
	if bytes.Compare(cws.Payload.Data, expectedPayload) != 0 {
		t.Errorf("Expected '%s', got: '%s'", expectedPayload, cws.Payload.Data)
	}

	expectedCrawlLog := &log2.CrawlLog{
		RecordType:          "resource",
		PayloadDigest:       "pd",
		WarcId:              "WarcId:collectionId1",
		RequestedUri:        "dns:example.org",
		IpAddress:           s.Addr,
		Size:                int64(48),
		StatusCode:          1,
		BlockDigest:         "bd",
		DiscoveryPath:       "P",
		StorageRef:          "ref",
		ContentType:         "text/dns",
		CollectionFinalName: "cfn",
	}
	gotCrawlLog := logServiceMock.CrawlLogs[0]
	expectedCrawlLog.TimeStamp = gotCrawlLog.TimeStamp
	expectedCrawlLog.FetchTimeMs = gotCrawlLog.FetchTimeMs
	expectedCrawlLog.FetchTimeStamp = gotCrawlLog.FetchTimeStamp
	if !reflect.DeepEqual(expectedCrawlLog, gotCrawlLog) {
		t.Fatalf("Expected %v, got %v", expectedCrawlLog, gotCrawlLog)
	}

	// Call our plugin directly, and check the result.
	req2 := new(dns.Msg)
	req2.SetQuestion("example.org.", dns.TypeA)
	// req2.Extra = append(req2.Extra, c2)
	// Create a new Recorder that captures the result.
	rec2 := dnstest.NewRecorder(&test.ResponseWriter{})

	ctx2 := context.WithValue(ctx, resolve.CollectionIdKey{}, "collectionId2")

	_, _ = a.ServeDNS(ctx2, rec2, req2)

	msg2 := rec2.Msg
	// expect answer message to have same id as the request
	if msg2.Id != req2.Id {
		t.Fatalf("Expected answer message to have same id as the request. Expected: %v, got: %v", req2.Id, msg2.Id)
	}
	// Set id equal to first message to check that all other fields match
	msg2.Id = msg1.Id
	if !reflect.DeepEqual(msg1, msg2) {
		t.Errorf("Expected second request to get cached message. Expected:\n%v, Got:\n%v", msg1, msg2)
	}

	expectedCrawlLog = &log2.CrawlLog{
		RecordType:          "resource",
		PayloadDigest:       "pd",
		WarcId:              "WarcId:collectionId2",
		RequestedUri:        "dns:example.org",
		IpAddress:           s.Addr,
		Size:                int64(48),
		StatusCode:          1,
		BlockDigest:         "bd",
		DiscoveryPath:       "P",
		StorageRef:          "ref",
		ContentType:         "text/dns",
		CollectionFinalName: "cfn",
	}
	gotCrawlLog = logServiceMock.CrawlLogs[1]
	expectedCrawlLog.TimeStamp = gotCrawlLog.TimeStamp
	expectedCrawlLog.FetchTimeMs = gotCrawlLog.FetchTimeMs
	expectedCrawlLog.FetchTimeStamp = gotCrawlLog.FetchTimeStamp
	if !reflect.DeepEqual(expectedCrawlLog, gotCrawlLog) {
		t.Errorf("Expected %v, got %v", expectedCrawlLog, gotCrawlLog)
	}

	// Call our plugin directly, and check the result.
	req3 := new(dns.Msg)
	req3.SetQuestion("example.org.", dns.TypeA)
	// req3.Extra = append(req3.Extra, c3)
	// Create a new Recorder that captures the result.
	rec3 := dnstest.NewRecorder(&test.ResponseWriter{})
	ctx3 := context.WithValue(context.Background(), resolve.CollectionIdKey{}, &configV1.ConfigRef{Kind: configV1.Kind_collection, Id: "collectionId2"})

	_, _ = a.ServeDNS(ctx3, rec3, req3)

	msg3 := rec3.Msg
	// expect answer message to have same id as the request
	if msg3.Id != req3.Id {
		t.Fatalf("Expected answer message to have same id as the request. Expected: %v, got: %v", req3.Id, msg3.Id)
	}
	// Set id equal to first message to check that all other fields match
	msg3.Id = msg1.Id
	if !reflect.DeepEqual(msg1, msg3) {
		t.Errorf("Expected third request to get cached message. Expected:\n%v, Got:\n%v", msg1, msg3)
	}

	if len(logServiceMock.CrawlLogs) != 2 {
		t.Errorf("Expected 2 crawl logs, got %d", len(logServiceMock.CrawlLogs))
	}
}

func TestConcurrent(t *testing.T) {
	s := dnstest.NewServer(func(w dns.ResponseWriter, r *dns.Msg) {
		msg := new(dns.Msg)
		msg.SetReply(r)
		switch r.Question[0].Name {
		case "example.org.":
			msg.Answer = append(msg.Answer, test.A("example.org. IN A 127.0.0.1"))
		case "example2.org.":
			msg.Answer = append(msg.Answer, test.A("example2.org. IN A 127.0.0.2"))
		}
		_ = w.WriteMsg(msg)
	})
	defer s.Close()

	ctx := context.WithValue(context.Background(), dnsserver.Key{}, &dnsserver.Server{
		Addr: s.Addr,
	})

	// Setup forward plugin with server as proxy
	next := forward.New()
	next.SetProxy(forward.NewProxy(s.Addr, transport.DNS))

	// Setup contentWriter server mock
	cws := NewContentWriterServerMock(5001)
	defer cws.Close()

	// Setup cache
	ca, err := NewCache(10*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Setup contentWriter client
	cwc := NewContentWriterClient("localhost", 5001)

	// Setup log service mock
	logServiceMock := NewLogServiceMock(5002)
	defer logServiceMock.Close()

	logClient := logclient.New(logclient.WithPort(5002))

	// Setup plugin
	a := NewArchivingCache(ca, logClient, cwc)
	a.Next = next
	err = a.OnStartup()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()

	ctx = context.WithValue(ctx, resolve.CollectionIdKey{}, "foo")

	req1 := new(dns.Msg)
	req1.SetQuestion("example.org.", dns.TypeA)

	req2 := new(dns.Msg)
	req2.SetQuestion("example2.org.", dns.TypeA)

	reqs := []*dns.Msg{req1, req2}

	it := 10
	expected := it * len(reqs)

	ch := make(chan *dns.Msg, expected)
	runParallelRequests(t, ctx, a, ch, reqs, it)
	close(ch)

	totalPerReq := make(map[string]int, len(reqs))
	total := 0

	for msg := range ch {
		if len(msg.Answer) != 1 {
			t.Errorf("Expected one answer, got: %d", len(msg.Answer))
		}
		totalPerReq[msg.Answer[0].String()]++
		total++
	}

	if len(logServiceMock.CrawlLogs) > expected {
		t.Errorf("Expected less than %d written crawl logs, got %d", expected, len(logServiceMock.CrawlLogs))
	}

	if total != expected {
		t.Errorf("Expected %d messages, got: %d", expected, total)
	}

	for _, nr := range totalPerReq {
		if nr != it {
			t.Errorf("Expected %d answers, got: %d", it, nr)
		}
	}
}

func runParallelRequests(t *testing.T, ctx context.Context, h plugin.Handler, ch chan *dns.Msg, reqs []*dns.Msg, it int) {
	rec := NewChannelRecorder(ch)
	for i := 0; i < it*len(reqs); i++ {
		rec.Add(1)
		nr := i
		go func() {
			req := reqs[nr%len(reqs)]
			rc, err := h.ServeDNS(ctx, rec, req)
			if rc != dns.RcodeSuccess || err != nil {
				t.Errorf("Failed to resolve %s: %v", req.Question[0].Name, err)
			}
		}()
	}
	rec.Wait()
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

// WriteMsg records the status code and calls the
// underlying ResponseWriter's WriteMsg method.
func (r *ChannelRecorder) WriteMsg(res *dns.Msg) error {
	defer r.Done()
	r.ch <- res
	return r.ResponseWriter.WriteMsg(res)
}
