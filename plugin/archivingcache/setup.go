package archivingcache

import (
	"fmt"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/forward"
	"github.com/coredns/coredns/plugin/metrics"
	"github.com/mholt/caddy"
	"github.com/nlnwa/veidemann-dns-resolver/iputil"
	"gopkg.in/gorethink/gorethink.v4"
	"net"
	"strconv"
	"time"
)

// init registers this plugin within the Caddy plugin framework. It uses "syncache" as the
// name, and couples it to the Action "setup".
func init() {
	caddy.RegisterPlugin("archivingcache", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

// setup is the function that gets called when the config parser see the token "syncache". Setup is responsible
// for parsing any extra options the archive plugin may have. The first token this function sees is "syncache".
func setup(c *caddy.Controller) error {
	a, err := parseArchivingCache(c)
	if err != nil {
		return plugin.Error("erratic", err)
	}

	// Add a startup function that will -- after all plugins have been loaded -- check if the
	// prometheus plugin has been used - if so we will export metrics. We can only register
	// this metric once, hence the "once.Do".
	c.OnStartup(func() error {
		once.Do(func() {
			metrics.MustRegister(c,
				cacheSize, cacheHits, cacheMisses, requestCount)
		})

		return a.OnStartup()
	})

	c.OnShutdown(func() error {
		return a.OnShutdown()
	})

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		a.Next = next
		return a
	})

	return nil
}

// OnStartup starts a goroutines for all proxies.
func (a *ArchivingCache) OnStartup() (err error) {
	addr := []string{net.JoinHostPort(a.UpstreamIP, a.UpstreamPort)}
	a.forward = forward.NewLookup(addr)
	log.Infof("Archiver is using upstream DNS at: %s", addr)

	return a.Connection.connect()
}

// OnShutdown stops all configured proxies.
func (a *ArchivingCache) OnShutdown() error {
	a.forward.Close()

	if a.Connection.contentWriterClient != nil {
		if err := a.Connection.contentWriterClientConn.Close(); err != nil {
			log.Errorf("Could not disconnect from Content Writer: %v", err)
		}
	}

	if s, ok := a.Connection.dbSession.(*gorethink.Session); ok {
		if err := s.Close(); err != nil {
			log.Errorf("Could not disconnect from database: %v", err)
		}
	}
	return nil
}

// Close is a synonym for OnShutdown().
func (a *ArchivingCache) Close() { a.OnShutdown() }

func parseArchivingCache(c *caddy.Controller) (*ArchivingCache, error) {
	eviction := defaultEviction
	maxSizeMb := defaultMaxSizeMb
	var contentWriterHost string
	var contentWriterPort int
	var dbHost string
	var dbPort int
	var dbUser string
	var dbPassword string
	var dbName string
	var upstreamIP string
	var upstreamPort string

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
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					duration, err := time.ParseDuration(arg)
					if err != nil {
						return nil, err
					}
					eviction = duration
				}
			case "maxSizeMb":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					amount, err := strconv.Atoi(arg)
					if err != nil {
						return nil, err
					}
					if amount < 0 {
						return nil, fmt.Errorf("illegal amount value given %q", arg)
					}
					maxSizeMb = amount
				}
			case "contentWriterHost":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					contentWriterHost = arg
				}
			case "contentWriterPort":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					contentWriterPort, err = strconv.Atoi(arg)
					if err != nil {
						return nil, err
					}
				}
			case "dbHost":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					dbHost = arg
				}
			case "dbPort":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					dbPort, err = strconv.Atoi(arg)
					if err != nil {
						return nil, err
					}
				}
			case "dbUser":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					dbUser = arg
				}
			case "dbPassword":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					dbPassword = arg
				}
			case "dbName":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					dbName = arg
				}
			case "upstreamDNS":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					if upstreamIP, upstreamPort, err = iputil.IPAndPortForAddr(arg, 53); err != nil {
						return nil, plugin.Error("archiver", c.Errf("Unable to resolve upstream DNS server: %v", err))
					}
				}
			default:
				return nil, c.Errf("unknown property '%s'", c.Val())
			}
		}
	}
	connection := NewConnection(dbHost, dbPort, dbUser, dbPassword, dbName, contentWriterHost, contentWriterPort)
	return NewArchivingCache(eviction, maxSizeMb, upstreamIP, upstreamPort, connection)
}

func getArg(c *caddy.Controller) (string, error) {
	args := c.RemainingArgs()
	if len(args) > 1 {
		return "", c.ArgErr()
	}

	if len(args) == 0 {
		return "", fmt.Errorf("missing value for %q", c.ArgErr())
	}

	return args[0], nil
}

const (
	defaultEviction  = time.Duration(1 * time.Hour)
	defaultMaxSizeMb = 1024 * 8
)
