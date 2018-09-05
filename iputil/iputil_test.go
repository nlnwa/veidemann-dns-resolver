package iputil

import "testing"

func TestIpForHost(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		wantIp   string
		wantPort string
		wantErr  bool
	}{
		{
			name:     "Default port",
			addr:     "localhost",
			wantIp:   "127.0.0.1",
			wantPort: "999",
		},
		{
			name:     "With port",
			addr:     "localhost:80",
			wantIp:   "127.0.0.1",
			wantPort: "80",
		},
		{
			name:     "Ip address with default port",
			addr:     "198.161.0.3",
			wantIp:   "198.161.0.3",
			wantPort: "999",
		},
		{
			name:     "Ip address with port",
			addr:     "198.161.0.4:80",
			wantIp:   "198.161.0.4",
			wantPort: "80",
		},
		{
			name:    "Unknown host",
			addr:    "foo.bar:80",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIp, gotPort, err := IpAndPortForAddr(tt.addr, 999)
			if (err != nil) != tt.wantErr {
				t.Errorf("IpAndPortForAddr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotIp != tt.wantIp {
				t.Errorf("IpAndPortForAddr() = %v, want %v", gotIp, tt.wantIp)
			}
			if gotPort != tt.wantPort {
				t.Errorf("IpAndPortForAddr() = %v, want %v", gotPort, tt.wantPort)
			}
		})
	}
}
