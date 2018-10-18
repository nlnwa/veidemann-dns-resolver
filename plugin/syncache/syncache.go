// Package syncache is a CoreDNS plugin that caches lookups. If more than one request for the same resource
// is in flight, then they are queued to avoid redundant forwarding lookups
package syncache

import (
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"context"
	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
	"time"
	"github.com/coredns/coredns/plugin/metrics"
)

// Define log to be a logger with the plugin name in it. This way we can just use log.Info and
// friends to log.
var (
	log = clog.NewWithPlugin("syncache")
)

// Syncache is a cache plugin.
type Syncache struct {
	Next      plugin.Handler
	cache     *Cache
	g         singleflight.Group
	eviction  time.Duration
	maxSizeMb int
}

// New returns a new instance of Syncache
func New(eviction time.Duration, maxSizeMb int) (*Syncache, error) {
	c, err := NewCache(eviction, maxSizeMb)

	return &Syncache{
		cache:     c,
		eviction:  eviction,
		maxSizeMb: maxSizeMb,
	}, err
}

// ServeDNS implements the plugin.Handler interface.
func (s *Syncache) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	key := r.Question[0].String()
	server := metrics.WithServer(ctx)
	miss := false

	v, _, shared := s.g.Do(key, func() (interface{}, error) {
		rec := NewRecorder(s, key, server)
		if entry, err := s.cache.Get(key); err == nil {
			rec.SetMsg(entry)
		} else {
			rec.NextPlugin(ctx, r)
			miss = true
		}
		return rec, nil
	})

	if miss {
		cacheMisses.WithLabelValues(server).Inc()
	} else {
		cacheHits.WithLabelValues(server, Success).Inc()
	}
	rec := v.(*Recorder)

	if rec.Rcode != dns.RcodeSuccess || rec.Err != nil || (len(rec.Msg.Answer) == 0 && rec.Msg.Question[0].Qtype == dns.TypeA) {
		log.Errorf("rcode: %v, err: %v, record: %v\n\n", rec.Rcode, rec.Err, rec.Msg)
	}

	return rec.WriteRecordedMessage(w, r, shared)
}

// Name implements the Handler interface.
func (s *Syncache) Name() string { return "syncache" }
