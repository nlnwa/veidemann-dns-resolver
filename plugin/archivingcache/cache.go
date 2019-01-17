package archivingcache

import (
	"bytes"
	"fmt"
	"github.com/allegro/bigcache"
	"github.com/coredns/coredns/plugin/pkg/response"
	"github.com/miekg/dns"
	"time"
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
func (c *Cache) Put(key string, server string, cacheEntry *CacheEntry) error {
	mt, _ := response.Typify(cacheEntry.r, c.now().UTC())
	switch mt {
	case response.NoError, response.Delegation, response.NameError, response.NoData:
		entry, err := cacheEntry.pack()
		if err != nil {
			return err
		}

		err = c.cache.Set(key, entry)
		log.Debugf("Record written to cache %s, %v", key, cacheEntry)
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
func (c *Cache) Get(key string) (*CacheEntry, error) {
	data, err := c.cache.Get(key)
	if err != nil {
		return nil, err
	}
	entry := new(CacheEntry)
	err = entry.unpack(data)
	if err != nil {
		return nil, err
	}

	return entry, nil
}

type CacheEntry struct {
	collectionIds []string
	r             *dns.Msg
}

func (ce *CacheEntry) pack() ([]byte, error) {
	var packed []byte
	for _, v := range ce.collectionIds {
		packed = append(packed, v...)
		packed = append(packed, ':')
	}
	packed = append(packed, ':')
	entry, err := ce.r.Pack()
	if err != nil {
		return nil, err
	}
	packed = append(packed, entry...)
	return packed, nil
}

func (ce *CacheEntry) unpack(entry []byte) error {
	for entry[0] != ':' {
		idx := bytes.IndexByte(entry, ':')
		if idx == -1 {
			return fmt.Errorf("Error unpacking collections from cache entry")
		}
		ce.collectionIds = append(ce.collectionIds, string(entry[:idx]))
		entry = entry[idx+1:]
	}
	entry = entry[1:]
	m := new(dns.Msg)
	err := m.Unpack(entry)
	if err != nil {
		return err
	}

	m.Authoritative = false
	ce.r = m
	return nil
}

func (ce *CacheEntry) AddCollectionId(collectionId string) []string {
	ce.collectionIds = append(ce.collectionIds, collectionId)
	return ce.collectionIds
}

func (ce *CacheEntry) HasCollectionId(collectionId string) bool {
	if collectionId == "" {
		return true
	}
	for _, cid := range ce.collectionIds {
		if collectionId == cid {
			return true
		}
	}
	return false
}

func (ce *CacheEntry) String() string {
	return fmt.Sprintf("CollectionIds: %v, dns.Msg: %v", ce.collectionIds, ce.r)
}

const (
	// Success is the class for caching positive caching.
	Success = "success"
	// Denial is the class defined for negative caching.
	Denial = "denial"
)
