package ritalin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPelicanHelper(t *testing.T) {
	mode := os.Getenv("RITALIN_TEST_PELICAN")
	if mode == "" {
		return
	}
	if mode == "error" {
		os.Exit(9)
	}
	text := "nothing generated"
	if mode == "keyword" {
		text = "使用内嵌 SVG 来画鹈鹕。"
	}
	if mode == "html" {
		text = "<!doctype html><html><body><svg></svg></body></html>"
	}
	ev := map[string]any{"type": "item.completed", "item": map[string]string{"type": "agent_message", "text": text}}
	_ = json.NewEncoder(os.Stdout).Encode(ev)
	if mode == "sleep" {
		time.Sleep(20 * time.Second)
	}
	os.Exit(0)
}
func TestPelicanOutcomesAndResume(t *testing.T) {
	for _, mode := range []string{"nohtml", "keyword", "error", "html", "sleep"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("RITALIN_TEST_PELICAN", mode)
			t.Setenv("CODEX_CA_CERTIFICATE", "")
			t.Setenv("SSL_CERT_FILE", "")
			home := t.TempDir()
			s := &Store{Home: home, Root: filepath.Join(home, "ritalin")}
			if e := atomicWrite(filepath.Join(home, "auth.json"), []byte(`{"tokens":{"access_token":"synthetic-token","account_id":"test-account"}}`), 0600); e != nil {
				t.Fatal(e)
			}
			bin, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			c := Defaults()
			c.Command = []string{bin, "-test.run=^TestPelicanHelper$", "--"}
			c.Browser = filepath.Join(home, "missing-browser")
			c.KeywordFilter = true
			c.States = []State{{ID: "trial", Value: syntheticState(), Status: "pending", AuthHome: home, AccountHash: hash("test-account"), Model: "synthetic-model"}}
			ctx := context.Background()
			if mode == "sleep" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 500*time.Millisecond)
				defer cancel()
			}
			var displayed strings.Builder
			e = pelican(ctx, s, &c, "trial", func(text string) { displayed.WriteString(text) })
			switch mode {
			case "nohtml", "keyword":
				if e != nil || len(c.States) != 0 {
					t.Fatal("expected deletion", e, c.States)
				}
			case "error", "sleep":
				if e == nil || len(c.States) != 1 || c.States[0].Status != "pending" {
					t.Fatal("failure must preserve candidate", e)
				}
			case "html":
				if e == nil || c.States[0].HTML == "" || c.States[0].Status != "review" {
					t.Fatal("render failure must preserve HTML", e)
				}
				if _, e := os.Stat(c.States[0].HTML); e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(displayed.String(), "\x00output:") {
					t.Fatal("model text not displayed")
				}
			}
		})
	}
}
