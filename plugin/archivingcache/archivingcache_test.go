package archivingcache

import (
	"github.com/nlnwa/veidemann-dns-resolver/plugin/resolve"
	"reflect"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	configV1 "github.com/nlnwa/veidemann-api-go/config/v1"
	contentwriterV1 "github.com/nlnwa/veidemann-api-go/contentwriter/v1"
	r "gopkg.in/gorethink/gorethink.v4"

	"bytes"
	"context"
	"fmt"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/forward"
	"github.com/miekg/dns"
	"time"
)

func TestExample(t *testing.T) {
	s := dnstest.NewServer(func(w dns.ResponseWriter, r *dns.Msg) {
		ret := new(dns.Msg)
		ret.SetReply(r)
		ret.Answer = append(ret.Answer, test.A("example.org. IN A 127.0.0.1"))
		w.WriteMsg(ret)
	})
	defer s.Close()

	cws := NewCWServer(5001)
	defer cws.Close()

	// Create a new Archiver Plugin. Use the test.ErrorHandler as the next plugin.
	addr := []string{s.Addr}
	conn := &Connection{
		contentWriterAddr: "localhost:5001",
		dbConnectOpts: r.ConnectOpts{
			Database: "mock",
		},
	}
	a, _ := NewArchivingCache(10*time.Second, 1024, "127.0.0.1", "53", conn)
	a.Next = test.ErrorHandler()
	a.forward = forward.NewLookup(addr)
	a.Connection.connect()

	defer a.Close()

	// Set the expected db queries
	dbMock := a.Connection.dbSession.(*r.Mock)
	dbQuery1 := dbMock.On(r.Table("crawl_log").Insert(map[string]interface{}{
		"recordType":          "resource",
		"payloadDigest":       "pd",
		"warcId":              "WarcId:collectionId",
		"requestedUri":        "dns:example.org",
		"ipAddress":           "127.0.0.1",
		"size":                int64(48),
		"statusCode":          1,
		"blockDigest":         "bd",
		"discoveryPath":       "P",
		"fetchTimeStamp":      r.EpochTime(r.MockAnything()),
		"fetchTimeMs":         r.MockAnything(),
		"contentType":         "text/dns",
		"collectionFinalName": "cfn"}),
	).Return(map[string]interface{}{"foo": "bar"}, nil)
	dbQuery2 := dbMock.On(r.Table("crawl_log").Insert(map[string]interface{}{
		"recordType":          "resource",
		"payloadDigest":       "pd",
		"warcId":              "WarcId:collectionId2",
		"requestedUri":        "dns:example.org",
		"ipAddress":           "127.0.0.1",
		"size":                int64(48),
		"statusCode":          1,
		"blockDigest":         "bd",
		"discoveryPath":       "P",
		"fetchTimeStamp":      r.EpochTime(r.MockAnything()),
		"fetchTimeMs":         r.MockAnything(),
		"contentType":         "text/dns",
		"collectionFinalName": "cfn"}),
	).Return(map[string]interface{}{"foo": "bar"}, nil)

	ctx1 := context.TODO()
	req1 := new(dns.Msg)
	req1.SetQuestion("example.org.", dns.TypeA)
	c1 := &resolve.COLLECTION{CollectionRef: &configV1.ConfigRef{Kind: configV1.Kind_collection, Id: "collectionId"}}
	req1.Extra = append(req1.Extra, c1)
	// Create a new Recorder that captures the result.
	rec1 := dnstest.NewRecorder(&test.ResponseWriter{})

	// Call our plugin directly, and check the result.
	a.ServeDNS(ctx1, rec1, req1)

	msg1 := rec1.Msg
	// expect answer message to have same id as the request
	if msg1.Id != req1.Id {
		t.Fatalf("Expected answer message to have same id as the request. Expected: %v, got: %v", req1.Id, msg1.Id)
	}
	// expect answer section with A record in it
	if len(msg1.Answer) == 0 {
		t.Fatalf("Expected at least one RR in the answer section, got none: %s", msg1)
	}
	if msg1.Answer[0].Header().Rrtype != dns.TypeA {
		t.Errorf("Expected RR to A, got: %d", msg1.Answer[0].Header().Rrtype)
	}
	if msg1.Answer[0].(*dns.A).A.String() != "127.0.0.1" {
		t.Errorf("Expected 127.0.0.1, got: %s", msg1.Answer[0].(*dns.A).A.String())
	}

	// Validate ContentWriters Meta record
	if cws.Meta.IpAddress != "127.0.0.1" {
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

	// Validate ContentWriters Payload record
	ts := time.Now().UTC()

	formattedTime := fmt.Sprintf("%d%02d%02d%02d%02d%02d",
		ts.Year(), ts.Month(), ts.Day(),
		ts.Hour(), ts.Minute(), ts.Second())

	expected := []byte(formattedTime + "\nexample.org.\t3600\tIN\tA\t127.0.0.1\n")
	if bytes.Compare(cws.Payload.Data, expected) != 0 {
		t.Errorf("Expected '%s', got: '%s'", expected, cws.Payload.Data)
	}

	// Call our plugin directly, and check the result.
	req2 := new(dns.Msg)
	req2.SetQuestion("example.org.", dns.TypeA)
	c2 := &resolve.COLLECTION{CollectionRef: &configV1.ConfigRef{Kind: configV1.Kind_collection, Id: "collectionId2"}}
	req2.Extra = append(req2.Extra, c2)
	// Create a new Recorder that captures the result.
	rec2 := dnstest.NewRecorder(&test.ResponseWriter{})
	ctx2 := context.TODO()
	a.ServeDNS(ctx2, rec2, req2)

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

	// Call our plugin directly, and check the result.
	req3 := new(dns.Msg)
	req3.SetQuestion("example.org.", dns.TypeA)
	c3 := &resolve.COLLECTION{CollectionRef: &configV1.ConfigRef{Kind: configV1.Kind_collection, Id: "collectionId2"}}
	req3.Extra = append(req3.Extra, c3)
	// Create a new Recorder that captures the result.
	rec3 := dnstest.NewRecorder(&test.ResponseWriter{})
	ctx3 := context.TODO()
	a.ServeDNS(ctx3, rec3, req3)

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

	dbMock.AssertExpectations(t)
	dbMock.AssertNumberOfExecutions(t, dbQuery1, 1)
	dbMock.AssertNumberOfExecutions(t, dbQuery2, 1)
}

func TestConcurrent(t *testing.T) {
	s := dnstest.NewServer(func(w dns.ResponseWriter, r *dns.Msg) {
		ret := new(dns.Msg)
		ret.SetReply(r)
		switch r.Question[0].Name {
		case "example.org.":
			ret.Answer = append(ret.Answer, test.A("example.org. IN A 127.0.0.1"))
		case "example2.org.":
			ret.Answer = append(ret.Answer, test.A("example2.org. IN A 127.0.0.2"))
		}
		w.WriteMsg(ret)
	})
	defer s.Close()

	cws := NewCWServer(5001)
	defer cws.Close()

	// Create a new Archiver Plugin. Use the test.ErrorHandler as the next plugin.
	addr := []string{s.Addr}
	conn := &Connection{
		contentWriterAddr: "localhost:5001",
		dbConnectOpts: r.ConnectOpts{
			Database: "mock",
		},
	}
	a, _ := NewArchivingCache(10*time.Second, 1024, "127.0.0.1", "53", conn)
	a.Next = test.ErrorHandler()
	a.forward = forward.NewLookup(addr)
	a.Connection.connect()

	defer a.Close()

	// Set the expected db queries
	m := a.Connection.dbSession.(*r.Mock)
	q := m.On(r.MockAnything()).Return(map[string]interface{}{"foo": "bar"}, nil)

	ctx := context.TODO()
	req1 := new(dns.Msg)
	req1.SetQuestion("example.org.", dns.TypeA)
	req2 := new(dns.Msg)
	req2.SetQuestion("example2.org.", dns.TypeA)

	// Call our plugin directly several times in parallel, and check the result.
	it := 20
	ch := make(chan *dns.Msg)
	res := make(map[string]int)
	total := 0
	runParallelRequests(ctx, a, t, req1, it/4, ch)
	runParallelRequests(ctx, a, t, req2, it/4, ch)
	// Wait for goroutines to finish
	for i := 0; i < it/2; i++ {
		resp := <-ch
		if len(resp.Answer) != 1 {
			t.Errorf("Expected one answer, got: %d", len(resp.Answer))
		}
		res[resp.Answer[0].String()]++
		total++
	}
	runParallelRequests(ctx, a, t, req1, it/4, ch)
	runParallelRequests(ctx, a, t, req2, it/4, ch)
	// Wait for goroutines to finish
	for i := 0; i < it/2; i++ {
		resp := <-ch
		if len(resp.Answer) != 1 {
			t.Errorf("Expected one answer, got: %d", len(resp.Answer))
		}
		res[resp.Answer[0].String()]++
		total++
	}

	a.Connection.dbSession.(*r.Mock).AssertNumberOfExecutions(t, q, 2)

	if total != it {
		t.Errorf("Expected %d messages, got: %d", it, total)
	}

	for _, count := range res {
		if count != it/2 {
			t.Errorf("Expected %d answers, got: %d", it/2, count)
		}
	}
}

func runParallelRequests(ctx context.Context, c plugin.Handler, t *testing.T, req *dns.Msg, it int, ch chan *dns.Msg) {
	for i := 0; i < it; i++ {
		go func() {
			_, err := c.ServeDNS(ctx, NewChannelRecorder(ch), req)
			if err != nil {
				t.Error(err)
			}
		}()
	}
}

// ChannelRecorder is a type of ResponseWriter that sends the message over a channel.
type ChannelRecorder struct {
	dns.ResponseWriter
	Chan chan *dns.Msg
}

// NewChannelRecorder makes and returns a new ChannelRecorder.
func NewChannelRecorder(ch chan *dns.Msg) *ChannelRecorder {
	return &ChannelRecorder{
		ResponseWriter: &test.ResponseWriter{},
		Chan:           ch,
	}
}

// WriteMsg records the status code and calls the
// underlying ResponseWriter's WriteMsg method.
func (r *ChannelRecorder) WriteMsg(res *dns.Msg) error {
	go func() { r.Chan <- res }()
	return r.ResponseWriter.WriteMsg(res)
}
