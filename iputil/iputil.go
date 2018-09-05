package iputil

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

// Resolve an address of the form 'host:port' into an IP and port,
// using the default port if port part of address is missing
func IpAndPortForAddr(addr string, defaultPort int) (string, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			host = addr
			port = strconv.Itoa(defaultPort)
		} else {
			return "", "", err
		}
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return "", "", err
	}
	for _, ip := range ips {
		ipV4 := ip.To4()
		if ipV4 != nil {
			return ipV4.String(), port, nil
		}
	}
	return "", "", errors.New("No IPv4 address found for " + addr)
}
