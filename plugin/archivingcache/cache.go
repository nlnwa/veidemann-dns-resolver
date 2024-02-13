package archivingcache

import (
	"bytes"
	"fmt"
	"time"

	"github.com/allegro/bigcache"
	"github.com/miekg/dns"
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

func (c *Cache) Len() int {
	return c.cache.Len()
}

// Set writes a DNS response to the cache
func (c *Cache) Set(key string, entry *CacheEntry) error {
	e, err := entry.pack()
	if err != nil {
		return err
	}
	err = c.cache.Set(key, e)
	return err
}

// Get reads a DNS response from the cache. It returns an EntryNotFoundError when no entry exists for the given key.
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

func (c *Cache) Reset() error {
	return c.cache.Reset()
}

type CacheEntry struct {
	ProxyAddr     string
	CollectionIds []string
	Msg           *dns.Msg
}

func (ce *CacheEntry) pack() ([]byte, error) {
	var packed []byte

	// proxy addr
	if len(ce.ProxyAddr) > 0 {
		packed = append(packed, ce.ProxyAddr...)
		packed = append(packed, '|')
	}

	// collection ids
	for _, v := range ce.CollectionIds {
		packed = append(packed, v...)
		packed = append(packed, ':')
	}
	packed = append(packed, ':')

	// dns message
	entry, err := ce.Msg.Pack()
	if err != nil {
		return nil, err
	}
	packed = append(packed, entry...)
	return packed, nil
}

func (ce *CacheEntry) unpack(entry []byte) error {
	// proxy address
	idx := bytes.IndexByte(entry, '|')
	if idx != -1 {
		ce.ProxyAddr = string(entry[:idx])
		entry = entry[idx+1:]
	}

	// collection ids
	for entry[0] != ':' {
		idx := bytes.IndexByte(entry, ':')
		if idx == -1 {
			return fmt.Errorf("error unpacking collections from cache entry")
		}
		ce.CollectionIds = append(ce.CollectionIds, string(entry[:idx]))
		entry = entry[idx+1:]
	}
	entry = entry[1:]

	// dns message
	m := new(dns.Msg)
	err := m.Unpack(entry)
	if err != nil {
		return err
	}

	m.Authoritative = false
	ce.Msg = m
	return nil
}

func (ce *CacheEntry) AddCollectionId(collectionId string) []string {
	ce.CollectionIds = append(ce.CollectionIds, collectionId)
	return ce.CollectionIds
}

func (ce *CacheEntry) HasCollectionId(collectionId string) bool {
	if collectionId == "" {
		return true
	}
	for _, cid := range ce.CollectionIds {
		if cid == collectionId {
			return true
		}
	}
	return false
}

func (ce *CacheEntry) String() string {
	return fmt.Sprintf("proxy: %s, collections: %v", ce.ProxyAddr, ce.CollectionIds)
}
