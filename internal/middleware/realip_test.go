package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealIPOnlyTrustsLocalProxies(t *testing.T) {
	for _, tc := range []struct {
		name, peer, header, value, want string
	}{
		{"cloudflared header", "127.0.0.1:4000", "CF-Connecting-IP", "203.0.113.7", "203.0.113.7"},
		{"forwarded last hop", "127.0.0.1:4000", "X-Forwarded-For", "127.0.0.1, 203.0.113.8", "203.0.113.8"},
		{"direct client spoof", "198.51.100.2:4000", "X-Forwarded-For", "127.0.0.1", "198.51.100.2"},
		{"direct client CF spoof", "198.51.100.2:4000", "CF-Connecting-IP", "127.0.0.1", "198.51.100.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := RealIP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set(tc.header, tc.value)
			h.ServeHTTP(httptest.NewRecorder(), r)
			host, _, err := net.SplitHostPort(got)
			if err != nil || host != tc.want {
				t.Fatalf("RemoteAddr = %q, want host %q", got, tc.want)
			}
		})
	}
}
