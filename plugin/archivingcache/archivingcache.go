// Package archivecache is a CoreDNS plugin that caches lookups and writes. If more than one request for the same resource
// is in flight, then they are queued to avoid redundant forwarding lookups
package archivingcache

import (
	"context"
	"fmt"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/nonwriter"
	"github.com/coredns/coredns/plugin/pkg/response"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/forward"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/resolve"
	"golang.org/x/sync/singleflight"
	"net"
	"strings"
	"time"
)

// Define log to be a logger with the plugin name in it. This way we can just use archive.Info and
// friends to log.
var (
	log = clog.NewWithPlugin("archivingcache")
)

// ArchivingCache is a CoreDNS plugin.
type ArchivingCache struct {
	Next          plugin.Handler
	cache         *Cache
	contentWriter *ContentWriterClient
	logWriter     *LogWriterClient
	now           time.Time
	singleflight.Group
}

// NewArchivingCache returns a new instance of ArchivingCache
func NewArchivingCache(cache *Cache, lw *LogWriterClient, cw *ContentWriterClient) *ArchivingCache {
	return &ArchivingCache{
		cache:         cache,
		logWriter:     lw,
		contentWriter: cw,
		now:           time.Now().UTC(),
	}
}

func (a *ArchivingCache) Ready() bool {
	return a.logWriter.Ready() && a.contentWriter.Ready()
}

// ServeDNS implements the plugin.Handler interface.
func (a *ArchivingCache) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	val, err, shared := a.Do(r.Question[0].String(), func() (interface{}, error) {
		rec := NewRecorder(w)

		rc, err := a.serveDNS(ctx, rec, r)
		if err != nil {
			rec.Rcode = rc
		}
		return rec, err
	})
	rec := val.(*Recorder)
	if err != nil {
		return rec.Rcode, err
	}

	msg := rec.Msg
	if shared {
		msg = msg.Copy().SetRcode(r, msg.Rcode)
	}

	w.WriteMsg(msg)
	return 0, nil
}

// ServeDNS implements the plugin.Handler interface.
func (a *ArchivingCache) serveDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := &request.Request{
		Req: r,
		W:   w,
	}
	// server is the address of the dns server serving the request
	server := metrics.WithServer(ctx)
	fetchStart := time.Now().UTC()
	// collectionId and executionId is set by the resolver plugin
	collectionId, hasCollectionId := ctx.Value(resolve.CollectionIdKey{}).(string)
	executionId, _ := ctx.Value(resolve.ExecutionIdKey{}).(string)
	// cache key
	key := state.Name() + state.Type()

	var msg *dns.Msg

	entry := a.get(key, server)
	if entry == nil {
		var proxyAddr string
		ctx = context.WithValue(ctx, forward.ProxyKey{}, &proxyAddr)

		nw := nonwriter.New(w)

		rc, err := plugin.NextOrFailure(a.Name(), a.Next, ctx, nw, r)
		if err != nil {
			return rc, err
		}

		msg = nw.Msg

		if hasCollectionId {
			proxyIpAddr, err := parseHostPortOrIP(proxyAddr)
			if err != nil {
				log.Errorf("failed to parse proxy address \"%s\" as host:port pair or IP address: %v", proxyAddr, err)
			}
			mt, _ := response.Typify(msg, a.now)
			if err := a.set(key, mt, msg, collectionId, proxyIpAddr, server); err != nil {
				log.Errorf("%v: %v", key, err)
			}
			if err = a.archive(state, mt, msg, executionId, collectionId, proxyIpAddr, fetchStart); err != nil {
				log.Errorf("%v: %v", key, err)
			}
		}
	} else {
		msg = entry.Msg.SetRcode(r, entry.Msg.Rcode)

		if hasCollectionId && collectionId != "" && !entry.HasCollectionId(collectionId) {
			if err := a.update(key, entry, collectionId); err != nil {
				log.Errorf("%v: %v", key, err)
			}
			mt, _ := response.Typify(msg, a.now)
			if err := a.archive(state, mt, msg, executionId, collectionId, entry.ProxyAddr, fetchStart); err != nil {
				log.Errorf("%v: %v", key, err)
			}
		}
	}

	w.WriteMsg(msg)
	return 0, nil
}

func (a *ArchivingCache) update(key string, entry *CacheEntry, collectionId string) error {
	entry.CollectionIds = append(entry.CollectionIds, collectionId)

	err := a.cache.Set(key, entry)
	if err != nil {
		return fmt.Errorf("failed to update cache entry: %v, %v, %w", key, entry, err)
	}

	return nil
}

// set caches a new record.
func (a *ArchivingCache) set(key string, t response.Type, msg *dns.Msg, collectionId string, proxyAddr string, server string) error {
	switch t {
	case response.NoError, response.NameError, response.Delegation, response.NoData:
		// cache these response types
	default:
		return nil
	}

	entry := &CacheEntry{
		Msg:       msg.Copy(),
		ProxyAddr: proxyAddr,
		CollectionIds: []string{collectionId},
	}

	err := a.cache.Set(key, entry)
	if err != nil {
		return fmt.Errorf("failed to cache entry: %s: %w", key, err)
	}
	log.Debugf("Cache set: %s, %v", key, entry)

	CacheSize.WithLabelValues(server, Success).Set(float64(a.cache.Len()))

	return nil
}

// get returns a cached entry if it exists.
func (a *ArchivingCache) get(key string, server string) *CacheEntry {
	entry, err := a.cache.Get(key)
	if err != nil {
		log.Debugf("Cache miss: %s", key)
		CacheMisses.WithLabelValues(server).Inc()
		return nil
	}
	log.Debugf("Cache hit: %s", key)
	CacheHits.WithLabelValues(server, Success).Inc()
	return entry
}

// archive writes a WARC record and a crawl log.
func (a *ArchivingCache) archive(state *request.Request, t response.Type, msg *dns.Msg, executionId string, collectionId string, proxyAddr string, fetchStart time.Time) error {
	if t != response.NoError || len(msg.Answer) == 0 {
		return nil
	}

	fetchDurationMs := (time.Now().Sub(fetchStart).Nanoseconds() + 500000) / 1000000
	requestedHost := strings.TrimSuffix(state.Name(), ".")

	payload := []byte(fmt.Sprintf("%d%02d%02d%02d%02d%02d\n%s\n",
		fetchStart.Year(), fetchStart.Month(), fetchStart.Day(),
		fetchStart.Hour(), fetchStart.Minute(), fetchStart.Second(), msg.Answer[0]))
	size := len(payload)

	reply, err := a.contentWriter.WriteRecord(payload, fetchStart, requestedHost, proxyAddr, executionId, collectionId)
	if err != nil {
		return fmt.Errorf("failed to write WARC record: %w", err)
	}

	err = a.logWriter.WriteCrawlLog(reply.GetMeta().GetRecordMeta()[0], size, requestedHost, fetchStart, fetchDurationMs, proxyAddr, executionId)
	if err != nil {
		return fmt.Errorf("failed to write crawl log: %w", err)
	}

	return nil
}

// Name implements the Handler interface.
func (a *ArchivingCache) Name() string { return "archivingcache" }

// parseHostPortOrIP parses a host:port pair or IP address into an IP address.
func parseHostPortOrIP(addr string) (string, error) {
	// Assume the proxy address is a host:port pair
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Try to parse proxy address as IP address
		ip := net.ParseIP(addr)
		if ip == nil {
			return "", err
		} else {
			return ip.String(), nil
		}
	}
	return host, nil
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
