// Package archivecache is a CoreDNS plugin that caches lookups and writes. If more than one request for the same resource
// is in flight, then they are queued to avoid redundant forwarding lookups
package archivingcache

import (
	"context"
	"errors"
	"fmt"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/nonwriter"
	"github.com/coredns/coredns/plugin/pkg/rcode"
	"github.com/coredns/coredns/plugin/pkg/response"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/forward"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/resolve"
	"google.golang.org/grpc/connectivity"
	"strings"
	"time"
)

// Define log to be a logger with the plugin name in it. This way we can just use archive.Info and
// friends to log.
var (
	log = clog.NewWithPlugin("archivingcache")
)

// ArchivingCache is a cache plugin.
type ArchivingCache struct {
	Next          plugin.Handler
	cache         *Cache
	contentWriter *ContentWriterClient
	db            *database
	now           time.Time
}

// NewArchivingCache returns a new instance of ArchivingCache
func NewArchivingCache(cache *Cache, db *database, cw *ContentWriterClient) *ArchivingCache {
	return &ArchivingCache{
		cache:         cache,
		db:            db,
		contentWriter: cw,
		now:           time.Now().UTC(),
	}
}

func (a *ArchivingCache) Ready() bool {
	return a.db.Session.IsConnected() && a.contentWriter.conn.Conn.GetState() != connectivity.Shutdown
}

// ServeDNS implements the plugin.Handler interface.
func (a *ArchivingCache) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := &request.Request{
		Req: r,
		W:   w,
	}
	// server is the address of the dns server serving the request
	server := metrics.WithServer(ctx)
	fetchStart := time.Now().UTC()
	collectionId, hasCollectionId := ctx.Value(resolve.CollectionIdKey{}).(string)
	key := state.Name() + state.Class() + state.Type()
	var msg *dns.Msg

	entry := a.get(key, server)
	if entry == nil {
		// not found in cache
		var proxyAddr string
		ctx = context.WithValue(ctx, forward.ProxyKey{}, &proxyAddr)
		nw := nonwriter.New(w)
		rc, err := plugin.NextOrFailure(a.Name(), a.Next, ctx, nw, r)
		if err != nil || rc != dns.RcodeSuccess ||
			nw.Msg == nil ||
			(len(nw.Msg.Answer) == 0 && len(nw.Msg.Question) > 0 && nw.Msg.Question[0].Qtype == dns.TypeA) {
			log.Debugf("%s: err: %v, archive: %v\n\n", rcode.ToString(rc), err, nw.Msg)
			return rc, fmt.Errorf("rc: %v, err: %v, archive: %v\n\n", rc, err, nw.Msg)
		}
		if proxyAddr == "" {
			return dns.RcodeServerFailure, errors.New("failed to get proxy address")
		}
		msg = nw.Msg

		// only cache/archive msg if collectionId is part of context
		if hasCollectionId {
		out:
			for _, answer := range msg.Answer {
				switch dnsRecord := answer.(type) {
				case *dns.A, *dns.AAAA, *dns.PTR:
					msg.Answer = []dns.RR{dnsRecord}

					// cache the response
					err := a.set(key, msg, collectionId, proxyAddr, server)
					if err != nil {
						log.Warningf("Failed to cache new response: %v, %v, %v", state.Name(), entry, err)
					}
					// archive the response
					a.archive(state, msg, collectionId, proxyAddr, fetchStart)
					break out
				}
			}
		}
	} else {
		// found in cache
		msg = entry.Msg

		if hasCollectionId {
			if len(collectionId) > 0 && !entry.HasCollectionId(collectionId) {
				entry.CollectionIds = append(entry.CollectionIds, collectionId)
				err := a.update(key, entry)
				if err != nil {
					log.Warningf("failed to update cache entry: %v", err)
				}
				a.archive(state, msg, collectionId, entry.ProxyAddr, fetchStart)
			}
		}
	}

	if err := w.WriteMsg(msg.SetReply(r)); err != nil {
		return dns.RcodeServerFailure, err
	} else {
		return dns.RcodeSuccess, nil
	}
}

func (a *ArchivingCache) update(key string, entry *CacheEntry) error {
	err := a.cache.Set(key, entry)
	if err != nil {
		return fmt.Errorf("failed to update cache entry: %v, %v, %v", key, entry, err)
	}
	return nil
}

// set caches a new record.
func (a *ArchivingCache) set(key string, msg *dns.Msg, collectionId string, proxyAddr string, server string) error {
	entry := &CacheEntry{
		Msg:       msg.Copy(),
		ProxyAddr: proxyAddr,
	}
	if len(collectionId) > 0 {
		entry.CollectionIds = append(entry.CollectionIds, collectionId)
	} else {
		log.Debugf("Caching record without collection ref: %s %v", key, entry)
	}

	mt, _ := response.Typify(msg, a.now)
	switch mt {
	// only cache the following record types
	case response.NoError, response.Delegation, response.NameError, response.NoData:
		err := a.cache.Set(key, entry)
		if err != nil {
			return fmt.Errorf("failed to cache entry: %v, %v, %v", key, entry, err)
		}
		CacheSize.WithLabelValues(server, Success).Set(float64(a.cache.Len()))
		log.Debugf("Cache set: %s, %v", key, entry)
		return nil
	default:
		return fmt.Errorf("not caching type classification: %d", mt)
	}
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

// archive stores a dns record as a WARC record and as an entry in the crawl log.
func (a *ArchivingCache) archive(state *request.Request, msg *dns.Msg, collectionId, proxyAddr string, fetchStart time.Time) {
	fetchDurationMs := (time.Now().Sub(fetchStart).Nanoseconds() + 500000) / 1000000
	requestedHost := strings.Trim(state.Name(), ".")
	payload := []byte(fmt.Sprintf("%d%02d%02d%02d%02d%02d\n%s\n",
		fetchStart.Year(), fetchStart.Month(), fetchStart.Day(),
		fetchStart.Hour(), fetchStart.Minute(), fetchStart.Second(), msg.Answer[0]))

	payload, reply, err := a.contentWriter.writeRecord(payload, fetchStart, requestedHost, proxyAddr, collectionId)
	if err != nil {
		log.Errorf("Failed to write WARC record: %v", err)
	} else {
		err := a.db.WriteCrawlLog(payload, reply.GetMeta().GetRecordMeta()[0], requestedHost, fetchStart, fetchDurationMs, proxyAddr)
		if err != nil {
			log.Error("Failed to write crawl log: %w", err)
		}
	}
}

// Name implements the Handler interface.
func (a *ArchivingCache) Name() string { return "archivingcache" }
