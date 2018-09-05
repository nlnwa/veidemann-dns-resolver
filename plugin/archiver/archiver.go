// Package example is a CoreDNS plugin that prints "example" to stdout on every packet received.
//
// It serves as an example CoreDNS plugin with numerous code comments.
package archiver

import (
	"fmt"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"context"
	"crypto/sha1"
	"github.com/coredns/coredns/plugin/forward"
	"github.com/coredns/coredns/request"
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

// Archiver is an example plugin to show how to write a plugin.
type Archiver struct {
	Next         plugin.Handler
	UpstreamIp   string
	UpstreamPort string
	Connection   *Connection
	forward      *forward.Forward
}

// ServeDNS implements the plugin.Handler interface. This method gets called when archiver is used
// in a Server.
func (a *Archiver) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	// Debug log that we've have seen the query. This will only be shown when the debug plugin is loaded.
	log.Debug("Received response")

	// Wrap.
	pw := NewResponsePrinter(w, a.Connection, r.Question[0].Name, a.UpstreamIp)

	state := request.Request{W: pw, Req: r}
	msg, err := a.forward.Forward(state)
	if err != nil {
		log.Errorf("Upstream error %v", err)
		if msg == nil {
			return dns.RcodeServerFailure, err
		} else {
			return msg.Rcode, err
		}
	}

	pw.WriteMsg(msg)

	// Export metric with the server label set to the current server handling the request.
	requestCount.WithLabelValues(metrics.WithServer(ctx)).Inc()

	return msg.Rcode, nil
}

// Name implements the Handler interface.
func (a *Archiver) Name() string { return "archiver" }

// ResponsePrinter wrap a dns.ResponseWriter and will write example to standard output when WriteMsg is called.
type ResponsePrinter struct {
	dns.ResponseWriter
	FetchStart          time.Time
	RequestedHost       string
	UpstreamIp          string
	payload             []byte
	contentWriterClient vm.ContentWriterClient
	dbSession           r.QueryExecutor
}

// NewResponsePrinter returns ResponseWriter.
func NewResponsePrinter(w dns.ResponseWriter, connection *Connection, requestedHost string, upstreamIp string) *ResponsePrinter {
	return &ResponsePrinter{
		ResponseWriter:      w,
		contentWriterClient: connection.contentWriterClient,
		dbSession:           connection.dbSession,
		FetchStart:          time.Now().UTC(),
		RequestedHost:       strings.Trim(requestedHost, "."),
		UpstreamIp:          upstreamIp,
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
		res.Answer = []dns.RR{record}
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
				IpAddress:      rp.UpstreamIp,
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
		"ipAddress":      rp.UpstreamIp,
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
		log.Error(err)
	}
}
