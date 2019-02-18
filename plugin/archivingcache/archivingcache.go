// Package syncache is a CoreDNS plugin that caches lookups. If more than one request for the same resource
// is in flight, then they are queued to avoid redundant forwarding lookups
package archivingcache

import (
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/forward"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/resolve"
	configV1 "github.com/nlnwa/veidemann-api-go/config/v1"

	"context"
	"github.com/coredns/coredns/plugin/metrics"
	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
	"time"
)

// Define log to be a logger with the plugin name in it. This way we can just use log.Info and
// friends to log.
var (
	log = clog.NewWithPlugin("archivingcache")
)

// Syncache is a cache plugin.
type ArchivingCache struct {
	Next         plugin.Handler
	cache        *Cache
	g            singleflight.Group
	eviction     time.Duration
	maxSizeMb    int
	UpstreamIP   string
	UpstreamPort string
	Connection   *Connection
	forward      *forward.Forward
}

// NewArchivingCache returns a new instance of Syncache
func NewArchivingCache(eviction time.Duration, maxSizeMb int, UpstreamIP string, UpstreamPort string, Connection *Connection) (*ArchivingCache, error) {
	c, err := NewCache(eviction, maxSizeMb)

	return &ArchivingCache{
		cache:        c,
		eviction:     eviction,
		maxSizeMb:    maxSizeMb,
		UpstreamIP:   UpstreamIP,
		UpstreamPort: UpstreamPort,
		Connection:   Connection,
	}, err
}

// ServeDNS implements the plugin.Handler interface.
func (a *ArchivingCache) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	// Debug log that we've have seen the query. This will only be shown when the debug plugin is loaded.
	log.Debugf("Got request: %v %v %v", r.Question[0].Name, dns.ClassToString[r.Question[0].Qclass], dns.TypeToString[r.Question[0].Qtype])

	// Export metric with the Server label set to the current Server handling the request.
	requestCount.WithLabelValues(metrics.WithServer(ctx)).Inc()

	key := r.Question[0].String()
	server := metrics.WithServer(ctx)
	miss := false

	v, _, shared := a.g.Do(key, func() (interface{}, error) {
		collectionRef := getCollectionRef(r)
		rec := NewRecorder(a, key, server, a.Connection, r.Question[0].Name, a.UpstreamIP, collectionRef)
		if entry, err := a.cache.Get(key); err == nil {
			log.Debugf("Found request in cache: %v = %v", key, entry)
			rec.SetMsg(entry.r)

			if collectionRef != nil && !entry.HasCollectionId(collectionRef.Id) {
				log.Debugf("New collection for cached request: %v = %v", key, entry)
				if l := len(entry.AddCollectionId(collectionRef.Id)); l == 1 {
					log.Debugf("Request '%v' found in cache, but is previously not stored. " +
						"Response will be stored in collection '%v'", key, collectionRef.Id)
				}

				rec.FetchDurationMs = (time.Now().UTC().Sub(rec.FetchStart).Nanoseconds() + 500000) / 1000000
				reply := rec.writeContentwriter(entry.r.Answer[0].String())
				rec.writeCrawlLog(reply.GetMeta().GetRecordMeta()[0])
				a.cache.Put(key, server, entry)
			}
		} else {
			log.Debugf("No request found in cache for: %v", key)
			a.forward.ServeDNS(ctx, rec, r)
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

func getCollectionRef(r *dns.Msg) *configV1.ConfigRef {
	for i, v := range r.Extra {
		if c, ok := v.(*resolve.COLLECTION); ok {
			r.Extra = append(r.Extra[:i], r.Extra[i+1:]...)
			return c.CollectionRef
		}
	}

	return nil
}

// Name implements the Handler interface.
func (a *ArchivingCache) Name() string { return "archivingcache" }
