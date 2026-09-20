package ritalin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCompactProgressNodeName(t *testing.T) {
	for _, tc := range []struct {
		name, language, status string
		err                    error
	}{
		{"JP 日本Y01 | IEPL", "zh", "候选已保存", nil},
		{"HK 香港Y02 | IEPL", "zh", "connection failed", errors.New("connection failed")},
		{"US 01", "en", "Candidate saved", nil},
	} {
		r := probeResult{nodeName: tc.name, err: tc.err}
		want := "compact 2/43 · " + tc.name + " · " + tc.status
		if got := r.progress(2, 43, tc.language); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestCompactFailureKeepsNodeName(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := Node{ID: "node-test", Name: "JP 日本Y01"}
	r := compact(ctx, n, "", t.TempDir(), "test-model", auth{})
	if r.err == nil || r.nodeName != n.Name || r.attempt.Node != n.ID {
		t.Fatalf("failure lost node identity: %+v", r)
	}
	if !strings.Contains(r.progress(1, 43, "zh"), n.Name) {
		t.Fatal("failed node missing from progress")
	}
}
