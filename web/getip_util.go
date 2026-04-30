package web

import (
	"net"
	"net/http"
	"regexp"
	"strings"
)

// normalizeCandidateIP validates and normalizes an IP address candidate
// from a request header. It trims whitespace, takes the first comma-separated
// token (for XFF-like headers that may contain a chain), and validates.
func normalizeCandidateIP(raw string, ipv6 bool) string {
	ip := strings.TrimSpace(raw)
	// For XFF-like values, take the first address before a comma
	if idx := strings.Index(ip, ","); idx != -1 {
		ip = strings.TrimSpace(ip[:idx])
	}
	if ip == "" {
		return ""
	}
	if ipv6 {
		parsed := net.ParseIP(ip)
		if parsed != nil && parsed.To16() != nil && parsed.To4() == nil {
			return strings.TrimPrefix(ip, "::ffff:")
		}
		return ""
	}
	parsed := net.ParseIP(ip)
	if parsed != nil {
		return strings.TrimPrefix(ip, "::ffff:")
	}
	return ""
}

// getClientIP extracts the real client IP from the request using the following
// priority chain, mirroring the PHP getIP_util.php behavior:
//
//  1. CF-Connecting-IPv6 (Cloudflare, must be a valid IPv6)
//  2. Client-IP
//  3. X-Real-IP
//  4. X-Forwarded-For (first address in the chain)
//  5. RemoteAddr (fallback)
func getClientIP(r *http.Request) string {
	// 1. Cloudflare IPv6 header — must be a valid IPv6 address
	if cf := r.Header.Get("CF-Connecting-IPv6"); cf != "" {
		if ip := normalizeCandidateIP(cf, true); ip != "" {
			return strings.TrimPrefix(ip, "::ffff:")
		}
	}

	// 2–4. Other forwarding / proxy headers — accept any valid IP
	for _, header := range []string{"Client-IP", "X-Real-IP", "X-Forwarded-For"} {
		if v := r.Header.Get(header); v != "" {
			if ip := normalizeCandidateIP(v, false); ip != "" {
				return strings.TrimPrefix(ip, "::ffff:")
			}
		}
	}

	// 5. Fallback: RemoteAddr set by the server
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr may not have a port in some environments
		ip = r.RemoteAddr
	}
	if parsed := net.ParseIP(ip); parsed != nil {
		return strings.TrimPrefix(ip, "::ffff:")
	}

	return ""
}

// classifyPrivateIP returns a human-readable description if the IP is a
// private or special-purpose address, or an empty string otherwise.
// Mirrors the PHP getLocalOrPrivateIpInfo() function.
func classifyPrivateIP(ip string) string {
	// Strip IPv4-mapped IPv6 prefix if present
	ip = strings.TrimPrefix(ip, "::ffff:")

	switch {
	case ip == "::1":
		return "localhost IPv6 access"
	case strings.HasPrefix(ip, "fe80:"):
		return "link-local IPv6 access"
	// ULA IPv6 (fc00::/7): fc00:: - fdff:ffff:...
	case isULAIPv6(ip):
		return "ULA IPv6 access"
	case strings.HasPrefix(ip, "127."):
		return "localhost IPv4 access"
	case strings.HasPrefix(ip, "10."):
		return "private IPv4 access"
	case mustCompile(`^172\.(1[6-9]|2\d|3[01])\.`).MatchString(ip):
		return "private IPv4 access"
	case strings.HasPrefix(ip, "192.168"):
		return "private IPv4 access"
	case strings.HasPrefix(ip, "169.254"):
		return "link-local IPv4 access"
	case mustCompile(`^100\.([6-9][0-9]|1[0-2][0-7])\.`).MatchString(ip):
		return "CGNAT IPv4 access"
	}
	return ""
}

// isULAIPv6 checks if an IP is a Unique Local IPv6 Unicast Address (fc00::/7).
func isULAIPv6(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To16() == nil {
		return false
	}
	// fc00::/7 means the first 7 bits are 1111110
	// So the first byte & 0xFE must equal 0xFC
	return ip[0]&0xFE == 0xFC
}

// mustCompile is a helper that compiles a regex and panics on error
// (safe to use for static patterns).
func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}
