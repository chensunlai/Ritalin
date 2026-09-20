package ritalin

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAppServerStreamsBeforeCompletion(t *testing.T) {
	t.Setenv("RITALIN_TEST_PELICAN", "stream")
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-test.run=^TestPelicanHelper$", "--", "app-server")
	cmd.Dir = t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var firstDelta, completed time.Time
	var output strings.Builder
	err = appServerTurn(ctx, cmd, "synthetic-model", "low", "test", func(method string, p appParams) error {
		if method == "item/agentMessage/delta" {
			if firstDelta.IsZero() {
				firstDelta = time.Now()
			}
			output.WriteString(p.Delta)
		}
		if method == "turn/completed" {
			completed = time.Now()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstDelta.IsZero() || completed.Sub(firstDelta) < 150*time.Millisecond || !strings.HasPrefix(output.String(), "<!doctype html>") {
		t.Fatal("output was not streamed before completion")
	}
	if cmd.ProcessState == nil {
		t.Fatal("app-server not reaped")
	}
}
