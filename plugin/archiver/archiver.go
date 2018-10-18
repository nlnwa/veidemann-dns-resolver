// Package archiver is a CoreDNS plugin that forwards requests to an upstream server
// and store the result to a Content Writer and also writes a log record.
package archiver

import (
	"fmt"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"context"
	"crypto/sha1"
	"github.com/coredns/coredns/plugin/forward"
	"github.com/golang/protobuf/ptypes"
	"github.com/miekg/dns"
	vm "github.com/nlnwa/veidemann-dns-resolver/veidemann_api"
	r "gopkg.in/gorethink/gorethink.v4"
	"strings"
	"time"
)

// Define log to be a logger with the plugin name in it. This way we can just use log.Info and
// friends to log.
var log = clog.NewWithPlugin("archiver")

// Archiver is an archiving forwarder plugin.
type Archiver struct {
	Next         plugin.Handler
	UpstreamIP   string
	UpstreamPort string
	Connection   *Connection
	forward      *forward.Forward
}

// ServeDNS implements the plugin.Handler interface. This method gets called when archiver is used
// in a Server.
func (a *Archiver) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	// Debug log that we've have seen the query. This will only be shown when the debug plugin is loaded.
	log.Debugf("Got request: %v %v %v", r.Question[0].Name, dns.ClassToString[r.Question[0].Qclass], dns.TypeToString[r.Question[0].Qtype])

	// Wrap.
	pw := NewResponsePrinter(w, a.Connection, r.Question[0].Name, a.UpstreamIP)

	// Export metric with the Server label set to the current Server handling the request.
	requestCount.WithLabelValues(metrics.WithServer(ctx)).Inc()

	return a.forward.ServeDNS(ctx, pw, r)
}

// Name implements the Handler interface.
func (a *Archiver) Name() string { return "archiver" }

// ResponsePrinter wrap a dns.ResponseWriter.
type ResponsePrinter struct {
	dns.ResponseWriter
	FetchStart          time.Time
	FetchDurationMs     int64
	RequestedHost       string
	UpstreamIP          string
	payload             []byte
	contentWriterClient vm.ContentWriterClient
	dbSession           r.QueryExecutor
}

// NewResponsePrinter returns ResponseWriter.
func NewResponsePrinter(w dns.ResponseWriter, connection *Connection, requestedHost string, upstreamIP string) *ResponsePrinter {
	return &ResponsePrinter{
		ResponseWriter:      w,
		contentWriterClient: connection.contentWriterClient,
		dbSession:           connection.dbSession,
		FetchStart:          time.Now().UTC(),
		RequestedHost:       strings.Trim(requestedHost, "."),
		UpstreamIP:          upstreamIP,
	}
}

// WriteMsg calls the underlying ResponseWriter's WriteMsg method and prints "example" to standard output.
func (rp *ResponsePrinter) WriteMsg(res *dns.Msg) error {
	var record dns.RR

	for _, answer := range res.Answer {
		switch v := answer.(type) {
		case *dns.A:
			record = v
			break
		case *dns.AAAA:
			record = v
			break
		case *dns.PTR:
			record = v
			break
		default:
			// Not interested in other record types
		}
		if record != nil {
			break
		}
	}

	if record == nil {
		//statusCode = -1
	} else {
		log.Debugf("Got response: %v", record)

		res.Answer = []dns.RR{record}
		// Get the fetch duration in ns, round and convert to ms
		rp.FetchDurationMs = (time.Now().UTC().Sub(rp.FetchStart).Nanoseconds() + 500000) / 1000000
		reply := rp.writeContentwriter(record.String())
		rp.writeCrawlLog(reply.GetMeta().GetRecordMeta()[0])
	}

	return rp.ResponseWriter.WriteMsg(res)
}

func (rp *ResponsePrinter) writeContentwriter(record string) *vm.WriteReply {
	t := rp.FetchStart
	ts, _ := ptypes.TimestampProto(t)

	rp.payload = []byte(fmt.Sprintf("%d%02d%02d%02d%02d%02d\n%s\n",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(), record))

	host := strings.Trim(rp.RequestedHost, ".")

	d := sha1.New()
	d.Write(rp.payload)
	digest := fmt.Sprintf("sha1:%x", d.Sum(nil))

	metaRequest := &vm.WriteRequest{
		Value: &vm.WriteRequest_Meta{
			Meta: &vm.WriteRequestMeta{
				RecordMeta: map[int32]*vm.WriteRequestMeta_RecordMeta{
					0: {
						RecordNum:         0,
						Type:              vm.RecordType_RESOURCE,
						RecordContentType: "text/dns",
						Size:              int64(len(rp.payload)),
						BlockDigest:       digest,
					},
				},
				TargetUri:      "dns:" + host,
				StatusCode:     1,
				FetchTimeStamp: ts,
				IpAddress:      rp.UpstreamIP,
			},
		},
	}

	payloadRequest := &vm.WriteRequest{
		Value: &vm.WriteRequest_Payload{
			Payload: &vm.Data{
				RecordNum: 0,
				Data:      rp.payload,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s, err := rp.contentWriterClient.Write(ctx)
	if err != nil {
		log.Errorf("%v.Resolve(_) = _, %v: ", rp.contentWriterClient, err)
		return nil
	}

	s.Send(metaRequest)
	s.Send(payloadRequest)

	reply, err := s.CloseAndRecv()
	if err != nil {
		log.Errorf("Error writing DNS record to content writer: %v", err)
		return nil
	}

	return reply
}

func (rp *ResponsePrinter) writeCrawlLog(record *vm.WriteResponseMeta_RecordMeta) {
	crawlLog := map[string]interface{}{
		"recordType":     "response",
		"requestedUri":   "dns:" + strings.Trim(rp.RequestedHost, "."),
		"discoveryPath":  "P",
		"statusCode":     1,
		"fetchTimeStamp": r.EpochTime(rp.FetchStart.Unix()),
		"fetchTimeMs":    rp.FetchDurationMs,
		"ipAddress":      rp.UpstreamIP,
		"contentType":    "text/dns",
		"size":           int64(len(rp.payload)),
		"warcId":         record.GetWarcId(),
		"blockDigest":    record.GetBlockDigest(),
		"payloadDigest":  record.GetPayloadDigest(),
	}

	res, err := r.Table("crawl_log").Insert(crawlLog).Run(rp.dbSession)
	if err != nil {
		log.Error(err)
	}

	var response map[string]interface{}
	err = res.One(&response)
	if err != nil {
		log.Errorf("Failed writing to DB. %v", err)
	}
}
