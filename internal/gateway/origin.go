package gateway

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// resolveOrigin returns scheme://host for the request, honouring forwarded
// headers only when the immediate peer's IP matches one of the configured
// trusted-proxy CIDRs. When the peer is untrusted, the host falls back to
// gateway.public_url if set, else r.Host; the scheme to https if r.TLS is
// non-nil, else http.
func (s *Server) resolveOrigin(r *http.Request) string {
	if s.peerIsTrusted(r) {
		scheme := s.scheme(r)
		if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
			if idx := strings.Index(proto, ","); idx >= 0 {
				proto = proto[:idx]
			}
			switch strings.ToLower(strings.TrimSpace(proto)) {
			case "http", "https":
				scheme = strings.ToLower(strings.TrimSpace(proto))
			default:
				slog.DebugContext(r.Context(), "ignoring unrecognised X-Forwarded-Proto", "value", proto)
			}
		}
		host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
		if host == "" {
			host = strings.TrimSpace(r.Host)
		}
		if host == "" {
			return ""
		}
		return scheme + "://" + host
	}
	scheme := s.scheme(r)
	host := s.publicHost()
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func (s *Server) scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if u := s.publicURL(); u != nil {
		return u.Scheme
	}
	return "http"
}

func (s *Server) publicHost() string {
	u := s.publicURL()
	if u == nil {
		return ""
	}
	return u.Host
}

func (s *Server) publicURL() *url.URL {
	raw := strings.TrimSpace(s.cfg.Gateway.PublicURL)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}

func (s *Server) peerIsTrusted(r *http.Request) bool {
	if len(s.trustedProxies) == 0 {
		return false
	}
	host := ipFromRemoteAddr(r.RemoteAddr)
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, cidr := range s.trustedProxies {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// parseTrustedProxies parses a list of CIDR strings. Empty list returns nil
// (no proxies trusted). Malformed entries produce an error so that startup
// fails fast rather than silently disabling the gate.
func parseTrustedProxies(raw []string) ([]*net.IPNet, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]*net.IPNet, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(s)
		if err != nil {
			return nil, err
		}
		out = append(out, cidr)
	}
	return out, nil
}
