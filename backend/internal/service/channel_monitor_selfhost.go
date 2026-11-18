package service

import (
	"context"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// monitorSelfHosts holds exact hostnames of this deployment's public site.
// When Docker/internal DNS resolves those names to RFC1918/ULA addresses,
// validateEndpoint and safeDialContext may accept them. IP literals, loopback,
// and link-local/metadata addresses are never exempted.
var (
	monitorSelfHostsMu      sync.RWMutex
	monitorSelfHostsStatic  = map[string]struct{}{}
	monitorSelfHostsDynamic func() []string
)

func setMonitorSelfHosts(hosts []string) {
	next := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		if h = normalizeMonitorHostname(h); h != "" {
			next[h] = struct{}{}
		}
	}
	monitorSelfHostsMu.Lock()
	monitorSelfHostsStatic = next
	monitorSelfHostsMu.Unlock()
}

func setMonitorSelfHostsDynamic(fn func() []string) {
	monitorSelfHostsMu.Lock()
	monitorSelfHostsDynamic = fn
	monitorSelfHostsMu.Unlock()
}

// configureMonitorSelfHosts installs the allowlist from config, environment,
// and (when available) the settings service's public frontend / API URLs.
func configureMonitorSelfHosts(settings *SettingService) {
	var raw []string
	if settings != nil && settings.cfg != nil {
		raw = append(raw, settings.cfg.Server.FrontendURL)
		raw = append(raw, settings.cfg.CORS.AllowedOrigins...)
		raw = append(raw, settings.cfg.ChannelMonitor.SelfHosts...)
	}
	if env := strings.TrimSpace(os.Getenv("CHANNEL_MONITOR_SELF_HOSTS")); env != "" {
		raw = append(raw, strings.FieldsFunc(env, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
		})...)
	}
	setMonitorSelfHosts(parseMonitorSelfHosts(raw))

	if settings == nil {
		setMonitorSelfHostsDynamic(nil)
		return
	}
	setMonitorSelfHostsDynamic(func() []string {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		urls := []string{settings.GetFrontendURL(ctx)}
		if settings.settingRepo != nil {
			if v, err := settings.settingRepo.GetValue(ctx, SettingKeyAPIBaseURL); err == nil {
				urls = append(urls, v)
			}
		}
		return parseMonitorSelfHosts(urls)
	})
}

func parseMonitorSelfHosts(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		h := hostnameFromMonitorSelfRef(item)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

func hostnameFromMonitorSelfRef(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return cleanedMonitorHostname(u.Hostname())
	}
	// Bare hostname, optionally with a port.
	host := raw
	if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.ContainsAny(host, "/:?#") {
		return ""
	}
	return cleanedMonitorHostname(host)
}

func cleanedMonitorHostname(host string) string {
	host = normalizeMonitorHostname(host)
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}
	return host
}

func normalizeMonitorHostname(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

func isSelfMonitorHost(hostname string) bool {
	host := normalizeMonitorHostname(hostname)
	if host == "" || net.ParseIP(host) != nil || isBlockedHostname(host) {
		return false
	}
	monitorSelfHostsMu.RLock()
	_, ok := monitorSelfHostsStatic[host]
	dyn := monitorSelfHostsDynamic
	monitorSelfHostsMu.RUnlock()
	if ok {
		return true
	}
	if dyn == nil {
		return false
	}
	for _, h := range dyn() {
		if normalizeMonitorHostname(h) == host {
			return true
		}
	}
	return false
}

// selfHostPrivateCIDRs is the only private space a self-host may resolve to:
// RFC1918 IPv4 and IPv6 ULA. Loopback, link-local (including 169.254.169.254),
// CGNAT, and unspecified addresses stay blocked even for an allowlisted name.
var selfHostPrivateCIDRs = mustParseCIDRs([]string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"fc00::/7",
})

func isRFC1918OrULA(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range selfHostPrivateCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// monitorDestinationBlocked reports whether a resolved address may be used
// for hostname. IP literals never receive the self-host exception; callers
// should pass the hostname as an IP string in that case (or use isPrivateIP).
func monitorDestinationBlocked(hostname string, ip net.IP) bool {
	if !isPrivateIP(ip) {
		return false
	}
	if net.ParseIP(hostname) != nil {
		return true
	}
	return !isSelfMonitorHost(hostname) || !isRFC1918OrULA(ip)
}
