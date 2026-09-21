package ritalin

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func nextWarpState(t *testing.T, events <-chan warpStateEvent) warpStateEvent {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("state observation missing")
		return warpStateEvent{}
	}
}

func TestWarpStateSnapshotAndDisplay(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses?private-query=secret", nil)
	req.Header.Set("Authorization", "Bearer private-token")
	req.Header.Set(stateHeader, syntheticState())
	e := newWarpStateEvent(7, req, nil, false)
	req.Header.Set(stateHeader, "changed-after-snapshot")
	for _, lang := range []string{"zh", "en"} {
		text := e.text(lang)
		for _, want := range []string{"#7", "POST", "/backend-api/codex/responses", "292", "160", "10", syntheticState()} {
			if !strings.Contains(text, want) {
				t.Fatalf("state display missing %s", want)
			}
		}
		for _, unwanted := range []string{"private-query", "private-token", "changed-after-snapshot"} {
			if strings.Contains(text, unwanted) {
				t.Fatal("observation retained unrelated data or a mutable header")
			}
		}
	}
	resp := &http.Response{StatusCode: 401, Header: http.Header{}}
	e = newWarpStateEvent(7, req, resp, true)
	if text := e.text("en"); !strings.Contains(text, "HTTP 401") || !strings.Contains(text, "Absent") {
		t.Fatal("absent response state not identified")
	}
	resp.Header.Set(stateHeader, "")
	resp.Header.Add(stateHeader, "invalid\x1b[31m")
	e = newWarpStateEvent(7, req, resp, true)
	if text := e.text("en"); strings.Contains(text, "Absent") || strings.Contains(text, "\x1b") || !strings.Contains(text, "Unrecognized layout") || !strings.Contains(text, `""`) {
		t.Fatal("empty, repeated or invalid state was misrepresented")
	}
	e = newWarpStateEvent(7, req, nil, true)
	if !strings.Contains(e.text("en"), "No HTTP response received") {
		t.Fatal("transport failure presented as an absent server header")
	}
}

func TestStateObservationDoesNotMixWithModelOutput(t *testing.T) {
	m := dashboardFixture(t)
	req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", nil)
	req.Header.Set(stateHeader, syntheticState())
	m.Update(logMsg(newWarpStateEvent(7, req, nil, false).text("en")))
	m.Update(logMsg("\x00output:model and tool output"))
	if !strings.Contains(m.log, "\nx-codex-turn-state: ") || !strings.Contains(m.log, syntheticState()) || m.modelOutput != "model and tool output" {
		t.Fatal("state log lost line breaks or contaminated app-server output")
	}
}
