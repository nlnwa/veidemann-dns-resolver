package archivingcache

import (
	"sync"

	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
)

// requestCount exports a prometheus metric that is incremented every time a query is seen by the example plugin.
var (
	cacheSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "size",
		Help:      "The number of elements in the cache.",
	}, []string{"server", "type"})

	cacheHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "hits_total",
		Help:      "The count of cache hits.",
	}, []string{"server", "type"})

	cacheMisses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "misses_total",
		Help:      "The count of cache misses.",
	}, []string{"server"})

	requestCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "archiver",
		Name:      "request_count_total",
		Help:      "Counter of requests made.",
	}, []string{"server"})
)

var once sync.Once
