// Package single implements a plugin allowing only one upstream at a time per similar request.
package single

import (
	"context"
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

// Define log to be a logger with the plugin name in it. This way we can just use log.Info and
// friends to log.
var (
	log = clog.NewWithPlugin("single")
)

func init() { plugin.Register("single", setup) }

func setup(c *caddy.Controller) error {
	c.Next() // 'single'
	if c.NextArg() {
		return plugin.Error("single", c.ArgErr())
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		return &Single{
			Next: next,
		}
	})

	return nil
}

// Single is a plugin that only allow one similar request at a time.
type Single struct {
	Next plugin.Handler
	singleflight.Group
}

// ServeDNS implements the plugin.Handler interface.
func (s *Single) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	val, err, shared := s.Do(key(r), func() (interface{}, error) {
		rec := NewRecorder(w)
		_, err := plugin.NextOrFailure(s.Name(), s.Next, ctx, rec, r)
		return rec, err
	})
	rec := val.(*Recorder)
	if rec.Rcode != dns.RcodeSuccess {
		return rec.Rcode, err
	}

	msg := rec.Msg
	if shared {
		msg = msg.Copy()
	}
	msg.SetReply(r)

	w.WriteMsg(msg)
	return 0, nil
}

// Name implements the Handler interface.
func (s *Single) Name() string { return "single" }

// key returns a string representing the request.
func key(r *dns.Msg) string {
	return dns.Name(r.Question[0].Name).String() +
		dns.Class(r.Question[0].Qclass).String() +
		dns.Type(r.Question[0].Qtype).String()
}

type Recorder struct {
	dns.ResponseWriter
	Rcode int
	Msg   *dns.Msg
}

// NewRecorder makes and returns a new Recorder.
func NewRecorder(w dns.ResponseWriter) *Recorder { return &Recorder{ResponseWriter: w} }

// WriteMsg records the message and the response code, but doesn't write anything to the client.
func (w *Recorder) WriteMsg(res *dns.Msg) error {
	w.Msg = res
	w.Rcode = res.Rcode
	return nil
}
