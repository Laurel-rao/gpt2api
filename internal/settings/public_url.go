package settings

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

// PublicURLFromRequest builds an externally reachable URL for locally served public assets.
func PublicURLFromRequest(r *http.Request, configuredBase string, publicPath string) string {
	publicPath = strings.TrimSpace(publicPath)
	if publicPath == "" {
		return ""
	}
	if strings.HasPrefix(publicPath, "http://") || strings.HasPrefix(publicPath, "https://") {
		return publicPath
	}

	base := strings.TrimRight(strings.TrimSpace(configuredBase), "/")
	if base == "" && r != nil {
		scheme := publicURLScheme(r)
		host := firstForwardedValue(r.Host)
		if forwardedHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
			host = forwardedHost
		}
		host = publicURLHostWithForwardedPort(host, firstForwardedValue(r.Header.Get("X-Forwarded-Port")), scheme)
		if host != "" {
			base = scheme + "://" + host
		}
	}
	if !strings.HasPrefix(publicPath, "/") {
		publicPath = "/" + publicPath
	}
	if base == "" {
		return publicPath
	}
	return base + publicPath
}

func publicURLScheme(r *http.Request) string {
	scheme := "http"
	if r != nil && r.TLS != nil {
		scheme = "https"
	}
	if r != nil {
		if forwarded := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
			scheme = forwarded
		}
	}
	return strings.ToLower(strings.TrimSpace(scheme))
}

func firstForwardedValue(value string) string {
	if value == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(value, ",")[0])
}

func publicURLHostWithForwardedPort(host string, port string, scheme string) string {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" || port == "" || publicURLHostHasPort(host) || publicURLIsDefaultPort(scheme, port) {
		return host
	}
	if _, err := strconv.Atoi(port); err != nil {
		return host
	}
	if strings.HasPrefix(host, "[") {
		return host + ":" + port
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func publicURLHostHasPort(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if _, port, err := net.SplitHostPort(host); err == nil {
		return strings.TrimSpace(port) != ""
	}
	if strings.Count(host, ":") != 1 {
		return false
	}
	_, port, ok := strings.Cut(host, ":")
	if !ok || strings.TrimSpace(port) == "" {
		return false
	}
	_, err := strconv.Atoi(port)
	return err == nil
}

func publicURLIsDefaultPort(scheme string, port string) bool {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	port = strings.TrimSpace(port)
	return (scheme == "http" && port == "80") || (scheme == "https" && port == "443")
}
