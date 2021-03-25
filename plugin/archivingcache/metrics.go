package archivingcache

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	CacheSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "size",
		Help:      "The number of elements in the cache.",
	}, []string{"server", "type"})

	CacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "hits_total",
		Help:      "The count of cache hits.",
	}, []string{"server", "type"})

	CacheMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "misses_total",
		Help:      "The count of cache misses.",
	}, []string{"server"})
)

const (
	// Success is the class for caching positive caching.
	Success = "success"
	// Denial is the class defined for negative caching.
	Denial = "denial"
)
