package syncache

import (
	"fmt"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	"github.com/mholt/caddy"
	"strconv"
	"time"
)

// init registers this plugin within the Caddy plugin framework. It uses "syncache" as the
// name, and couples it to the Action "setup".
func init() {
	caddy.RegisterPlugin("syncache", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

// setup is the function that gets called when the config parser see the token "syncache". Setup is responsible
// for parsing any extra options the archive plugin may have. The first token this function sees is "syncache".
func setup(c *caddy.Controller) error {
	s, err := parseSyncache(c)
	if err != nil {
		return plugin.Error("erratic", err)
	}

	// Add a startup function that will -- after all plugins have been loaded -- check if the
	// prometheus plugin has been used - if so we will export metrics. We can only register
	// this metric once, hence the "once.Do".
	c.OnStartup(func() error {
		once.Do(func() {
			metrics.MustRegister(c,
				cacheSize, cacheHits, cacheMisses)
		})
		return nil
	})

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		s.Next = next
		return s
	})

	return nil
}

func parseSyncache(c *caddy.Controller) (*Syncache, error) {
	eviction := defaultEviction
	maxSizeMb := defaultMaxSizeMb

	j := 0
	for c.Next() { // 'syncache'
		if j > 0 {
			return nil, plugin.ErrOnce
		}
		j++

		if len(c.RemainingArgs()) > 0 {
			return nil, c.Errf("unknown property '%s'", c.Val())
		}
		for c.NextBlock() {
			switch c.Val() {
			case "eviction":
				args := c.RemainingArgs()
				if len(args) > 1 {
					return nil, c.ArgErr()
				}

				if len(args) == 0 {
					return nil, fmt.Errorf("missing value for %q", c.ArgErr())
				}

				duration, err := time.ParseDuration(args[0])
				if err != nil {
					return nil, err
				}
				eviction = duration
			case "maxSizeMb":
				args := c.RemainingArgs()
				if len(args) > 1 {
					return nil, c.ArgErr()
				}

				if len(args) == 0 {
					return nil, fmt.Errorf("missing value for %q", c.ArgErr())
				}

				amount, err := strconv.Atoi(args[0])
				if err != nil {
					return nil, err
				}
				if amount < 0 {
					return nil, fmt.Errorf("illegal amount value given %q", args[0])
				}
				maxSizeMb = amount
			default:
				return nil, c.Errf("unknown property '%s'", c.Val())
			}
		}
	}
	return New(eviction, maxSizeMb)
}

const (
	defaultEviction  = time.Duration(1 * time.Hour)
	defaultMaxSizeMb = 1024 * 8
)
