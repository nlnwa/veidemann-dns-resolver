package archivingcache

import (
	"testing"

	"github.com/coredns/caddy"
	"time"
)

func TestSetup(t *testing.T) {
	tests := []struct {
		input     string
		shouldErr bool
		eviction  time.Duration
		maxSizeMb int
	}{
		{`archivingcache`, false, defaultEviction, defaultMaxSizeMb},
		{`archivingcache {
				eviction 10s
			}`, false, 10 * time.Second, defaultMaxSizeMb},
		{`archivingcache {
				maxSizeMb 1024
			}`, false, defaultEviction, 1024},
		{`archivingcache {
				eviction 10m
				maxSizeMb 1024
			}`, false, 10 * 60 * time.Second, 1024},
		{`archivingcache {
				contentWriterHost cwHost
			}`, false, defaultEviction, defaultMaxSizeMb},

		// fails
		{`archivingcache example.nl {
				eviction
				maxSizeMb 1024
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`archivingcache example.nl {
				eviction 15s
				maxSizeMb
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`archivingcache example.nl {
				eviction aaa
				maxSizeMb aaa
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`archivingcache 0 example.nl`, true, defaultEviction, defaultMaxSizeMb},
		{`archivingcache -1 example.nl`, true, defaultEviction, defaultMaxSizeMb},
		{`archivingcache 1 example.nl {
				positive 0
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`archivingcache 1 example.nl {
				positive 0
				prefetch -1
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`archivingcache 1 example.nl {
				prefetch 0 blurp
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`archivingcache
		  archivingcache`, true, defaultEviction, defaultMaxSizeMb},
	}
	for i, test := range tests {
		c := caddy.NewTestController("dns", test.input)
		_, err := parseArchivingCache(c)
		if test.shouldErr && err == nil {
			t.Errorf("Test %v: Expected error but found nil", i)
			continue
		} else if !test.shouldErr && err != nil {
			t.Errorf("Test %v: Expected no error but found error: %v", i, err)
			continue
		}
	}
}
