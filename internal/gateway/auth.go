package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
)

// principalKey returns the rate-limit key for a request:
//
//	hex(hmac-sha256(serverNonce, bearer)) || "|" || ip
//
// The HMAC makes the key resistant to dumping the limiter map (the token
// never appears in cleartext in logs or memory dumps), and concatenating
// the IP keeps two NAT'd-but-same-token callers on separate budgets.
func (s *Server) principalKey(r *http.Request) string {
	bearer := ""
	if v := r.Header.Get("Authorization"); v != "" {
		if rest, ok := strings.CutPrefix(v, "Bearer "); ok {
			bearer = rest
		}
	}
	h := hmac.New(sha256.New, s.serverNonce)
	_, _ = h.Write([]byte(bearer))
	sum := h.Sum(nil)
	const hexdigits = "0123456789abcdef"
	buf := make([]byte, len(sum)*2)
	for i, b := range sum {
		buf[i*2] = hexdigits[b>>4]
		buf[i*2+1] = hexdigits[b&0x0f]
	}
	return string(buf) + "|" + ipFromRemoteAddr(r.RemoteAddr)
}

// ipFromRemoteAddr returns just the IP portion of "host:port" or "[ipv6]:port".
// Anything unparseable falls back to the input — caller treats it as opaque.
func ipFromRemoteAddr(remote string) string {
	if remote == "" {
		return ""
	}
	if i := strings.LastIndex(remote, ":"); i >= 0 {
		host := remote[:i]
		host = strings.TrimPrefix(host, "[")
		host = strings.TrimSuffix(host, "]")
		return host
	}
	return remote
}

// authHMAC is the constant-time bearer-token check. The expected and supplied
// tokens are both HMAC'd with the server nonce so the comparison runs over
// two 32-byte digests regardless of supplied length — no length-leak.
func (s *Server) authHMAC(supplied string) bool {
	got := hmac.New(sha256.New, s.serverNonce)
	_, _ = got.Write([]byte(supplied))
	want := hmac.New(sha256.New, s.serverNonce)
	_, _ = want.Write([]byte(s.cfg.Gateway.Token))
	return subtle.ConstantTimeCompare(got.Sum(nil), want.Sum(nil)) == 1
}

// auth wraps next with bearer-token authentication. On rejection the
// response is HTTP 401 with body {"error":"unauthorized"}; the reason
// (`missing`, `bad-format`, `mismatch`) is emitted on a single `warn`
// log line. The supplied token is NEVER logged.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			slog.WarnContext(r.Context(), "unauthorized request", "reason", "missing")
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		bearer, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || bearer == "" {
			slog.WarnContext(r.Context(), "unauthorized request", "reason", "bad-format")
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !s.authHMAC(bearer) {
			slog.WarnContext(r.Context(), "unauthorized request", "reason", "mismatch")
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
