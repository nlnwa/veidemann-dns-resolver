package syncache

import (
	"github.com/miekg/dns"
	"github.com/coredns/coredns/plugin"
	"context"
	"errors"
	"net"
)

// Recorder is a type of ResponseWriter that captures
// the message written to it.
type Recorder struct {
	localAddr  net.Addr
	remoteAddr net.Addr
	Rcode      int
	Err        error
	Msg        *dns.Msg
	server     string // Server handling the request.
	s          *Syncache
	key        string
}

// NewRecorder makes and returns a new Recorder,
func NewRecorder(s *Syncache, key string, server string) *Recorder {
	return &Recorder{
		Rcode:  dns.RcodeSuccess,
		Err:    nil,
		Msg:    nil,
		server: server,
		s:      s,
		key:    key,
	}
}

// Write is the hack that makes this work. It does not actually write the message
// but returns the bytes we need to to write in r. We can then pick this up in Query
// and write a proper protobuf back to the client.
// Write implements the dns.ResponseWriter interface.
func (r *Recorder) Write(b []byte) (int, error) {
	if r.Msg != nil {
		log.Errorf("Recorders WriteMsg was called twice")
		return len(b), errors.New("Recorders WriteMsg was called twice")
	}
	m := new(dns.Msg)
	m.Unpack(b)

	return len(b), r.WriteMsg(m)
}

// WriteMsg records the messsage.
// WriteMsg implements the dns.ResponseWriter interface.
func (r *Recorder) WriteMsg(res *dns.Msg) error {
	if r.Msg != nil {
		log.Errorf("Recorders WriteMsg was called twice")
		return errors.New("Recorders WriteMsg was called twice")
	}
	if err := r.s.cache.Put(r.key, r.server, res); err != nil {
		log.Errorf("WriteMsg %v", err)
		return err
	}
	r.Msg = res
	return nil
}

// SetMsg records the messsage without adding it to the cache.
func (r *Recorder) SetMsg(res *dns.Msg) error {
	if r.Msg != nil {
		log.Errorf("Recorders WriteMsg was called twice")
		return errors.New("Recorders WriteMsg was called twice")
	}
	r.Msg = res
	return nil
}

// NextPlugin runs the next plugin and captures the result
func (r *Recorder) NextPlugin(ctx context.Context, request *dns.Msg) {
	r.Rcode, r.Err = plugin.NextOrFailure(r.s.Name(), r.s.Next, ctx, r, request)
	if r.Err != nil {
		log.Errorf("NextPlugin %v", r.Err)
	}
}

// WriteRecordedMessage writes the message to the wrapped ResponseWriter
func (r *Recorder) WriteRecordedMessage(rw dns.ResponseWriter, request *dns.Msg, shared bool) (int, error) {
	m := r.Msg
	if shared {
		m = r.Msg.Copy()
	}
	m.SetReply(request)
	if err := rw.WriteMsg(m); err != nil {
		log.Errorf("WriteRecordedMessage %v", err)
		return dns.RcodeServerFailure, err
	}
	return r.Rcode, r.Err
}

// Close implement the dns.ResponseWriter interface from Go DNS.
func (r *Recorder) Close() error          { return nil }
// TsigStatus implement the dns.ResponseWriter interface from Go DNS.
func (r *Recorder) TsigStatus() error     { return nil }
// TsigTimersOnly implement the dns.ResponseWriter interface from Go DNS.
func (r *Recorder) TsigTimersOnly(b bool) { return }
// Hijack implement the dns.ResponseWriter interface from Go DNS.
func (r *Recorder) Hijack()               { return }
// LocalAddr implement the dns.ResponseWriter interface from Go DNS.
func (r *Recorder) LocalAddr() net.Addr   { return r.localAddr }
// RemoteAddr implement the dns.ResponseWriter interface from Go DNS.
func (r *Recorder) RemoteAddr() net.Addr  { return r.remoteAddr }
