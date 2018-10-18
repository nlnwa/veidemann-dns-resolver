package syncache

import (
	"testing"

	"github.com/mholt/caddy"
	"time"
)

func TestSetup(t *testing.T) {
	tests := []struct {
		input     string
		shouldErr bool
		eviction  time.Duration
		maxSizeMb int
	}{
		{`syncache`, false, defaultEviction, defaultMaxSizeMb},
		{`syncache {
				eviction 10s
			}`, false, 10 * time.Second, defaultMaxSizeMb},
		{`syncache {
				maxSizeMb 1024
			}`, false, defaultEviction, 1024},
		{`syncache {
				eviction 10m
				maxSizeMb 1024
			}`, false, 10 * 60 * time.Second, 1024},

		// fails
		{`syncache example.nl {
				eviction
				maxSizeMb 1024
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`syncache example.nl {
				eviction 15s
				maxSizeMb
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`syncache example.nl {
				eviction aaa
				maxSizeMb aaa
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`syncache 0 example.nl`, true, defaultEviction, defaultMaxSizeMb},
		{`syncache -1 example.nl`, true, defaultEviction, defaultMaxSizeMb},
		{`syncache 1 example.nl {
				positive 0
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`syncache 1 example.nl {
				positive 0
				prefetch -1
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`syncache 1 example.nl {
				prefetch 0 blurp
			}`, true, defaultEviction, defaultMaxSizeMb},
		{`syncache
		  syncache`, true, defaultEviction, defaultMaxSizeMb},
	}
	for i, test := range tests {
		c := caddy.NewTestController("dns", test.input)
		ca, err := parseSyncache(c)
		if test.shouldErr && err == nil {
			t.Errorf("Test %v: Expected error but found nil", i)
			continue
		} else if !test.shouldErr && err != nil {
			t.Errorf("Test %v: Expected no error but found error: %v", i, err)
			continue
		}
		if test.shouldErr && err != nil {
			continue
		}

		if ca.eviction != test.eviction {
			t.Errorf("Test %v: Expected eviction %v but found: %v", i, test.eviction, ca.eviction)
		}
		if ca.maxSizeMb != test.maxSizeMb {
			t.Errorf("Test %v: Expected maxSizeMb %v but found: %v", i, test.maxSizeMb, ca.maxSizeMb)
		}
	}
}
