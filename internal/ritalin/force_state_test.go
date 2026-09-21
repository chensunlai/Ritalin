package ritalin

import (
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
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestForceStateSwitchesPersistIndependently(t *testing.T) {
	m := dashboardFixture(t)
	if m.c.UseForceState || m.c.TestForceState {
		t.Fatal("force state must default to off")
	}
	for _, tab := range []int{tabTest, tabUse} {
		m.tab = tab
		found := false
		for i, e := range m.entries() {
			if e.action == "force-state" {
				found = true
				m.cursor = i
				m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				break
			}
		}
		if !found {
			t.Fatal("missing switch")
		}
		c, err := m.store.Load()
		if err != nil || !c.TestForceState || c.UseForceState != (tab == tabUse) {
			t.Fatal("switch was not saved independently", err)
		}
	}
	m.activate(entry{action: "force-state", id: "test"})
	if m.c.TestForceState || !m.c.UseForceState {
		t.Fatal("turning test force off changed use force")
	}
	for _, lang := range []string{"zh", "en"} {
		m.c.Language = lang
		if m.entryLabel(forceStateEntry(true, "use")) == m.entryLabel(forceStateEntry(false, "use")) {
			t.Fatal("switch status is not visible")
		}
	}
	// A failed save must not leave a switch enabled only in memory.
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	m.store.Root = filepath.Join(path, "not-a-directory")
	m.activate(entry{action: "force-state", id: "test"})
	if m.c.TestForceState {
		t.Fatal("failed save did not roll back switch")
	}
}

func TestForceStateHTTPS(t *testing.T) {
	t.Setenv("CODEX_CA_CERTIFICATE", "")
	t.Setenv("SSL_CERT_FILE", "")
	for _, mode := range []string{"test", "use", "disabled", "empty"} {
		for _, force := range []bool{false, true} {
			for _, present := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/force=%v/present=%v", mode, force, present), func(t *testing.T) {
					want := func(target bool, original string) string {
						if target && (force || present) && mode != "disabled" && mode != "empty" {
							return syntheticState()
						}
						if present {
							return original
						}
						return ""
					}
					origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						target := r.URL.Path == "/backend-api/codex/responses"
						if r.Header.Get(stateHeader) != want(target, "client-state") {
							t.Error("wrong outbound header")
						}
						if present {
							w.Header().Set(stateHeader, "server-state")
						}
						fmt.Fprint(w, "unchanged-body")
					}))
					defer origin.Close()
					m := nodeUseFixture(t)
					var logs strings.Builder
					var mu sync.Mutex
					emit := func(s string) { mu.Lock(); defer mu.Unlock(); logs.WriteString(s) }
					var warp *Warp
					var cleanup func()
					var err error
					switch mode {
					case "test":
						m.c.TestForceState, m.c.UseForceState = force, !force
						warp, cleanup, err = startTestWarp(context.Background(), m.store, m.c, &m.c.States[0], emit)
					case "use":
						m.c.UseForceState, m.c.TestForceState = force, !force
						warp, cleanup, err = startUseWarp(context.Background(), m.store, m.c, &m.c.States[0], emit)
					default:
						value := syntheticState()
						if mode == "empty" {
							value = ""
						}
						warp, err = startWarpObserved(m.store, value, mode != "disabled", force, "", nil)
						if err == nil {
							cleanup = warp.Close
						}
					}
					if err != nil {
						t.Fatal(err)
					}
					defer cleanup()
					warp.tr.Proxy = nil
					warp.tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(origin.URL, "https://"))
					}
					roots := x509.NewCertPool()
					roots.AddCert(origin.Certificate())
					warp.tr.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com"}
					pem, err := os.ReadFile(warp.CA)
					if err != nil {
						t.Fatal(err)
					}
					roots = x509.NewCertPool()
					roots.AppendCertsFromPEM(pem)
					u, _ := url.Parse(warp.URL)
					client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u), TLSClientConfig: &tls.Config{RootCAs: roots}}, Timeout: 3 * time.Second}
					defer client.CloseIdleConnections()
					for _, path := range []string{"/backend-api/codex/responses", "/unrelated"} {
						req, _ := http.NewRequest("POST", "https://chatgpt.com"+path, nil)
						if present {
							req.Header.Set(stateHeader, "client-state")
						}
						resp, err := client.Do(req)
						if err != nil {
							t.Fatal(err)
						}
						body, err := io.ReadAll(resp.Body)
						resp.Body.Close()
						if err != nil || string(body) != "unchanged-body" || resp.Header.Get(stateHeader) != want(path != "/unrelated", "server-state") {
							t.Fatal("response header or body changed incorrectly", err)
						}
					}
					mu.Lock()
					log := logs.String()
					mu.Unlock()
					if mode == "test" {
						if present && !strings.Contains(log, "server-state") {
							t.Fatal("original server state hidden by forced replacement")
						}
						if !present && !strings.Contains(log, "x-codex-turn-state: 无") {
							t.Fatal("missing original server header shown as injected state")
						}
					}
				})
			}
		}
	}
}
