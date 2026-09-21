package ritalin

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func transferFixture(t *testing.T) *ui {
	m := dashboardFixture(t)
	raw, err := base64.URLEncoding.DecodeString(syntheticState())
	if err != nil {
		t.Fatal(err)
	}
	raw[12] ^= 1
	m.c.States[1].Value = base64.URLEncoding.EncodeToString(raw)
	m.c.States[0].AuthHome = "/private/credential-home"
	m.c.States[0].NodeID, m.c.States[0].UseNodeID = "private-source-node", "private-use-node"
	m.c.States[1].NodeID, m.c.States[1].UseNodeID = "private-source-node", "private-use-node"
	m.c.States[1].AuthHome = "/private/credential-home"
	m.c.States[1].AccountHash = "account-fingerprint"
	m.c.Active = "state-one"
	return m
}

func TestStateFileRoundTrip(t *testing.T) {
	for _, sourceUsable := range []bool{false, true} {
		for _, targetUsable := range []bool{false, true} {
			m := transferFixture(t)
			path := filepath.Join(t.TempDir(), "states.json")
			count, err := exportStates(m.c, path, sourceUsable)
			if err != nil || count != 1 {
				t.Fatal(count, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			index := 1
			if sourceUsable {
				index = 0
			}
			source := m.c.States[index]
			if !strings.Contains(string(data), source.Value) {
				t.Fatal("export must include the complete turn-state")
			}
			var wire struct {
				Version int              `json:"version"`
				States  []map[string]any `json:"states"`
			}
			if err := json.Unmarshal(data, &wire); err != nil || wire.Version != 2 || len(wire.States) != 1 {
				t.Fatal("unexpected export format", err)
			}
			if wire.States[0]["x-codex-turn-state"] != source.Value || wire.States[0]["value"] != nil {
				t.Fatal("both pages must export the full state under x-codex-turn-state")
			}
			for _, forbidden := range []string{"node_id", "use_node_id", "private-source-node", "private-use-node", "auth_home", "HTML", "html", "png", "active", "credential-home", "hidden-password", "browser_no_sandbox"} {
				if strings.Contains(string(data), forbidden) {
					t.Fatalf("non-portable field exported: %s", forbidden)
				}
			}
			if runtime.GOOS != "windows" {
				info, _ := os.Stat(path)
				if info.Mode().Perm() != 0600 {
					t.Fatal("export permissions too broad")
				}
			}
			c := Defaults()
			added, skipped, err := importStates(m.store, &c, path, targetUsable)
			if err != nil || added != 1 || skipped != 0 || len(c.States) != 1 {
				t.Fatal(added, skipped, err)
			}
			got := c.States[0]
			if got.NodeID != "" || got.UseNodeID != "" {
				t.Fatal("imported state must require an explicit node selection")
			}
			if got.Value != source.Value || got.Node != source.Node || got.Model != source.Model || got.AccountHash != source.AccountHash || (got.Status == "usable") != targetUsable || c.Active != "" || got.HTML != "" || got.PNG != "" || got.ID == source.ID {
				t.Fatal("portable state not restored correctly")
			}
			metrics, _ := parseState(source.Value)
			if got.Metrics != metrics || got.AuthHome != probeHome(m.store, c) {
				t.Fatal("local metadata not recalculated")
			}
			before := clone(c)
			added, skipped, err = importStates(m.store, &c, path, !targetUsable)
			if err != nil || added != 0 || skipped != 1 || !reflect.DeepEqual(c, before) {
				t.Fatal("duplicate changed existing state")
			}
			if _, err = exportStates(m.c, path, sourceUsable); err == nil {
				t.Fatal("overwrote existing export")
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(data) {
				t.Fatal("existing file modified")
			}
		}
	}
}

func TestInvalidStateImportIsAtomic(t *testing.T) {
	m := transferFixture(t)
	for _, input := range []string{
		`{`, `{"format":"wrong","version":2,"states":[]}`,
		`{"format":"codex-ritalin/states","version":1,"states":[{"value":"` + syntheticState() + `"}]}`,
		`{"format":"codex-ritalin/states","version":3,"states":[]}`,
		`{"format":"codex-ritalin/states","version":2,"states":[{"value":"` + syntheticState() + `"}]}`,
		`{"format":"codex-ritalin/states","version":2,"states":[{"x-codex-turn-state":"` + syntheticState() + `"},{"x-codex-turn-state":"secret-invalid-value"}]}`,
	} {
		path := filepath.Join(t.TempDir(), "invalid.json")
		if err := atomicWrite(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		c := Defaults()
		before := clone(c)
		if _, _, err := importStates(m.store, &c, path, true); err == nil || strings.Contains(err.Error(), "secret-invalid-value") {
			t.Fatal("invalid input accepted or leaked")
		}
		if !reflect.DeepEqual(before, c) {
			t.Fatal("partial import")
		}
	}
	// A failed config save must not mutate the live list.
	path := filepath.Join(t.TempDir(), "states.json")
	if _, err := exportStates(m.c, path, true); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := atomicWrite(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Defaults()
	if _, _, err := importStates(&Store{Root: blocked}, &c, path, true); err == nil || len(c.States) != 0 {
		t.Fatal("failed save mutated config")
	}
}

func TestStateImportDeduplicatesWithinFile(t *testing.T) {
	m := transferFixture(t)
	c := Defaults()
	path := filepath.Join(t.TempDir(), "states.json")
	data := stateFile{Format: stateFileFormat, Version: stateFileVersion, States: []portableState{
		{Value: syntheticState()}, {Value: strings.TrimRight(syntheticState(), "=")},
	}}
	b, _ := json.Marshal(data)
	if err := atomicWrite(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	added, skipped, err := importStates(m.store, &c, path, false)
	if err != nil || added != 1 || skipped != 1 {
		t.Fatal(added, skipped, err)
	}
}

func TestStateTransferUI(t *testing.T) {
	for _, tab := range []int{tabTest, tabUse} {
		m := transferFixture(t)
		m.tab = tab
		for _, action := range []string{"export-states", "import-states"} {
			found := false
			for _, entry := range m.entries() {
				if entry.action == action {
					m.activate(entry)
					found = true
					break
				}
			}
			if !found || m.form == "" {
				t.Fatal("transfer entry missing")
			}
			if action == "export-states" {
				path := m.input.Value()
				m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var exported stateFile
				if err := json.Unmarshal(data, &exported); err != nil || len(exported.States) != 1 {
					t.Fatal("wrong page exported", err)
				}
			} else {
				m.input.SetValue(filepath.Join(t.TempDir(), "missing.json"))
				m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if m.form == "" || m.notice == "" {
					t.Fatal("import error did not stay editable")
				}
			}
		}
	}
}
