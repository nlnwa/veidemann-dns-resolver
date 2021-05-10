// Package main is written according to https://coredns.io/2017/07/25/compile-time-enabling-or-disabling-plugins/#build-with-external-golang-source-code
package main

import (
	_ "github.com/coredns/coredns/plugin/debug"
	_ "github.com/coredns/coredns/plugin/errors"
	_ "github.com/coredns/coredns/plugin/log"
	_ "github.com/coredns/coredns/plugin/loop"
	_ "github.com/coredns/coredns/plugin/metrics"
	_ "github.com/coredns/coredns/plugin/ready"
	_ "github.com/coredns/coredns/plugin/reload"

	_ "github.com/nlnwa/veidemann-dns-resolver/plugin/archivingcache"
	_ "github.com/nlnwa/veidemann-dns-resolver/plugin/forward"
	_ "github.com/nlnwa/veidemann-dns-resolver/plugin/resolve"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/coremain"
)

// Directives are registered in the order they should be
// executed.
//
// Ordering is VERY important. Every plugin will
// feel the effects of all other plugin below
// (after) them during a request, but they must not
// care what plugin above them are doing.
var directives = []string{
	"reload",
	"debug",
	"ready",
	"prometheus",
	"errors",
	"log",
	"resolve",
	"archivingcache",
	"loop",
	"forward",
}

func init() {
	dnsserver.Directives = directives
}

func main() {
	coremain.Run()
}
