package ritalin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

func TestManualStateAndToggle(t *testing.T) {
	s := &Store{Home: t.TempDir(), Root: filepath.Join(t.TempDir(), "ritalin")}
	m := &ui{store: s, c: Defaults(), input: textarea.New(), width: 100, height: 30}
	m.openForm("manual", "state", syntheticState())
	m.applyForm()
	if len(m.c.States) != 1 || m.c.States[0].Status != "usable" || m.c.Active != "" {
		t.Fatal("manual state not stored separately")
	}
	id := m.c.States[0].ID
	m.activate(entry{action: "select", id: id})
	m.activate(entry{action: "replace"})
	c, e := s.Load()
	if e != nil || c.Active != id || c.Replace {
		t.Fatal(c, e)
	}
	if strings.Contains(m.View(), syntheticState()) {
		t.Fatal("full state exposed in list")
	}
}
func TestFilterIsLimitedToCurrentBatch(t *testing.T) {
	good, _ := parseState(syntheticState())
	bad := good
	bad.Characters = 312
	bad.Blocks = 11
	s := &Store{Root: t.TempDir()}
	c := Defaults()
	c.States = []State{{ID: "old", Metrics: bad}, {ID: "good", Metrics: good}, {ID: "bad", Metrics: bad}}
	m := &ui{store: s, c: c, probeIDs: []string{"good", "bad"}}
	m.filter(false)
	if len(m.c.States) != 2 || findState(&m.c, "old") == nil || findState(&m.c, "bad") != nil {
		t.Fatal("filtered historical state")
	}
}
func TestWaitingProgressAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &ui{store: &Store{}, c: Defaults(), busy: true, started: time.Now().Add(-6 * time.Second), cancel: cancel, width: 100, height: 30}
	if !strings.Contains(m.View(), "░") {
		t.Fatal("no progress indicator after 5s")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ctx.Err() == nil {
		t.Fatal("escape did not cancel")
	}
}
func TestDownloadProgress(t *testing.T) {
	var messages []string
	b, e := downloadProgress(bytes.NewReader(make([]byte, 128000)), 128000, 200000, func(s string) { messages = append(messages, s) })
	if e != nil || len(b) != 128000 || !strings.Contains(messages[len(messages)-1], "100%") {
		t.Fatal(messages, e)
	}
}
func TestCodexArgumentTransparency(t *testing.T) {
	c := Defaults()
	c.Command = []string{"custom-codex", "prefix with spaces"}
	args := []string{"--yolo", "--config", `a="b c"`, "literal;$(noop)"}
	cmd, e := codexCommand(c, args)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Join(cmd.Args, "\x00") != strings.Join(append(c.Command, args...), "\x00") {
		t.Fatal("arguments modified")
	}
}
func TestRenderWithBrowser(t *testing.T) {
	bin := os.Getenv("RITALIN_TEST_BROWSER")
	if bin == "" {
		t.Skip("set RITALIN_TEST_BROWSER for actual rendering")
	}
	s := &Store{Root: t.TempDir()}
	dir, e := s.runDir("pelican")
	if e != nil {
		t.Fatal(e)
	}
	html := filepath.Join(dir, "test.html")
	png := filepath.Join(dir, "test.png")
	if e = atomicWrite(html, []byte(`<html><body><svg width="400" height="300"><rect width="400" height="300" fill="skyblue"/><circle cx="180" cy="150" r="60" fill="orange"/></svg></body></html>`), 0600); e != nil {
		t.Fatal(e)
	}
	c := Defaults()
	c.Browser = bin
	c.BrowserNoSandbox = os.Getenv("RITALIN_TEST_NO_SANDBOX") == "1"
	if e = render(context.Background(), s, c, html, png, func(string) {}); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(png)
	if e != nil || !bytes.HasPrefix(b, []byte{137, 80, 78, 71}) {
		t.Fatal("not PNG", e)
	}
}
