package archivingcache

import "testing"

func TestParseProxyAddress(t *testing.T) {
	tests := []struct {
		HostPortOrIp string
		Expect       string
	}{
		{
			HostPortOrIp: "8.8.8.8:53",
			Expect:       "8.8.8.8",
		},
		{
			HostPortOrIp: "8.8.8.8",
			Expect:       "8.8.8.8",
		},
		{
			HostPortOrIp: "njet",
			Expect:       "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.HostPortOrIp, func(t *testing.T) {
			got, _ := parseHostPortOrIP(tt.HostPortOrIp)
			if tt.Expect != got {
				t.Errorf("Expected \"%s\" but got \"%s\"", tt.Expect, got)
			}
		})
	}
}
