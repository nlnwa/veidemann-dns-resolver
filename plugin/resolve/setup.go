package resolve

import (
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/mholt/caddy"
	"strconv"
)

// init registers this plugin within the Caddy plugin framework. It uses "resolve" as the
// name, and couples it to the Action "setup".
func init() {
	caddy.RegisterPlugin("resolve", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

// setup is the function that gets called when the config parser see the token "resolve". Setup is responsible
// for parsing any extra options the resolve plugin may have. The first token this function sees is "resolve".
func setup(c *caddy.Controller) error {
	c.Next() // Ignore "archive" and give us the next token.
	args := c.RemainingArgs()

	if len(args) != 1 {
		return plugin.Error("resolve", c.ArgErr())
	}

	port, err := strconv.Atoi(args[0])

	if err != nil {
		return plugin.Error("resolve", c.Errf("DNS resolver port not a number: %v", args[1]))
	}

	var server = NewResolver(port)

	c.OnStartup(func() error {
		// Find the first configured handler
		conf := dnsserver.GetConfig(c)
		for _, d := range dnsserver.Directives {
			h := conf.Handler(d)
			if h != nil {
				server.Next = h
				break
			}
		}
		return nil
	})

	// Add a startup function that will -- after all plugins have been loaded -- check if the
	// prometheus plugin has been used - if so we will export metrics. We can only register
	// this metric once, hence the "once.Do".
	c.OnStartup(func() error {
		once.Do(func() { metrics.MustRegister(c, requestCount) })
		return nil
	})

	c.OnStartup(server.OnStartup)
	c.OnRestart(server.OnRestart)
	c.OnFinalShutdown(server.OnFinalShutdown)

	// All OK, return a nil error.
	return nil
}
