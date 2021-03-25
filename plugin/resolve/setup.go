package resolve

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

// init registers this plugin within the Caddy plugin framework. It uses "resolve" as the
// name, and couples it to the Action "setup".
func init() {
	plugin.Register("resolve", setup)
}

// setup is the function that gets called when the config parser see the token "resolve". Setup is responsible
// for parsing any extra options the resolve plugin may have. The first token this function sees is "resolve".
func setup(c *caddy.Controller) error {
	c.Next() // Ignore "resolve" and give us the next token.
	args := c.RemainingArgs()

	if len(args) != 1 {
		return plugin.Error("resolve", c.ArgErr())
	}

	addr := args[0]
	resolver := NewResolver(addr)

	c.OnStartup(func() error {
		// Find the first configured handler
		conf := dnsserver.GetConfig(c)
		for _, d := range dnsserver.Directives {
			h := conf.Handler(d)
			if h != nil {
				resolver.Next = h
				break
			}
		}
		return nil
	})
	c.OnStartup(resolver.OnStartup)
	c.OnRestart(resolver.OnStop)
	c.OnFinalShutdown(resolver.OnStop)

	return nil
}
