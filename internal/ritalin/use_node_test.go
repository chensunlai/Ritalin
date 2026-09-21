package ritalin

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func nodeUseFixture(t *testing.T) *ui {
	m := dashboardFixture(t)
	m.tab = tabUse
	for i := range m.c.Nodes {
		m.c.Nodes[i].Checked = stamp()
		m.c.Nodes[i].Reach = []string{"chatgpt"}
	}
	m.c.States[0].NodeID = m.c.Nodes[0].ID
	m.c.States[1].Status = "usable"
	m.c.States[1].Node = m.c.Nodes[0].Name // Imported name must not auto-bind.
	return m
}

func TestUseNodeSelectionAndPersistence(t *testing.T) {
	m := nodeUseFixture(t)
	m.activate(entry{action: "select", id: "state-one"})
	m.activate(entry{action: "route-mode"})
	if !m.c.UseNode || m.routeState != "" || useNode(&m.c, &m.c.States[0]).ID != "node-one" {
		t.Fatal("known candidate did not automatically use its source")
	}
	m.activate(entry{action: "select", id: "state-two"})
	if m.routeState != "state-two" || m.c.Active != "state-one" {
		t.Fatal("imported state must wait for explicit node selection")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.routeState != "" || m.c.Active != "state-one" {
		t.Fatal("cancelling changed the active state")
	}
	m.activate(entry{action: "select", id: "state-two"})
	m.activate(entry{action: "route-pick", id: "node-two"})
	c, err := m.store.Load()
	if err != nil || c.Active != "state-two" || !c.UseNode || c.States[1].UseNodeID != "node-two" || c.States[1].NodeID != "" {
		t.Fatal("explicit binding was not saved separately from provenance", err)
	}
	m.activate(entry{action: "route-mode"})
	if m.c.UseNode || m.c.Active != "state-two" {
		t.Fatal("default connection should retain the active state")
	}
	m.activate(entry{action: "route-mode"})
	if !m.c.UseNode || m.routeState != "" || useNode(&m.c, &m.c.States[1]).ID != "node-two" {
		t.Fatal("chosen node was not remembered")
	}
	var banner bytes.Buffer
	startup(&banner, m.store, m.c, func(_ time.Duration) {})
	if !strings.Contains(banner.String(), "Singapore") {
		t.Fatal("startup did not show the selected proxy node")
	}
}

func TestUseNodeRequiresConfirmedID(t *testing.T) {
	m := nodeUseFixture(t)
	m.c.Nodes[1].Checked = ""
	m.pickUseNode("state-two")
	for _, e := range m.entries() {
		if e.id == "node-two" {
			t.Fatal("unchecked node offered")
		}
	}
	state := &m.c.States[0]
	state.UseNodeID = "deleted-node"
	if useNode(&m.c, state) != nil {
		t.Fatal("deleted explicit selection fell back to source")
	}
	state.UseNodeID, state.NodeID = "", ""
	if useNode(&m.c, state) != nil {
		t.Fatal("name inferred a binding")
	}
	m.c.Nodes = nil
	m.closeNodePicker()
	m.pickUseNode("state-two")
	if m.routeState != "" || m.notice == "" {
		t.Fatal("empty picker opened")
	}
}

func TestUseNodePickerBothLanguages(t *testing.T) {
	for _, language := range []string{"zh", "en"} {
		m := nodeUseFixture(t)
		m.c.Language = language
		m.pickUseNode("state-two")
		m.cursor = 1
		view := m.View()
		if !strings.Contains(view, "Tokyo") || strings.Contains(view, "hidden-password") {
			t.Fatal("node picker missing label or exposed credentials")
		}
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if m.routeState != "" || m.c.Active != "state-two" || !m.c.UseNode {
			t.Fatal("Enter did not confirm the route")
		}
	}
}

func TestStartUseWarpSelectsExplicitUpstream(t *testing.T) {
	t.Setenv("CODEX_CA_CERTIFICATE", "")
	t.Setenv("SSL_CERT_FILE", "")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:2")
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:3")
	t.Setenv("NO_PROXY", "*")
	for _, upstream := range []string{"http://127.0.0.1:9", "https://127.0.0.1:9", "socks5://127.0.0.1:9"} {
		t.Run(upstream, func(t *testing.T) {
			m := nodeUseFixture(t)
			m.c.UseNode, m.c.Upstream = true, "http://127.0.0.1:1"
			m.c.Nodes[0].URL = upstream
			warp, closeWarp, err := startUseWarp(context.Background(), m.store, m.c, &m.c.States[0], func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			defer closeWarp()
			req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", nil)
			proxy, err := warp.tr.Proxy(req)
			if err != nil || proxy == nil || proxy.String() != upstream {
				t.Fatal("node route was overridden by environment, NO_PROXY or configured upstream")
			}
		})
	}
}

func TestStartUseWarpDefaultAndFailure(t *testing.T) {
	t.Setenv("CODEX_CA_CERTIFICATE", "")
	t.Setenv("SSL_CERT_FILE", "")
	m := nodeUseFixture(t)
	m.c.Upstream = "http://127.0.0.1:7"
	warp, closeWarp, err := startUseWarp(context.Background(), m.store, m.c, &m.c.States[0], func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	proxy, _ := warp.tr.Proxy(req)
	closeWarp()
	if proxy == nil || proxy.String() != m.c.Upstream {
		t.Fatal("default route changed")
	}
	m.c.UseNode = true
	for _, mode := range []string{"missing", "empty-url", "invalid-url", "missing-mihomo"} {
		t.Run(mode, func(t *testing.T) {
			c := clone(m.c)
			switch mode {
			case "missing":
				c.Nodes = nil
			case "empty-url":
				c.Nodes[0].URL = ""
			case "invalid-url":
				c.Nodes[0].URL = "not a proxy"
			case "missing-mihomo":
				c.States[0].UseNodeID = "node-two"
				c.Mihomo = filepath.Join(t.TempDir(), "missing-mihomo")
			}
			w, cleanup, err := startUseWarp(context.Background(), m.store, c, &c.States[0], func(string) {})
			if err == nil || w != nil || cleanup != nil {
				t.Fatal("failed node silently fell back to default route")
			}
		})
	}
}

func TestUseNodeLoopbackRouting(t *testing.T) {
	for _, scenario := range []struct {
		kind string
		test bool
	}{{"proxy", false}, {"clash", false}, {"proxy", true}, {"clash", true}} {
		t.Run(fmt.Sprintf("%s/test=%v", scenario.kind, scenario.test), func(t *testing.T) {
			kind := scenario.kind
			bin := os.Getenv("RITALIN_TEST_MIHOMO")
			if kind == "clash" && bin == "" {
				t.Skip("set RITALIN_TEST_MIHOMO for the loopback Mihomo routing test")
			}
			t.Setenv("CODEX_CA_CERTIFICATE", "")
			t.Setenv("SSL_CERT_FILE", "")
			var proxyRequests, defaultRequests atomic.Int32
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get(stateHeader) != syntheticState() {
					t.Error("outbound state was not replaced")
				}
				w.Header().Set(stateHeader, "server-state")
				fmt.Fprint(w, "unchanged-body")
			}))
			defer origin.Close()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyRequests.Add(1)
				if r.Method != "CONNECT" || r.Host != "chatgpt.com:443" {
					t.Error("unexpected proxy request")
					return
				}
				dst, err := net.Dial("tcp", strings.TrimPrefix(origin.URL, "https://"))
				if err != nil {
					t.Error(err)
					return
				}
				defer dst.Close()
				conn, rw, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_, _ = rw.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
				_ = rw.Flush()
				go func() { _, _ = io.Copy(dst, rw) }()
				_, _ = io.Copy(conn, dst)
			}))
			defer upstream.Close()
			defaultProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defaultRequests.Add(1)
				w.WriteHeader(502)
			}))
			defer defaultProxy.Close()
			for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
				t.Setenv(key, defaultProxy.URL)
			}
			t.Setenv("NO_PROXY", "*")
			m := nodeUseFixture(t)
			m.c.UseNode, m.c.Upstream, m.c.Mihomo = true, defaultProxy.URL, bin
			n := &m.c.Nodes[0]
			n.Kind, n.URL = kind, upstream.URL
			u, _ := url.Parse(upstream.URL)
			port, _ := strconv.Atoi(u.Port())
			n.Clash = map[string]any{"type": "http", "server": u.Hostname(), "port": port}
			var warp *Warp
			var closeWarp func()
			var err error
			var display strings.Builder
			var displayMu sync.Mutex
			if scenario.test {
				// Test source wins even when normal use chose a different node,
				// replacement is disabled, and another state is active.
				m.c.UseNode, m.c.Replace, m.c.Active = false, false, "state-two"
				m.c.States[0].UseNodeID = "node-two"
				warp, closeWarp, err = startTestWarp(context.Background(), m.store, m.c, &m.c.States[0], func(s string) {
					displayMu.Lock()
					defer displayMu.Unlock()
					display.WriteString(s)
				})
			} else {
				warp, closeWarp, err = startUseWarp(context.Background(), m.store, m.c, &m.c.States[0], func(string) {})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer closeWarp()
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			warp.tr.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com"}
			pem, err := os.ReadFile(warp.CA)
			if err != nil {
				t.Fatal(err)
			}
			roots = x509.NewCertPool()
			roots.AppendCertsFromPEM(pem)
			u, _ = url.Parse(warp.URL)
			client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u), TLSClientConfig: &tls.Config{RootCAs: roots}}, Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", nil)
			req.Header.Set(stateHeader, "client-state")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || string(body) != "unchanged-body" || resp.Header.Get(stateHeader) != syntheticState() || proxyRequests.Load() != 1 || defaultRequests.Load() != 0 {
				t.Fatal("node route or header replacement failed", err)
			}
			displayMu.Lock()
			output := display.String()
			displayMu.Unlock()
			if scenario.test && (!strings.Contains(output, "server-state") || !strings.Contains(output, syntheticState()) || !strings.Contains(output, "Tokyo")) {
				t.Fatal("test node or live state observation missing")
			}
			closeWarp()
			if kind == "clash" {
				entries, err := os.ReadDir(filepath.Join(m.store.Root, "clash"))
				if err != nil || len(entries) != 0 {
					t.Fatal("Mihomo runtime configuration was not cleaned up", err)
				}
			}
		})
	}
}
