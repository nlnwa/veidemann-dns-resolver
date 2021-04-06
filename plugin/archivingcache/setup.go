package archivingcache

import (
	"fmt"
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/nlnwa/veidemann-log-service/pkg/logclient"
	"strconv"
	"time"
)

// init registers this plugin within the Caddy plugin framework. It uses "archivingcache" as the
// name, and couples it to the Action "setup".
func init() {
	plugin.Register("archivingcache", setup)
}

// setup is the function that gets called when the config parser see the token "archivingcache". Setup is responsible
// for parsing any extra options the archive plugin may have. The first token this function sees is "archivingcache".
func setup(c *caddy.Controller) error {
	a, err := parseArchivingCache(c)
	if err != nil {
		return plugin.Error("archivingcache", err)
	}

	c.OnStartup(a.OnStartup)
	c.OnShutdown(a.Close)

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		a.Next = next
		return a
	})

	return nil
}

// OnStartup connects to content writer and log writer.
func (a *ArchivingCache) OnStartup() error {
	if err := a.contentWriter.connect(); err != nil {
		return plugin.Error("archivingcache", fmt.Errorf("failed to connect to contentWriter: %w", err))
	}
	log.Infof("Connected to contentWriter at: %s", a.contentWriter.Addr())

	if err := a.logClient.Connect(); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	log.Infof("Connected to log client at: %s", a.logClient.Addr())

	return nil
}

// Close closes connections to content writer and log writer.
func (a *ArchivingCache) Close() error {
	if err := a.contentWriter.disconnect(); err != nil {
		log.Errorf("Error disconnecting from content writer: %v", err)
	}
	if err := a.logClient.Close(); err != nil {
		log.Errorf("Error disconnecting from log client: %v", err)
	}
	return nil
}

func parseArchivingCache(c *caddy.Controller) (*ArchivingCache, error) {
	eviction := defaultEviction
	maxSizeMb := defaultMaxSizeMb
	var contentWriterHost string
	var contentWriterPort int
	var logHost string
	var logPort int

	j := 0
	for c.Next() { // 'archivingcache'
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
			case "logHost":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					logHost = arg
				}
			case "logPort":
				if arg, err := getArg(c); err != nil {
					return nil, err
				} else {
					logPort, err = strconv.Atoi(arg)
					if err != nil {
						return nil, err
					}
				}
			default:
				return nil, c.Errf("unknown property '%s'", c.Val())
			}
		}
	}

	ca, err := NewCache(eviction, maxSizeMb)
	if err != nil {
		return nil, err
	}
	logClient := logclient.New(
		logclient.WithHost(logHost),
		logclient.WithPort(logPort),
	)
	cw := NewContentWriterClient(contentWriterHost, contentWriterPort)
	return NewArchivingCache(ca, logClient, cw), nil
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
	defaultEviction  = 1 * time.Hour
	defaultMaxSizeMb = 1024 * 8
)
