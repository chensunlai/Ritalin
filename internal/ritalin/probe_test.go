package ritalin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestProbeProgressNodeName(t *testing.T) {
	for _, tc := range []struct {
		name, language, status string
		err                    error
	}{
		{"JP 日本Y01 | IEPL", "zh", "候选已保存", nil},
		{"HK 香港Y02 | IEPL", "zh", "connection failed", errors.New("connection failed")},
		{"US 01", "en", "Candidate saved", nil},
	} {
		r := probeResult{nodeName: tc.name, err: tc.err}
		want := uiText(tc.language, "探测") + " 2/43 · " + tc.name + " · " + tc.status
		if got := r.progress(2, 43, tc.language); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestProbeFailureKeepsNodeName(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := Node{ID: "node-test", Name: "JP 日本Y01"}
	r := probeState(ctx, n, "", t.TempDir(), "test-model", auth{})
	if r.err == nil || r.nodeName != n.Name || r.attempt.Node != n.ID {
		t.Fatalf("failure lost node identity: %+v", r)
	}
	if !strings.Contains(r.progress(1, 43, "zh"), n.Name) {
		t.Fatal("failed node missing from progress")
	}
}

type probeTransportFunc func(*http.Request) (*http.Response, error)

func (f probeTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestShortResponsesProbe(t *testing.T) {
	state := syntheticState()
	completed := "data: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n"
	failed := "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\"}}}\n\n"
	for _, tc := range []struct {
		name, state, body                   string
		status                              int
		wantState, wantCompleted, wantError bool
	}{
		{"completed without compaction", state, completed, 200, true, true, false},
		{"missing state", "", completed, 200, false, true, true},
		{"invalid state", "invalid", completed, 200, false, true, true},
		{"no terminal event", state, "data: {\"type\":\"response.created\"}\n\n", 200, true, false, true},
		{"capacity failure", state, failed, 200, true, false, true},
		{"failure then completion", state, failed + completed, 200, true, false, true},
		{"incomplete", state, "data: {\"type\":\"response.incomplete\"}\n\n", 200, true, false, true},
		{"unauthorized", state, "{}", 401, false, false, true},
		{"rate limited", state, "{}", 429, false, false, true},
		{"redirect not followed", state, "", 302, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			tr := probeTransportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != "POST" || req.URL.String() != "https://chatgpt.com/backend-api/codex/responses" {
					t.Fatalf("unexpected target: %s %s", req.Method, req.URL)
				}
				if req.Header.Get("Authorization") != "Bearer test-token" || req.Header.Get("ChatGPT-Account-Id") != "test-account" {
					t.Fatal("current credentials missing")
				}
				if req.Header.Get(stateHeader) != "" || req.Header.Get("Accept") != "text/event-stream" {
					t.Fatal("probe must request SSE without injecting a turn-state")
				}
				var got, want any
				if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(`{"model":"test-model","instructions":"Reply with OK.","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with OK."}]}],"stream":true,"store":false,"parallel_tool_calls":true,"include":["reasoning.encrypted_content"]}`), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("unexpected probe payload: %#v", got)
				}
				h := make(http.Header)
				h.Set(stateHeader, tc.state)
				h.Set("Location", "https://example.invalid/do-not-follow")
				return &http.Response{StatusCode: tc.status, Header: h, Body: io.NopCloser(strings.NewReader(tc.body)), Request: req}, nil
			})
			r := probeStateWithTransport(context.Background(), Node{ID: "test-node", Name: "Test node"}, "test-home", "test-model", auth{"test-token", "test-account"}, tr)
			if calls != 1 || r.attempt.AutomaticRetries {
				t.Fatal("probe retried")
			}
			if (r.err != nil) != tc.wantError || (r.state != nil) != tc.wantState || r.attempt.Completed != tc.wantCompleted {
				t.Fatalf("unexpected result: %+v", r)
			}
			if r.state != nil && (r.state.ProbeCompleted != tc.wantCompleted || r.state.Status != "pending" || r.state.Value != state || r.state.AuthHome != "test-home") {
				t.Fatalf("unexpected candidate: %+v", r.state)
			}
			if strings.Contains(tc.body, "server_is_overloaded") && r.attempt.Events[0].Code != "server_is_overloaded" {
				t.Fatal("capacity code not recorded")
			}
		})
	}
}

func TestProbeInterruptedStreamRetainsCandidate(t *testing.T) {
	tr := probeTransportFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set(stateHeader, syntheticState())
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(probeBrokenReader{}), Request: req}, nil
	})
	r := probeStateWithTransport(context.Background(), Node{}, "test-home", "test-model", auth{}, tr)
	if r.err == nil || r.state == nil || r.state.ProbeCompleted || r.attempt.Completed {
		t.Fatalf("interrupted probe lost header or was marked complete: %+v", r)
	}
}

type probeBrokenReader struct{}

func (probeBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
