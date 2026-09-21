package ritalin

import (
	"context"
	"net/http"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTestNodeUsesSourceOrExplicitBinding(t *testing.T) {
	m := nodeUseFixture(t)
	state := &m.c.States[0]
	state.UseNodeID = "node-two"
	if testNode(&m.c, state).ID != "node-one" || useNode(&m.c, state).ID != "node-two" {
		t.Fatal("test must retain source independently of the normal use override")
	}
	state.NodeID = ""
	if testNode(&m.c, state).ID != "node-two" {
		t.Fatal("imported binding was not used")
	}
	state.UseNodeID = ""
	if testNode(&m.c, state) != nil {
		t.Fatal("test inferred a node from the source label")
	}
	w, closeWarp, err := startTestWarp(context.Background(), m.store, m.c, state, func(string) {})
	if err == nil || w != nil || closeWarp != nil {
		t.Fatal("test fell back to default connection without a node")
	}
}

func TestTestWarpUsesImportedBinding(t *testing.T) {
	t.Setenv("CODEX_CA_CERTIFICATE", "")
	t.Setenv("SSL_CERT_FILE", "")
	m := nodeUseFixture(t)
	state := &m.c.States[0]
	state.NodeID, state.UseNodeID = "", "node-one"
	m.c.UseNode = false
	w, closeWarp, err := startTestWarp(context.Background(), m.store, m.c, state, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer closeWarp()
	req, _ := http.NewRequest("GET", "https://chatgpt.com/backend-api/codex/responses", nil)
	u, err := w.tr.Proxy(req)
	if err != nil || u == nil || u.String() != m.c.Nodes[0].URL || m.c.UseNode {
		t.Fatal("test ignored its explicit binding or changed normal use mode")
	}
}

func TestTrialNodePickerAndBatchResume(t *testing.T) {
	for _, batch := range []bool{false, true} {
		m := nodeUseFixture(t)
		m.tab = tabTest
		m.c.Active = "state-one"
		m.c.UseNode = false
		m.c.States[1].Status, m.c.States[1].HTML = "pending", ""
		if batch {
			m.activate(entry{action: "batch"})
		} else {
			m.activate(entry{action: "trial", id: "state-two"})
		}
		if m.busy || !m.routeTest || m.routeState != "state-two" || m.batch != batch {
			t.Fatal("test did not wait for a node selection")
		}
		cmd := m.activate(entry{action: "route-pick", id: "node-one"})
		if cmd == nil || !m.busy || m.routeState != "" || m.batch != batch || m.c.Active != "" || m.c.UseNode || m.c.States[1].Status != "pending" {
			t.Fatal("picker failed to resume test or changed the use configuration")
		}
		// The fixture has no credentials: safely finish without an external call.
		for m.busy {
			select {
			case msg := <-m.events:
				m.Update(msg)
			case <-time.After(3 * time.Second):
				m.cancel()
				t.Fatal("test job did not finish")
			}
		}
		c, err := m.store.Load()
		if err != nil || c.States[1].UseNodeID != "node-one" || c.Active != "" || c.UseNode {
			t.Fatal("test binding or active-state isolation not persisted", err)
		}
	}
}

func TestCancelTrialNodePickerAndReviewWithoutNode(t *testing.T) {
	m := nodeUseFixture(t)
	m.c.States[1].Status, m.c.States[1].HTML = "pending", ""
	m.activate(entry{action: "batch"})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.batch || m.routeState != "" || m.busy || m.c.States[1].UseNodeID != "" {
		t.Fatal("cancel left a running batch or changed the binding")
	}
	m.c.Nodes = nil
	m.c.States[1].Status, m.c.States[1].HTML = "review", "/example/result.html"
	m.activate(entry{action: "trial", id: "state-two"})
	if m.review != "state-two" || m.routeState != "" || m.busy {
		t.Fatal("review was blocked by node availability")
	}
}
