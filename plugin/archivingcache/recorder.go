package archivingcache

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"github.com/coredns/coredns/plugin"
	"github.com/golang/protobuf/ptypes"
	"github.com/miekg/dns"
	configV1 "github.com/nlnwa/veidemann-api-go/config/v1"
	contentwriterV1 "github.com/nlnwa/veidemann-api-go/contentwriter/v1"
	r "gopkg.in/gorethink/gorethink.v4"
	"net"
	"strings"
	"time"
)

// Recorder is a type of ResponseWriter that captures
// the message written to it.
type Recorder struct {
	localAddr           net.Addr
	remoteAddr          net.Addr
	Rcode               int
	Err                 error
	Msg                 *dns.Msg
	server              string // Server handling the request.
	a                   *ArchivingCache
	key                 string
	FetchStart          time.Time
	FetchDurationMs     int64
	RequestedHost       string
	UpstreamIP          string
	payload             []byte
	contentWriterClient contentwriterV1.ContentWriterClient
	dbSession           r.QueryExecutor
	collectionRef       *configV1.ConfigRef
}

// NewRecorder makes and returns a new Recorder,
func NewRecorder(a *ArchivingCache, key string, server string, connection *Connection,
	requestedHost string, upstreamIP string, collectionRef *configV1.ConfigRef) *Recorder {
	return &Recorder{
		Rcode:               dns.RcodeSuccess,
		Err:                 nil,
		Msg:                 nil,
		server:              server,
		a:                   a,
		key:                 key,
		contentWriterClient: connection.contentWriterClient,
		dbSession:           connection.dbSession,
		FetchStart:          time.Now().UTC(),
		RequestedHost:       strings.Trim(requestedHost, "."),
		UpstreamIP:          upstreamIP,
		collectionRef:       collectionRef,
	}
}

// Write is the hack that makes this work. It does not actually write the message
// but returns the bytes we need to to write in r. We can then pick this up in Query
// and write a proper protobuf back to the client.
// Write implements the dns.ResponseWriter interface.
func (rec *Recorder) Write(b []byte) (int, error) {
	if rec.Msg != nil {
		log.Errorf("Recorders WriteMsg was called twice")
		return len(b), errors.New("Recorders WriteMsg was called twice")
	}
	m := new(dns.Msg)
	m.Unpack(b)

	return len(b), rec.WriteMsg(m)
}

// WriteMsg records the messsage.
// WriteMsg implements the dns.ResponseWriter interface.
func (rec *Recorder) WriteMsg(res *dns.Msg) error {
	if rec.Msg != nil {
		log.Errorf("Recorders WriteMsg was called twice")
		return errors.New("Recorders WriteMsg was called twice")
	}
	cacheEntry := &CacheEntry{r: res}
	if rec.collectionRef != nil {
		cacheEntry.AddCollectionId(rec.collectionRef.Id)
	}
	if err := rec.a.cache.Put(rec.key, rec.server, cacheEntry); err != nil {
		log.Errorf("WriteMsg %v", err)
		return err
	}
	rec.Msg = res

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
		rec.FetchDurationMs = (time.Now().UTC().Sub(rec.FetchStart).Nanoseconds() + 500000) / 1000000
		reply := rec.writeContentwriter(record.String())
		rec.writeCrawlLog(reply.GetMeta().GetRecordMeta()[0])
	}

	return nil
}

// SetMsg records the messsage without adding it to the cache.
func (rec *Recorder) SetMsg(res *dns.Msg) error {
	if rec.Msg != nil {
		log.Errorf("Recorders WriteMsg was called twice")
		return errors.New("Recorders WriteMsg was called twice")
	}
	rec.Msg = res
	return nil
}

// NextPlugin runs the next plugin and captures the result
func (rec *Recorder) NextPlugin(ctx context.Context, request *dns.Msg) {
	rec.Rcode, rec.Err = plugin.NextOrFailure(rec.a.Name(), rec.a.Next, ctx, rec, request)
	if rec.Err != nil {
		log.Errorf("NextPlugin %v", rec.Err)
	}
}

// WriteRecordedMessage writes the message to the wrapped ResponseWriter
func (rec *Recorder) WriteRecordedMessage(rw dns.ResponseWriter, request *dns.Msg, shared bool) (int, error) {
	m := rec.Msg
	if shared {
		m = rec.Msg.Copy()
	}
	m.SetReply(request)
	if err := rw.WriteMsg(m); err != nil {
		log.Errorf("WriteRecordedMessage %v", err)
		return dns.RcodeServerFailure, err
	}
	return rec.Rcode, rec.Err
}

func (rec *Recorder) writeContentwriter(record string) *contentwriterV1.WriteReply {
	t := rec.FetchStart
	ts, _ := ptypes.TimestampProto(t)

	rec.payload = []byte(fmt.Sprintf("%d%02d%02d%02d%02d%02d\n%s\n",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(), record))

	host := strings.Trim(rec.RequestedHost, ".")

	d := sha1.New()
	d.Write(rec.payload)
	digest := fmt.Sprintf("sha1:%x", d.Sum(nil))

	metaRequest := &contentwriterV1.WriteRequest{
		Value: &contentwriterV1.WriteRequest_Meta{
			Meta: &contentwriterV1.WriteRequestMeta{
				RecordMeta: map[int32]*contentwriterV1.WriteRequestMeta_RecordMeta{
					0: {
						RecordNum:         0,
						Type:              contentwriterV1.RecordType_RESOURCE,
						RecordContentType: "text/dns",
						Size:              int64(len(rec.payload)),
						BlockDigest:       digest,
						SubCollection:     configV1.Collection_DNS,
					},
				},
				TargetUri:      "dns:" + host,
				StatusCode:     1,
				FetchTimeStamp: ts,
				IpAddress:      rec.UpstreamIP,
				CollectionRef:  rec.collectionRef,
			},
		},
	}

	payloadRequest := &contentwriterV1.WriteRequest{
		Value: &contentwriterV1.WriteRequest_Payload{
			Payload: &contentwriterV1.Data{
				RecordNum: 0,
				Data:      rec.payload,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s, err := rec.contentWriterClient.Write(ctx)
	if err != nil {
		log.Errorf("%v.Resolve(_) = _, %v: ", rec.contentWriterClient, err)
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

func (rec *Recorder) writeCrawlLog(record *contentwriterV1.WriteResponseMeta_RecordMeta) {
	crawlLog := map[string]interface{}{
		"recordType":          "response",
		"requestedUri":        "dns:" + strings.Trim(rec.RequestedHost, "."),
		"discoveryPath":       "P",
		"statusCode":          1,
		"fetchTimeStamp":      r.EpochTime(rec.FetchStart.Unix()),
		"fetchTimeMs":         rec.FetchDurationMs,
		"ipAddress":           rec.UpstreamIP,
		"contentType":         "text/dns",
		"size":                int64(len(rec.payload)),
		"warcId":              record.GetWarcId(),
		"blockDigest":         record.GetBlockDigest(),
		"payloadDigest":       record.GetPayloadDigest(),
		"collectionFinalName": record.GetCollectionFinalName(),
	}

	res, err := r.Table("crawl_log").Insert(crawlLog).Run(rec.dbSession)
	if err != nil {
		log.Error(err)
	}

	var response map[string]interface{}
	err = res.One(&response)
	if err != nil {
		log.Errorf("Failed writing to DB. %v", err)
	}
}

// Close implement the dns.ResponseWriter interface from Go DNS.
func (rec *Recorder) Close() error { return nil }

// TsigStatus implement the dns.ResponseWriter interface from Go DNS.
func (rec *Recorder) TsigStatus() error { return nil }

// TsigTimersOnly implement the dns.ResponseWriter interface from Go DNS.
func (rec *Recorder) TsigTimersOnly(b bool) { return }

// Hijack implement the dns.ResponseWriter interface from Go DNS.
func (rec *Recorder) Hijack() { return }

// LocalAddr implement the dns.ResponseWriter interface from Go DNS.
func (rec *Recorder) LocalAddr() net.Addr { return rec.localAddr }

// RemoteAddr implement the dns.ResponseWriter interface from Go DNS.
func (rec *Recorder) RemoteAddr() net.Addr { return rec.remoteAddr }
