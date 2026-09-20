package ritalin

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestLiveModelsOptIn(t *testing.T) {
	p := os.Getenv("RITALIN_TEST_PROXY")
	if p == "" {
		t.Skip("set RITALIN_TEST_PROXY for credential-free models check")
	}
	r, e := reachable(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	t.Log(r)
}
func TestMihomoLifecycleOptIn(t *testing.T) {
	bin := os.Getenv("RITALIN_TEST_MIHOMO")
	if bin == "" {
		t.Skip("set RITALIN_TEST_MIHOMO for real child-process lifecycle check")
	}
	s := &Store{Root: t.TempDir()}
	c := Defaults()
	c.Mihomo = bin
	n := Node{ID: "synthetic", Kind: "clash", Clash: map[string]any{"name": "test", "type": "http", "server": "127.0.0.1", "port": 9}}
	run, e := startClash(context.Background(), s, c, []Node{n}, func(string) {})
	if e != nil {
		t.Fatal(e)
	}
	if run.URLs[n.ID] == "" {
		t.Fatal("no listener")
	}
	run.Close()
	files, e := os.ReadDir(filepath.Join(s.Root, "clash"))
	if e != nil {
		t.Fatal(e)
	}
	if len(files) != 0 {
		t.Fatal("private runtime config not cleaned")
	}
}

func TestStateMetricsAndFilters(t *testing.T) {
	for _, tt := range []struct {
		blocks, chars int
		team          bool
	}{{10, 292, false}, {12, 332, true}} {
		raw := make([]byte, 57+tt.blocks*16)
		raw[0] = 128
		value := base64.URLEncoding.EncodeToString(raw)
		m, e := parseState(value)
		if e != nil || m.Characters != tt.chars || m.Blocks != tt.blocks || !lengthMatch(m, tt.team) {
			t.Fatalf("metrics %+v %v", m, e)
		}
	}
	for _, v := range []string{"bad", base64.URLEncoding.EncodeToString(make([]byte, 100))} {
		if _, e := parseState(v); e == nil {
			t.Fatal("accepted invalid layout")
		}
	}
}
func TestHomeAndPrivateSave(t *testing.T) {
	h := t.TempDir()
	t.Setenv("CODEXHOME", h)
	t.Setenv("CODEX_HOME", "")
	if DefaultHome() != h {
		t.Fatal("alias not used")
	}
	h2 := t.TempDir()
	t.Setenv("CODEX_HOME", h2)
	s := OpenStore()
	if s.Home != h2 {
		t.Fatal("CODEX_HOME precedence")
	}
	c := Defaults()
	if e := s.Save(c); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Load(); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(s.Root, "config.json"), []byte("broken"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Load(); e == nil {
		t.Fatal("silently reset corrupt config")
	}
}
func TestProxyImport(t *testing.T) {
	n, e := parseProxies("http://127.0.0.1:1\nsocks5://user:pass@[::1]:2\nhttp://127.0.0.1:1")
	if e != nil || len(n) != 2 {
		t.Fatal(n, e)
	}
	if n[1].Name != "socks5://[::1]:2" {
		t.Fatal("credentials in label")
	}
	for _, s := range []string{"http://example.com", "file:///tmp/x", "https://x:8/path"} {
		if _, e := parseProxy(s); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	tr, e := transport(n[0].URL)
	if e != nil || tr.Proxy == nil {
		t.Fatal(e)
	}
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	direct, _ := transport("")
	if direct.Proxy != nil {
		t.Fatal("direct uses environment proxy")
	}
}
func TestClashExtractOnlyNodes(t *testing.T) {
	n, e := parseClash([]byte("proxies:\n  - name: test\n    type: ss\n    server: example.com\n    port: 443\n    password: synthetic\n    dialer-proxy: attack\n    skip-cert-verify: true\nrules: [MATCH,DIRECT]\ntun: {enable: true}\n"))
	if e != nil || len(n) != 1 {
		t.Fatal(e)
	}
	if n[0].Clash["dialer-proxy"] != nil || n[0].Clash["skip-cert-verify"] != false {
		t.Fatal("unsafe node settings kept")
	}
}
