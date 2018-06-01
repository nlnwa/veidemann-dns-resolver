// Package example is a CoreDNS plugin that prints "example" to stdout on every packet received.
//
// It serves as an example CoreDNS plugin with numerous code comments.
package archiver

import (
	"fmt"
	"io"
	"os"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"github.com/miekg/dns"
	"golang.org/x/net/context"
	"time"
	"google.golang.org/grpc"
	vm "github.com/nlnwa/veidemann-dns-resolver/veidemann_api"
	"strings"
	"github.com/golang/protobuf/ptypes"
	"crypto/sha1"
)

// Define log to be a logger with the plugin name in it. This way we can just use log.Info and
// friends to log.
var log = clog.NewWithPlugin("archiver")

// Archiver is an example plugin to show how to write a plugin.
type Archiver struct {
	Next           plugin.Handler
	Host           string
	Port           int
	Addr           string
	UpstreamHostIp string
}

// ServeDNS implements the plugin.Handler interface. This method gets called when example is used
// in a Server.
func (e Archiver) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	// This function could be simpler. I.e. just fmt.Println("example") here, but we want to show
	// a slightly more complex example as to make this more interesting.
	// Here we wrap the dns.ResponseWriter in a new ResponseWriter and call the next plugin, when the
	// answer comes back, it will print "example".

	// Debug log that we've have seen the query. This will only be shown when the debug plugin is loaded.
	log.Debug("Received response")
	fmt.Fprintln(out, r.Question)
	fmt.Fprintln(out, "HOST: ", e.Host, ":", e.Port)
	fmt.Fprintln(out, "UPSTREAM: ", e.UpstreamHostIp)

	// Wrap.
	pw := NewResponsePrinter(w, e.Addr, r.Question[0].Name, e.UpstreamHostIp)

	// Export metric with the server label set to the current server handling the request.
	requestCount.WithLabelValues(metrics.WithServer(ctx)).Inc()

	// Call next plugin (if any).
	return plugin.NextOrFailure(e.Name(), e.Next, ctx, pw, r)
}

// Name implements the Handler interface.
func (e Archiver) Name() string { return "archiver" }

// ResponsePrinter wrap a dns.ResponseWriter and will write example to standard output when WriteMsg is called.
type ResponsePrinter struct {
	dns.ResponseWriter
	Addr           string
	FetchStart     time.Time
	RequestedHost  string
	UpstreamHostIp string
}

// NewResponsePrinter returns ResponseWriter.
func NewResponsePrinter(w dns.ResponseWriter, a string, requestedHost string, upstreamHostIp string) *ResponsePrinter {
	return &ResponsePrinter{
		ResponseWriter: w,
		Addr:           a,
		FetchStart:     time.Now().UTC(),
		RequestedHost:  requestedHost,
		UpstreamHostIp: upstreamHostIp,
	}
}

// WriteMsg calls the underlying ResponseWriter's WriteMsg method and prints "example" to standard output.
func (r *ResponsePrinter) WriteMsg(res *dns.Msg) error {
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
			fmt.Fprintln(out, res)
		}
		if record != nil {
			break
		}
	}

	//var statusCode int32
	//t := r.FetchStart
	//ts, _ := ptypes.TimestampProto(r.FetchStart.UTC())

	if record == nil {
		//statusCode = -1
	} else {
		res.Answer = []dns.RR{record}
		r.writeContentwriter(record.String())
	}

	r.writeCrawlLog("")

	fmt.Println("=====================\n", res, "\n")

	return r.ResponseWriter.WriteMsg(res)
}

func (r *ResponsePrinter) writeContentwriter(record string) {
	t := r.FetchStart
	ts, _ := ptypes.TimestampProto(t)

	payload := []byte(fmt.Sprintf("%d%02d%02d%02d%02d%02d\n%s\n",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(), record))

	host := strings.Trim(r.RequestedHost, ".")

	d := sha1.New()
	d.Write(payload)
	digest := fmt.Sprintf("sha1:%x", d.Sum(nil))

	metaRequest := &vm.WriteRequest{
		Value: &vm.WriteRequest_Meta{
			Meta: &vm.WriteRequestMeta{
				RecordMeta: map[int32]*vm.WriteRequestMeta_RecordMeta{
					0: {
						RecordNum:         0,
						Type:              vm.RecordType_RESOURCE,
						RecordContentType: "text/dns",
						Size:              int64(len(payload)),
						BlockDigest:       digest,
					},
				},
				TargetUri:      "dns:" + host,
				StatusCode:     1,
				FetchTimeStamp: ts,
				IpAddress:      r.UpstreamHostIp,
			},
		},
	}

	payloadRequest := &vm.WriteRequest{
		Value: &vm.WriteRequest_Payload{
			Payload: &vm.Data{
				RecordNum: 0,
				Data:      payload,
			},
		},
	}

	fmt.Printf("\nPayload:\n%s\n", payload)
	fmt.Printf("Digest: %s\n", digest)
	fmt.Printf("Size: %d\n\n", int64(len(payload)))
	//log.Infof("Request: %s, Payload:\n%s", host, payload)
	//log.Infof("Upstream: %s", r.UpstreamHostIp)
	//log.Infof("Meta: %s", payloadRequest)
	//log.Infof("Payload: %s", metaRequest)
	//log.Infof("Digest: %s", digest)

	// Set Host to test for unit tests to avoid contacting contentwriter
	if strings.Contains(r.Addr, "test") {
		return
	}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure(), grpc.WithBlock())

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dialCancel()
	conn, err := grpc.DialContext(dialCtx, r.Addr, opts...)
	if err != nil {
		log.Errorf("fail to dial: %v", err)
	}
	defer conn.Close()

	client := vm.NewContentWriterClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s, err := client.Write(ctx)
	if err != nil {
		log.Errorf("%v.Resolve(_) = _, %v: ", client, err)
		return
	}

	s.Send(metaRequest)
	s.Send(payloadRequest)

	reply, error := s.CloseAndRecv()
	if error != nil {
		log.Errorf("Error writing DNS record to content writer: %v", error)
		return
	}

	fmt.Println(reply)
	//.setBlockDigest(new Sha1Digest().update(data).getPrefixedDigestString())

}
func (r *ResponsePrinter) writeCrawlLog(record string) {

}

// Make out a reference to os.Stdout so we can easily overwrite it for testing.
var out io.Writer = os.Stdout
