package ritalin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestReviewDoesNotStartBrowserOrCodex(t *testing.T) {
	for _, batch := range []bool{false, true} {
		m := dashboardFixture(t)
		m.c.Command = []string{"missing-codex"}
		m.c.Browser = "missing-browser"
		var cmd tea.Cmd
		if batch {
			cmd = m.activate(entry{action: "batch"})
		} else {
			cmd = m.activate(entry{action: "trial", id: "state-two"})
		}
		if cmd != nil || m.busy || m.review != "state-two" {
			t.Fatal("review must open immediately without starting a job")
		}
	}
}

func TestReviewActionsWhilePreviewRunning(t *testing.T) {
	for _, key := range []rune{'g', 'b', 's'} {
		t.Run(string(key), func(t *testing.T) {
			m := dashboardFixture(t)
			m.review = "state-two"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			task := &previewTask{id: m.review, png: "late.png", cancel: cancel}
			m.preview = task
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
			if ctx.Err() == nil || m.preview != nil || m.review != "" || m.busy {
				t.Fatal("review action blocked by preview")
			}
			m.Update(previewDone{task: task})
			state := findState(&m.c, "state-two")
			if key == 'b' {
				if state != nil {
					t.Fatal("late preview resurrected deleted state")
				}
			} else if state == nil || state.PNG == "late.png" || (key == 'g' && state.Status != "usable") {
				t.Fatal("late preview changed review result")
			}
			if key != 's' {
				c, err := m.store.Load()
				if err != nil {
					t.Fatal(err)
				}
				saved := findState(&c, "state-two")
				if key == 'b' && saved != nil || key == 'g' && (saved == nil || saved.Status != "usable") {
					t.Fatal("review decision not persisted")
				}
			}
		})
	}
}

func TestPreviewFailureDoesNotBlockReview(t *testing.T) {
	m := dashboardFixture(t)
	m.review = "state-two"
	m.c.Browser = filepath.Join(t.TempDir(), "missing-browser")
	cmd := m.startPreview()
	if cmd == nil || m.busy || m.review == "" {
		t.Fatal("preview must be a separate background command")
	}
	done := cmd().(previewDone)
	if done.err == nil {
		t.Fatal("missing browser should fail")
	}
	done.err = errors.New("No usable sandbox")
	m.Update(done)
	if m.review != "state-two" || m.busy || m.preview != nil || findState(&m.c, m.review).Status != "review" {
		t.Fatal("preview failure blocked review")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if findState(&m.c, "state-two").Status != "usable" {
		t.Fatal("cannot keep after preview failure")
	}
}

func TestPelicanResumesHTMLWithoutCredentialsOrBrowser(t *testing.T) {
	m := dashboardFixture(t)
	state := findState(&m.c, "state-two")
	state.HTML = filepath.Join(m.store.Root, "existing.html")
	if err := atomicWrite(state.HTML, []byte("<html></html>"), 0600); err != nil {
		t.Fatal(err)
	}
	m.c.Command = []string{"missing-codex"}
	m.c.Browser = "missing-browser"
	if err := pelican(context.Background(), m.store, &m.c, state.ID, func(string) {}); err != nil {
		t.Fatal("existing HTML should be reviewable offline", err)
	}
}
