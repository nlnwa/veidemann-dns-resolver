package main

import (
	_ "github.com/coredns/coredns/plugin/cache"
	_ "github.com/coredns/coredns/plugin/log"
	_ "github.com/coredns/coredns/plugin/metrics"
	_ "github.com/coredns/coredns/plugin/whoami"
	_ "github.com/coredns/coredns/plugin/health"
	_ "github.com/nlnwa/veidemann-dns-resolver/plugin/archiver"
	_ "github.com/nlnwa/veidemann-dns-resolver/plugin/resolve"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/coremain"
)

var directives = []string{
	"health",
	"prometheus",
	"resolve",
	"log",
	"cache",
	"archiver",
	"whoami",
}

func init() {
	dnsserver.Directives = directives
}

func main() {
	coremain.Run()
}
