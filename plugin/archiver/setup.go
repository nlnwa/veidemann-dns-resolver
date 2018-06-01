package archiver

import (
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"

	"github.com/mholt/caddy"
	"strconv"
	"fmt"
	"github.com/coredns/coredns/plugin/forward"
)

// init registers this plugin within the Caddy plugin framework. It uses "example" as the
// name, and couples it to the Action "setup".
func init() {
	caddy.RegisterPlugin("archiver", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

// setup is the function that gets called when the config parser see the token "archive". Setup is responsible
// for parsing any extra options the archive plugin may have. The first token this function sees is "archive".
func setup(c *caddy.Controller) error {
	c.Next() // Ignore "archive" and give us the next token.
	args := c.RemainingArgs()

	if len(args) != 2 {
		return plugin.Error("archiver", c.ArgErr())
	}

	host := args[0]
	port, err := strconv.Atoi(args[1])

	if err != nil {
		return plugin.Error("archiver", c.Errf("Port not a number: %v", args[1]))
	}

	// Add a startup function that will -- after all plugins have been loaded -- check if the
	// prometheus plugin has been used - if so we will export metrics. We can only register
	// this metric once, hence the "once.Do".
	c.OnStartup(func() error {
		once.Do(func() { metrics.MustRegister(c, requestCount) })
		return nil
	})

	addr := fmt.Sprintf("%s:%d", host, port)
	log.Debugf("Archiver is using contentwriter at: %s", addr)
	a := &Archiver{Host: host, Port: port, Addr: addr}

	c.OnStartup(func() error {
		upstream, err := GetUpstreamDns(c)
		if err != nil {
			return err
		}

		a.UpstreamHostIp = upstream
		return nil
	})

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		a.Next = next
		return a
	})

	// All OK, return a nil error.
	return nil
}

// Get the first upstream server configured for the froward plugin.
// Ideally we would like to know which of the upstream servers is actually used for a request, but that is not possible
// with the forward plugin, so this is the best we can do, and it is correct if only one upstream server is configured.
func GetUpstreamDns(c *caddy.Controller) (string, error) {
	m := dnsserver.GetConfig(c).Handler("forward")
	if m == nil {
		return "", fmt.Errorf("archiver requires forward plugin")
	}

	x, ok := m.(*forward.Forward)
	if !ok {
		return "", fmt.Errorf("archiver requires forward plugin")
	}

	u := x.List()
	if len(u) == 0 {
		return "", fmt.Errorf("archiver requires forward plugin to be configured with an upstream server")
	}
	_, h, _, err := dnsserver.SplitProtocolHostPort(u[0].Addr())

	if err != nil {
		return "", err
	}

	return h, nil
}
