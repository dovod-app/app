package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLocalRequest covers the four conditions and, more importantly, the two
// ways a caller who is not the operator can look like one:
//
//   - behind the same-host nginx template this repository ships, every remote
//     caller arrives from 127.0.0.1;
//   - a script on any web page the operator visits reaches 127.0.0.1 from the
//     operator's own browser, and corsMiddleware answers it with
//     Access-Control-Allow-Origin: *.
//
// Reading the peer address alone admits both.
func TestLocalRequest(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		host       string
		headers    map[string]string
		want       bool
	}{
		// The genuine local client.
		{"loopback, no headers", "127.0.0.1:54321", "127.0.0.1:8088", nil, true},
		{"loopback by name", "127.0.0.1:54321", "localhost:8088", nil, true},
		{"loopback v6", "[::1]:54321", "[::1]:8088", nil, true},
		{"host with no port", "127.0.0.1:54321", "localhost", nil, true},
		{"same-origin browser request", "127.0.0.1:54321", "localhost:8088",
			map[string]string{"Origin": "http://localhost:8088"}, true},
		{"the Nuxt dev server's browser, cross-port but local", "127.0.0.1:54321", "localhost:8088",
			map[string]string{"Origin": "http://localhost:3000"}, true},

		// A proxy in front.
		{"proxied, X-Forwarded-For", "127.0.0.1:54321", "localhost:8088",
			map[string]string{"X-Forwarded-For": "203.0.113.7"}, false},
		{"proxied, X-Real-IP", "127.0.0.1:54321", "localhost:8088",
			map[string]string{"X-Real-IP": "203.0.113.7"}, false},
		{"proxied, Forwarded", "127.0.0.1:54321", "localhost:8088",
			map[string]string{"Forwarded": "for=203.0.113.7"}, false},
		{"empty header value is still a header", "127.0.0.1:54321", "localhost:8088",
			map[string]string{"X-Forwarded-For": ""}, false},
		// A proxy that sets no forwarding header at all is caught only when it
		// passes the public name through. This is the partial half of the
		// defence and the test records exactly how far it goes.
		{"header-less proxy passing the public host through", "127.0.0.1:54321", "research.example.com", nil, false},

		// The operator's browser, driven by somebody else's page.
		{"cross-site script on evil.com", "127.0.0.1:54321", "127.0.0.1:8088",
			map[string]string{"Origin": "https://evil.com"}, false},
		{"cross-site script, http", "127.0.0.1:54321", "localhost:8088",
			map[string]string{"Origin": "http://evil.com:8088"}, false},
		{"sandboxed iframe sending Origin: null", "127.0.0.1:54321", "localhost:8088",
			map[string]string{"Origin": "null"}, false},

		// Off-machine.
		{"remote peer", "203.0.113.7:54321", "research.example.com", nil, false},
		{"remote peer forging a loopback X-Real-IP", "203.0.113.7:54321", "localhost:8088",
			map[string]string{"X-Real-IP": "127.0.0.1"}, false},
		{"the machine's own LAN address is not loopback", "192.168.1.10:54321", "192.168.1.5:8088", nil, false},
		{"a container's bridge gateway is not loopback", "172.17.0.1:54321", "localhost:8088", nil, false},
		{"unparseable peer", "not-an-address", "localhost:8088", nil, false},
		{"no Host at all", "127.0.0.1:54321", "", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
			r.RemoteAddr = tc.remoteAddr
			r.Host = tc.host
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := LocalRequest(r); got != tc.want {
				t.Fatalf("LocalRequest(peer=%q host=%q headers=%v) = %v, want %v",
					tc.remoteAddr, tc.host, tc.headers, got, tc.want)
			}
		})
	}
}
