//go:build unit

package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func stubMonitorLookup(t *testing.T, records map[string][]string) {
	t.Helper()
	old := lookupMonitorIPs
	lookupMonitorIPs = func(_ context.Context, hostname string) ([]net.IPAddr, error) {
		ips, ok := records[normalizeMonitorHostname(hostname)]
		if !ok {
			return nil, &net.DNSError{Err: "no such host", Name: hostname, IsNotFound: true}
		}
		out := make([]net.IPAddr, 0, len(ips))
		for _, s := range ips {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, &net.DNSError{Err: "bad stub address " + s, Name: hostname}
			}
			out = append(out, net.IPAddr{IP: ip})
		}
		return out, nil
	}
	t.Cleanup(func() { lookupMonitorIPs = old })
}

func withMonitorSelfHosts(t *testing.T, hosts ...string) {
	t.Helper()
	monitorSelfHostsMu.RLock()
	oldStatic := monitorSelfHostsStatic
	oldDyn := monitorSelfHostsDynamic
	monitorSelfHostsMu.RUnlock()
	setMonitorSelfHosts(hosts)
	setMonitorSelfHostsDynamic(nil)
	t.Cleanup(func() {
		monitorSelfHostsMu.Lock()
		monitorSelfHostsStatic = oldStatic
		monitorSelfHostsDynamic = oldDyn
		monitorSelfHostsMu.Unlock()
	})
}

func TestParseMonitorSelfHosts_IgnoresIPsWildcardsAndKeepsExactHost(t *testing.T) {
	got := parseMonitorSelfHosts([]string{
		"https://sub.batecode.xyz/",
		"SUB.BATECODE.XYZ.",
		"10.0.0.1",
		"https://192.168.1.2/v1",
		"*",
		"",
		"https://sub.batecode.xyz:443/path",
		"other.example.com:8443",
	})
	require.Equal(t, []string{"sub.batecode.xyz", "other.example.com"}, got)
}

func TestValidateEndpoint_SelfHostAllowsRFC1918AndULAOnly(t *testing.T) {
	withMonitorSelfHosts(t, "sub.batecode.xyz", "ula.example", "mixed.example", "loop.example", "meta.example", "cgnat.example", "public.example")
	stubMonitorLookup(t, map[string][]string{
		"sub.batecode.xyz": {"172.26.0.2"},
		"other.example":    {"172.26.0.2"},
		"loop.example":     {"127.0.0.1"},
		"meta.example":     {"169.254.169.254"},
		"mixed.example":    {"172.26.0.2", "127.0.0.1"},
		"cgnat.example":    {"100.64.0.1"},
		"ula.example":      {"fd12:3456::1"},
		"public.example":   {"8.8.8.8"},
	})

	require.NoError(t, validateEndpoint("https://sub.batecode.xyz"))
	require.NoError(t, validateEndpoint("https://sub.batecode.xyz/v1"))
	require.NoError(t, validateEndpoint("https://ula.example"))
	require.NoError(t, validateEndpoint("https://public.example"))

	require.ErrorIs(t, validateEndpoint("https://other.example"), ErrChannelMonitorEndpointPrivate)
	require.ErrorIs(t, validateEndpoint("https://loop.example"), ErrChannelMonitorEndpointPrivate)
	require.ErrorIs(t, validateEndpoint("https://meta.example"), ErrChannelMonitorEndpointPrivate)
	require.ErrorIs(t, validateEndpoint("https://mixed.example"), ErrChannelMonitorEndpointPrivate)
	require.ErrorIs(t, validateEndpoint("https://cgnat.example"), ErrChannelMonitorEndpointPrivate)

	// IP literals never get the self-host exception, even if they match a resolved self address.
	require.ErrorIs(t, validateEndpoint("https://172.26.0.2"), ErrChannelMonitorEndpointPrivate)
	require.ErrorIs(t, validateEndpoint("https://10.0.0.1"), ErrChannelMonitorEndpointPrivate)
	require.ErrorIs(t, validateEndpoint("https://127.0.0.1"), ErrChannelMonitorEndpointPrivate)
	require.ErrorIs(t, validateEndpoint("https://169.254.169.254"), ErrChannelMonitorEndpointPrivate)
}

func TestSafeDialContext_SelfHostAllowsRFC1918ButNotLoopbackOrMetadata(t *testing.T) {
	withMonitorSelfHosts(t, "sub.batecode.xyz")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	ctx := context.Background()

	// Allowlisted name resolving to the loopback listener via a stubbed RFC1918
	// address is tested through the decision helper; dial itself uses the stub
	// IPs. Verify blocking/allow decisions and literal rejects here.
	require.False(t, monitorDestinationBlocked("sub.batecode.xyz", net.ParseIP("172.26.0.2")))
	require.False(t, monitorDestinationBlocked("sub.batecode.xyz", net.ParseIP("10.1.2.3")))
	require.False(t, monitorDestinationBlocked("sub.batecode.xyz", net.ParseIP("192.168.0.9")))
	require.False(t, monitorDestinationBlocked("sub.batecode.xyz", net.ParseIP("fd12::1")))
	require.True(t, monitorDestinationBlocked("sub.batecode.xyz", net.ParseIP("127.0.0.1")))
	require.True(t, monitorDestinationBlocked("sub.batecode.xyz", net.ParseIP("169.254.169.254")))
	require.True(t, monitorDestinationBlocked("sub.batecode.xyz", net.ParseIP("100.64.0.1")))
	require.True(t, monitorDestinationBlocked("evil.example", net.ParseIP("172.26.0.2")))
	require.True(t, monitorDestinationBlocked("172.26.0.2", net.ParseIP("172.26.0.2")))
	require.True(t, monitorDestinationBlocked("10.1.2.3", net.ParseIP("10.1.2.3")))

	// Literal private IP in the dial address is always blocked.
	_, err = safeDialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked by SSRF policy")
	_, err = safeDialContext(ctx, "tcp", "169.254.169.254:80")
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked by SSRF policy")
	_, err = safeDialContext(ctx, "tcp", "10.0.0.1:80")
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked by SSRF policy")

	stubMonitorLookup(t, map[string][]string{
		"sub.batecode.xyz": {"127.0.0.1"},
		"other.example":    {"127.0.0.1"},
	})
	// Allowlisted name resolving to loopback is still rejected at dial time.
	_, err = safeDialContext(ctx, "tcp", net.JoinHostPort("sub.batecode.xyz", port))
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked by SSRF policy")
	_, err = safeDialContext(ctx, "tcp", net.JoinHostPort("other.example", port))
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked by SSRF policy")
}

func TestMonitorSSRFCheckRedirect_BlocksPrivateIPLiterals(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://169.254.169.254/latest", nil)
	require.NoError(t, err)
	err = monitorSSRFCheckRedirect(req, []*http.Request{req})
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked by SSRF policy")

	loop, err := http.NewRequest(http.MethodGet, "https://127.0.0.1/", nil)
	require.NoError(t, err)
	require.Error(t, monitorSSRFCheckRedirect(loop, []*http.Request{loop}))

	priv, err := http.NewRequest(http.MethodGet, "https://10.0.0.1/", nil)
	require.NoError(t, err)
	require.Error(t, monitorSSRFCheckRedirect(priv, []*http.Request{priv}))

	named, err := http.NewRequest(http.MethodGet, "https://evil.internal/", nil)
	require.NoError(t, err)
	require.NoError(t, monitorSSRFCheckRedirect(named, []*http.Request{named}))
}

func TestMonitorHTTPClient_RedirectToPrivateLiteralIsRejected(t *testing.T) {
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	t.Cleanup(redir.Close)

	// The test server is loopback; swap only CheckRedirect onto a client that
	// can reach the test server, to assert the redirect gate itself.
	client := &http.Client{CheckRedirect: monitorSSRFCheckRedirect}
	req, err := http.NewRequest(http.MethodGet, redir.URL, nil)
	require.NoError(t, err)
	_, err = client.Do(req)
	require.Error(t, err)
	var addrErr *net.AddrError
	require.True(t, errors.As(err, &addrErr) || (err != nil && err.Error() != ""), err)
	require.Contains(t, err.Error(), "blocked by SSRF policy")
}

func TestHostnameFromMonitorSelfRef(t *testing.T) {
	require.Equal(t, "sub.batecode.xyz", hostnameFromMonitorSelfRef("https://sub.batecode.xyz/v1"))
	require.Equal(t, "sub.batecode.xyz", hostnameFromMonitorSelfRef("SUB.BATECODE.XYZ."))
	require.Empty(t, hostnameFromMonitorSelfRef("127.0.0.1"))
	require.Empty(t, hostnameFromMonitorSelfRef("https://127.0.0.1/"))
	require.Empty(t, hostnameFromMonitorSelfRef("169.254.169.254"))
}

func TestIsSelfMonitorHost_DynamicSourceAndExactMatch(t *testing.T) {
	withMonitorSelfHosts(t, "sub.batecode.xyz")
	require.True(t, isSelfMonitorHost("SUB.BATECODE.XYZ."))
	require.False(t, isSelfMonitorHost("api.sub.batecode.xyz"))
	require.False(t, isSelfMonitorHost("batecode.xyz"))
	require.False(t, isSelfMonitorHost("localhost"))
	require.False(t, isSelfMonitorHost("172.26.0.2"))

	setMonitorSelfHostsDynamic(func() []string { return []string{"app.example.test"} })
	t.Cleanup(func() { setMonitorSelfHostsDynamic(nil) })
	require.True(t, isSelfMonitorHost("app.example.test"))
	require.False(t, isSelfMonitorHost("other.example.test"))
}
