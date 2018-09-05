package archiver

import (
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	vm "github.com/nlnwa/veidemann-dns-resolver/veidemann_api"
	r "gopkg.in/gorethink/gorethink.v4"

	"bytes"
	"context"
	"fmt"
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
	a := Archiver{Next: test.ErrorHandler(),
		forward:      forward.NewLookup(addr),
		UpstreamIp:   "127.0.0.1",
		UpstreamPort: "53",
		Connection: &Connection{
			contentWriterAddr: "localhost:5001",
			dbConnectOpts: r.ConnectOpts{
				Database: "mock",
			},
		},
	}
	a.Connection.connect()

	defer a.Close()

	// Set the expected db queries
	m := a.Connection.dbSession.(*r.Mock)
	m.On(r.Table("crawl_log").Insert(map[string]interface{}{
		"recordType":     "response",
		"payloadDigest":  "pd",
		"warcId":         "WarcId",
		"requestedUri":   "dns:example.org",
		"ipAddress":      "127.0.0.1",
		"size":           int64(48),
		"statusCode":     1,
		"blockDigest":    "bd",
		"discoveryPath":  "P",
		"fetchTimeStamp": r.EpochTime(r.MockAnything()),
		"contentType":    "text/dns"}))

	ctx := context.TODO()
	req := new(dns.Msg)
	req.SetQuestion("example.org.", dns.TypeA)
	// Create a new Recorder that captures the result.
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	// Call our plugin directly, and check the result.
	a.ServeDNS(ctx, rec, req)

	// expect answer section with A record in it
	if len(rec.Msg.Answer) == 0 {
		t.Fatalf("Expected to at least one RR in the answer section, got none: %s", rec.Msg)
	}
	if rec.Msg.Answer[0].Header().Rrtype != dns.TypeA {
		t.Errorf("Expected RR to A, got: %d", rec.Msg.Answer[0].Header().Rrtype)
	}
	if rec.Msg.Answer[0].(*dns.A).A.String() != "127.0.0.1" {
		t.Errorf("Expected 127.0.0.1, got: %s", rec.Msg.Answer[0].(*dns.A).A.String())
	}

	// Validate ContentWriters Meta record
	if cws.Meta.IpAddress != "127.0.0.1" {
		t.Errorf("Expected 127.0.0.1, got: %s", cws.Meta.IpAddress)
	}
	if cws.Meta.TargetUri != "dns:example.org" {
		t.Errorf("Expected dns:example.org, got: %s", cws.Meta.TargetUri)
	}
	if cws.Meta.RecordMeta[0].Type != vm.RecordType_RESOURCE {
		t.Errorf("Expected %d, got: %d", vm.RecordType_RESOURCE, cws.Meta.RecordMeta[0].Type)
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
	if cws.Meta.StatusCode != 1 {
		t.Errorf("Expected 1, got: %d", cws.Meta.StatusCode)
	}

	// Validate ContentWriters Payload record
	ts := time.Now().UTC()

	formattedTime := fmt.Sprintf("%d%02d%02d%02d%02d%02d",
		ts.Year(), ts.Month(), ts.Day(),
		ts.Hour(), ts.Minute(), ts.Second())

	if bytes.Compare(cws.Payload.Data, []byte(formattedTime+"\nexample.org.\t3600\tIN\tA\t127.0.0.1\n")) != 0 {
		t.Errorf("Expected 127.0.0.1, got: '%s'", cws.Payload.Data)
	}

	a.Connection.dbSession.(*r.Mock).AssertExpectations(t)
}
