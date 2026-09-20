package ritalin

import (
	"context"
	"encoding/json"
	"fmt"
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
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	read := func(method string) map[string]any {
		var req struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if dec.Decode(&req) != nil || req.Method != method {
			os.Exit(11)
		}
		return req.Params
	}
	reply := func(id int, result any) { _ = enc.Encode(map[string]any{"id": id, "result": result}) }
	notify := func(method string, params any) { _ = enc.Encode(map[string]any{"method": method, "params": params}) }
	read("initialize")
	if mode == "initerror" {
		_ = enc.Encode(map[string]any{"id": 0, "error": map[string]any{"code": -32600, "message": "initialize rejected"}})
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
	reply(0, map[string]any{})
	// New/legacy notifications can have unrelated parameter shapes.
	notify("mcpServer/startupStatus/updated", map[string]any{"error": "optional server unavailable"})
	notify("unknown/futureNotification", map[string]any{"message": map[string]string{"unexpected": "shape"}})
	read("initialized")
	p := read("thread/start")
	if p["model"] != "synthetic-model" || p["sandbox"] != "read-only" || p["approvalPolicy"] != "never" || p["ephemeral"] != true {
		os.Exit(12)
	}
	reply(1, map[string]any{"thread": map[string]string{"id": "thread-test"}})
	notify("thread/started", map[string]any{})
	p = read("turn/start")
	if p["threadId"] != "thread-test" || p["model"] != "synthetic-model" || p["effort"] != "low" {
		os.Exit(13)
	}
	reply(2, map[string]any{"turn": map[string]string{"id": "turn-test"}})
	notify("turn/started", map[string]any{"threadId": "thread-test", "turn": map[string]string{"id": "turn-test"}})
	if mode == "sleep" {
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
	if mode == "approval" {
		_ = enc.Encode(map[string]any{"id": "approval-test", "method": "item/commandExecution/requestApproval", "params": map[string]any{}})
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
	if mode == "retry" {
		notify("error", map[string]any{"threadId": "thread-test", "turnId": "turn-test", "willRetry": true, "error": map[string]string{"message": "capacity retry", "codexErrorInfo": "serverOverloaded"}})
		fmt.Fprintln(os.Stderr, "diagnostic synthetic-token test-account")
		time.Sleep(50 * time.Millisecond)
	}
	text := "nothing generated"
	if mode == "keyword" {
		text = "使用内嵌 SVG 来画鹈鹕。"
	}
	if mode == "html" || mode == "retry" || mode == "stream" || mode == "failed" {
		text = "<!doctype html><html><body><svg></svg></body></html>"
	}
	chunks := []string{text[:len(text)/2], text[len(text)/2:]}
	if mode == "keyword" {
		chunks = []string{"使用内", "嵌 SVG 来画鹈鹕。"}
	}
	for i, chunk := range chunks {
		notify("item/agentMessage/delta", map[string]string{"threadId": "thread-test", "turnId": "turn-test", "itemId": "item-test", "delta": chunk})
		if mode == "stream" && i == 0 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if mode == "keyword" {
		time.Sleep(20 * time.Second)
	}
	notify("item/completed", map[string]any{"threadId": "thread-test", "turnId": "turn-test", "item": map[string]string{"id": "item-test", "type": "agentMessage", "text": text}})
	turn := map[string]any{"id": "turn-test", "status": "completed"}
	if mode == "failed" {
		turn["status"] = "failed"
		turn["error"] = map[string]string{"message": "terminal failure"}
	}
	notify("turn/completed", map[string]any{"threadId": "thread-test", "turn": turn})
	// The client must return on turn/completed, not wait for server exit.
	time.Sleep(20 * time.Second)
	os.Exit(0)
}
func TestPelicanOutcomesAndResume(t *testing.T) {
	for _, mode := range []string{"nohtml", "keyword", "error", "html", "sleep", "retry", "failed", "approval", "initerror"} {
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
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
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
			case "error", "sleep", "failed", "approval", "initerror":
				if e == nil || len(c.States) != 1 || c.States[0].Status != "pending" {
					t.Fatal("failure must preserve candidate", e)
				}
			case "html", "retry":
				if e != nil || c.States[0].HTML == "" || c.States[0].Status != "review" || c.States[0].PNG != "" {
					t.Fatal("HTML must be reviewable without a browser", e)
				}
				if _, e := os.Stat(c.States[0].HTML); e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(displayed.String(), "\x00output:") {
					t.Fatal("model text not displayed")
				}
				if strings.Count(displayed.String(), "<!doctype html>") != 1 {
					t.Fatal("completed item duplicated streamed text")
				}
				if mode == "retry" {
					b, err := os.ReadFile(filepath.Join(filepath.Dir(c.States[0].HTML), "events.jsonl"))
					if err != nil || !strings.Contains(string(b), `"will_retry":true`) || !strings.Contains(string(b), "capacity retry") || !strings.Contains(string(b), "stderr") {
						t.Fatal("retry/diagnostic not recorded", err)
					}
					if strings.Contains(string(b), "synthetic-token") || strings.Contains(string(b), "test-account") || strings.Contains(displayed.String(), "synthetic-token") {
						t.Fatal("credentials exposed")
					}
				}
			}
		})
	}
}
