package web

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-src 'self'"

// secure wraps h with the host check, the client-header check and the
// standard response headers (D9).
func (s *Server) secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Content-Security-Policy", contentSecurityPolicy)

		if !s.opts.AllowRemote && !loopbackHostname(hostWithoutPort(r.Host)) {
			writeError(w, http.StatusForbidden, errors.New("forbidden host"), nil)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Forecast-Client") != "web" {
			writeError(w, http.StatusForbidden, errors.New("missing X-Forecast-Client header"), nil)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func hostWithoutPort(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.Trim(hostport, "[]")
}

// loopbackHostname reports whether host names the local machine: localhost,
// 127.0.0.0/8 or ::1.
func loopbackHostname(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// LoopbackAddr reports whether a listen address binds only to a loopback
// interface. An empty host means all interfaces and is not loopback.
func LoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return loopbackHostname(host)
}
