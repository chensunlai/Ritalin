package ritalin

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestClearListsRequireConfirmationAndKeepFiles(t *testing.T) {
	for _, target := range []struct {
		tab        int
		action, id string
	}{
		{tabProxies, "clear", "all"}, {tabProxies, "clear", "proxy"}, {tabProxies, "clear", "clash"},
		{tabTest, "clear-states", "pending"}, {tabUse, "clear-states", "usable"},
	} {
		t.Run(target.id, func(t *testing.T) {
			m := dashboardFixture(t)
			m.c.States = append(m.c.States, State{ID: "pending", Status: "pending"})
			m.c.Active = "state-one"
			m.tab = target.tab
			path := filepath.Join(m.store.Root, "pelican.html")
			if err := atomicWrite(path, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			m.c.States[1].HTML = path
			found := false
			for _, e := range m.entries() {
				if e.action == target.action && e.id == target.id {
					m.activate(e)
					found = true
					break
				}
			}
			if !found || m.confirm == "" || len(m.c.Nodes) != 2 || len(m.c.States) != 3 {
				t.Fatal("clear must ask before mutation")
			}
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
			if len(m.c.Nodes) != 2 || len(m.c.States) != 3 {
				t.Fatal("cancel changed lists")
			}
			m.activate(entry{action: target.action, id: target.id})
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
			c, err := m.store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if target.action == "clear" {
				want := 1
				if target.id == "all" {
					want = 0
				}
				if len(c.Nodes) != want || len(c.States) != 3 {
					t.Fatal("wrong node list cleared")
				}
			} else if target.id == "pending" {
				if len(c.States) != 1 || c.States[0].Status != "usable" || c.Active != "state-one" {
					t.Fatal("wrong pending list cleared")
				}
			} else if len(c.States) != 2 || c.Active != "" {
				t.Fatal("usable list or active selection not cleared")
			}
			if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
				t.Fatal("result file removed")
			}
		})
	}
}
