package ritalin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func importTestNodes(count int) []Node {
	nodes := make([]Node, count)
	for i := range nodes {
		kind := "proxy"
		if i%2 == 1 {
			kind = "clash"
		}
		nodes[i] = Node{ID: fmt.Sprint(i), Name: fmt.Sprint("node-", i), Kind: kind}
	}
	return nodes
}

func TestImportedNodesFourWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := &Store{Root: t.TempDir()}
	c := Defaults()
	nodes := importTestNodes(12)
	started := make(chan struct{}, len(nodes))
	release := make(chan struct{})
	var active, peak atomic.Int32
	check := func(ctx context.Context, n Node) (Node, error) {
		now := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); now > old; old = peak.Load() {
			if peak.CompareAndSwap(old, now) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
			n.Checked, n.ExitIP = "checked", "192.0.2.1"
			n.Reach = []string{"models: HTTP 401"}
			return n, nil
		case <-ctx.Done():
			return n, ctx.Err()
		}
	}
	var messages []string
	done := make(chan error, 1)
	go func() {
		done <- checkImportedNodes(ctx, s, &c, nodes, check, func(s string) { messages = append(messages, s) })
	}()
	for range 4 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("four checks did not start concurrently")
		}
	}
	if active.Load() != 4 {
		t.Fatal("expected four active checks")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 4 || active.Load() != 0 {
		t.Fatal("worker limit or cleanup failed", peak.Load(), active.Load())
	}
	stored, err := s.Load()
	if err != nil || len(stored.Nodes) != len(nodes) {
		t.Fatal("successful nodes were not saved", err)
	}
	for _, n := range stored.Nodes {
		if n.Checked != "checked" || n.ExitIP == "" || len(n.Reach) != 1 {
			t.Fatal("node check metadata was lost")
		}
	}
	if !strings.Contains(strings.Join(messages, "\n"), "12/12") {
		t.Fatal("completed progress was not reported")
	}
}

func TestImportedNodesFilterAndDeduplicate(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	c := Defaults()
	nodes := importTestNodes(4)
	c.Nodes = append(c.Nodes, nodes[0])
	nodes = append(nodes, nodes[1])
	err := checkImportedNodes(context.Background(), s, &c, nodes, func(_ context.Context, n Node) (Node, error) {
		if n.ID == "2" {
			return n, errors.New("unreachable")
		}
		return n, nil
	}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.Load()
	if err != nil || len(stored.Nodes) != 3 {
		t.Fatal("unexpected saved nodes", err)
	}
	seen := map[string]bool{}
	for _, n := range stored.Nodes {
		if n.ID == "2" || seen[n.ID] {
			t.Fatal("failed or duplicate node was saved")
		}
		seen[n.ID] = true
	}
}

func TestImportedNodesCancelKeepsProgress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := &Store{Root: t.TempDir()}
	c := Defaults()
	var checked atomic.Int32
	err := checkImportedNodes(ctx, s, &c, importTestNodes(12), func(_ context.Context, n Node) (Node, error) {
		checked.Add(1)
		return n, nil
	}, func(message string) {
		if strings.HasPrefix(message, "[2/12]") {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || checked.Load() >= 12 {
		t.Fatal("cancellation did not stop pending checks", err, checked.Load())
	}
	stored, err := s.Load()
	if err != nil || len(stored.Nodes) == 0 || len(stored.Nodes) != len(c.Nodes) {
		t.Fatal("cancellation lost completed saves", err)
	}
}

func TestImportedNodesSaveFailureCancelsWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Store{Root: filepath.Join(blocked, "ritalin")}
	c := Defaults()
	var active atomic.Int32
	err := checkImportedNodes(ctx, s, &c, importTestNodes(12), func(ctx context.Context, n Node) (Node, error) {
		active.Add(1)
		defer active.Add(-1)
		if n.ID == "0" {
			return n, nil
		}
		<-ctx.Done()
		return n, ctx.Err()
	}, func(string) {})
	if err == nil || ctx.Err() != nil || active.Load() != 0 || len(c.Nodes) != 0 {
		t.Fatal("save failure did not cleanly stop workers and roll back", err)
	}
}
