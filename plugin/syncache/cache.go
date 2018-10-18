package syncache

import (
	"github.com/allegro/bigcache"
	"time"
	"github.com/miekg/dns"
	"github.com/coredns/coredns/plugin/pkg/response"
)

// Cache is a wrapper around *bigcache.BigCache which takes care of marshalling and unmarshalling of dns.Msg.
type Cache struct {
	cache *bigcache.BigCache
	now   func() time.Time
}

// NewCache creates a new Cache
func NewCache(lifeWindow time.Duration, maxSizeMb int) (*Cache, error) {
	config := bigcache.Config{
		// number of shards (must be a power of 2)
		Shards: 1024,
		// time after which entry can be evicted
		LifeWindow: lifeWindow,
		// rps * lifeWindow, used only in initial memory allocation
		MaxEntriesInWindow: 1000 * 10 * 60,
		// max entry size in bytes, used only in initial memory allocation
		MaxEntrySize: 200,
		// prints information about additional memory allocation
		Verbose: true,
		// cache will not allocate more memory than this limit, value in MB
		// if value is reached then the oldest entries can be overridden for the new ones
		// 0 value means no size limit
		HardMaxCacheSize: maxSizeMb,
		// callback fired when the oldest entry is removed because of its expiration time or no space left
		// for the new entry, or because delete was called. A bitmask representing the reason will be returned.
		// Default value is nil which means no callback and it prevents from unwrapping the oldest entry.
		OnRemove:    nil,
		CleanWindow: 0,
	}

	c, err := bigcache.NewBigCache(config)
	if err != nil {
		return nil, err
	}

	return &Cache{
		cache: c,
		now:   time.Now,
	}, nil
}

// Put writes a DNS response to the cache
func (c *Cache) Put(key string, server string, r *dns.Msg) error {
	mt, _ := response.Typify(r, c.now().UTC())
	switch mt {
	case response.NoError, response.Delegation, response.NameError, response.NoData:
		entry, err := r.Pack()
		if err != nil {
			return err
		}

		err = c.cache.Set(key, entry)
		log.Debugf("Record written to cache %s, %v", key, r)
		cacheSize.WithLabelValues(server, Success).Set(float64(c.cache.Len()))
		return err

	case response.OtherError:
		// don't cache these
	default:
		log.Warningf("Caching called with unknown classification: %d", mt)
	}
	return nil
}

// Get reads a DNS response from the cahce. It returns an EntryNotFoundError when no entry exists for the given key.
func (c *Cache) Get(key string) (*dns.Msg, error) {
	entry, err := c.cache.Get(key)
	if err != nil {
		return nil, err
	}
	return entryToMsg(entry)
}

func entryToMsg(entry []byte) (*dns.Msg, error) {
	m1 := new(dns.Msg)
	err := m1.Unpack(entry)
	if err != nil {
		return nil, err
	}

	m1.Authoritative = false
	return m1, nil
}

const (
	// Success is the class for caching positive caching.
	Success = "success"
	// Denial is the class defined for negative caching.
	Denial = "denial"
)
