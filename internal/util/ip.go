package util

import (
	"net"
	"net/http"
)

// GetClientIP returns the client address. middleware.RealIP has already
// replaced RemoteAddr with the proxy-reported address when the request came
// through a trusted local proxy, so headers are not read again here.
func GetClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
