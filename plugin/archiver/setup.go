package archiver

import (
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"

	"github.com/mholt/caddy"
	"strconv"
	"github.com/coredns/coredns/plugin/forward"
	"gopkg.in/gorethink/gorethink.v4"
)

// init registers this plugin within the Caddy plugin framework. It uses "archiver" as the
// name, and couples it to the Action "setup".
func init() {
	caddy.RegisterPlugin("archiver", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

// setup is the function that gets called when the config parser see the token "archiver". Setup is responsible
// for parsing any extra options the archiver plugin may have. The first token this function sees is "archiver".
func setup(c *caddy.Controller) error {
	c.Next() // Ignore "archiver" and give us the next token.
	args := c.RemainingArgs()

	if len(args) != 7 {
		return plugin.Error("archiver", c.ArgErr())
	}

	contentWriterHost := args[0]
	contentWriterPort, err := strconv.Atoi(args[1])
	if err != nil {
		return plugin.Error("archiver", c.Errf("Content Writer Port not a number: %v", args[1]))
	}
	dbHost := args[2]
	dbPort, err := strconv.Atoi(args[3])
	if err != nil {
		return plugin.Error("archiver", c.Errf("Database Port not a number: %v", args[3]))
	}
	dbUser := args[4]
	dbPassword := args[5]
	database := "veidemann"

	a := &Archiver{
		Connection:     NewConnection(dbHost, dbPort, dbUser, dbPassword, database, contentWriterHost, contentWriterPort),
		UpstreamHostIp: args[6],
	}

	// Add a startup function that will -- after all plugins have been loaded -- check if the
	// prometheus plugin has been used - if so we will export metrics. We can only register
	// this metric once, hence the "once.Do".
	c.OnStartup(func() error {
		once.Do(func() { metrics.MustRegister(c, requestCount) })
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

	// All OK, return a nil error.
	return nil
}

// OnStartup starts a goroutines for all proxies.
func (a *Archiver) OnStartup() (err error) {
	addr := []string{a.UpstreamHostIp}
	a.forward = forward.NewLookup(addr)

	return a.Connection.connect()
}

// OnShutdown stops all configured proxies.
func (a *Archiver) OnShutdown() error {
	a.forward.Close()

	a.Connection.contentWriterClientConn.Close()
	if s, ok := a.Connection.dbSession.(*gorethink.Session); ok {
		s.Close()
	}
	return nil
}

// Close is a synonym for OnShutdown().
func (a *Archiver) Close() { a.OnShutdown() }
