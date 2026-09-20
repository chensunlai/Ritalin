package ritalin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

type appError struct {
	Message string          `json:"message"`
	Info    json.RawMessage `json:"codexErrorInfo"`
}

func (e *appError) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &e.Message)
	}
	type plain appError
	return json.Unmarshal(b, (*plain)(e))
}

type appItem struct {
	ID, Type, Text, Status         string
	Command, Cwd, AggregatedOutput string
	ExitCode                       *int
	Server, Tool, Query, Path      string
	Changes                        []struct{ Path string } `json:"changes"`
}

type appParams struct {
	ThreadID  string    `json:"threadId"`
	TurnID    string    `json:"turnId"`
	ItemID    string    `json:"itemId"`
	Delta     string    `json:"delta"`
	Message   string    `json:"message"`
	WillRetry bool      `json:"willRetry"`
	Error     *appError `json:"error"`
	Item      appItem   `json:"item"`
	Turn      struct {
		ID, Status string
		Error      *appError `json:"error"`
	} `json:"turn"`
}

// One isolated app-server per trial. Model output comes exclusively from its
// stdio protocol, never from the HTTP proxy or WebSocket transport.
func appServerTurn(ctx context.Context, cmd *exec.Cmd, model, effort, prompt string, notify func(string, appParams) error) error {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer stdout.Close()
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	defer stderr.Close()
	prepareBackground(cmd)
	if err = cmd.Start(); err != nil {
		return err
	}

	type readEvent struct {
		source, line string
		err          error
		eof          bool
	}
	reads := make(chan readEvent, 64)
	readctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for source, pipe := range map[string]io.Reader{"stdout": stdout, "stderr": stderr} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			send := func(ev readEvent) bool {
				select {
				case reads <- ev:
					return true
				case <-readctx.Done():
					return false
				}
			}
			sc := bufio.NewScanner(pipe)
			sc.Buffer(make([]byte, 8192), 4<<20)
			for sc.Scan() {
				if !send(readEvent{source: source, line: sc.Text()}) {
					return
				}
			}
			send(readEvent{source: source, err: sc.Err(), eof: true})
		}()
	}
	defer func() {
		cancel()
		stdin.Close()
		// A completed turn does not stop the server. Reap our process group on
		// every exit, including filters, cancellation and protocol failures.
		stopBackground(cmd)
		_ = cmd.Wait()
		wg.Wait()
	}()
	enc := json.NewEncoder(stdin)
	send := func(id int, method string, params any) error {
		return enc.Encode(map[string]any{"id": id, "method": method, "params": params})
	}
	if err = send(0, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "codex_ritalin", "title": "Ritalin", "version": "1"},
	}); err != nil {
		return err
	}
	threadID, turnID := "", ""
	stage := 0
	for {
		select {
		case <-ctx.Done():
			if threadID != "" && turnID != "" {
				_ = send(3, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID})
			}
			return ctx.Err()
		case ev := <-reads:
			if ev.eof {
				if ev.err != nil {
					return fmt.Errorf("app-server %s: %w", ev.source, ev.err)
				}
				if ev.source == "stdout" {
					return errors.New("app-server exited before turn/completed")
				}
				continue
			}
			if ev.source == "stderr" {
				if err = notify("stderr", appParams{Message: ev.line}); err != nil {
					return err
				}
				continue
			}
			var msg struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
				Result json.RawMessage `json:"result"`
				Error  *appError       `json:"error"`
			}
			if err = json.Unmarshal([]byte(ev.line), &msg); err != nil {
				return fmt.Errorf("invalid app-server JSON: %w", err)
			}
			if len(msg.ID) > 0 && msg.Method != "" {
				// The benchmark must not approve tools or hang on interactive input.
				_ = enc.Encode(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "Interactive requests are not supported by this benchmark"}})
				return fmt.Errorf("app-server requested interaction: %s", msg.Method)
			}
			if len(msg.ID) > 0 {
				if msg.Error != nil {
					return fmt.Errorf("app-server: %s", msg.Error.Message)
				}
				switch string(msg.ID) {
				case "0":
					if stage != 0 {
						return errors.New("duplicate initialize response")
					}
					stage = 1
					if err = notify("initialized", appParams{}); err != nil {
						return err
					}
					if err = enc.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
						return err
					}
					err = send(1, "thread/start", map[string]any{
						"model": model, "cwd": cmd.Dir, "sandbox": "danger-full-access",
						"approvalPolicy": "never", "ephemeral": true,
					})
				case "1":
					if stage != 1 {
						return errors.New("unexpected thread/start response")
					}
					stage = 2
					var result struct {
						Thread struct{ ID string } `json:"thread"`
					}
					if err = json.Unmarshal(msg.Result, &result); err != nil {
						return err
					}
					threadID = result.Thread.ID
					if threadID == "" {
						return errors.New("app-server returned no thread ID")
					}
					err = send(2, "turn/start", map[string]any{
						"threadId": threadID, "model": model, "effort": effort,
						"input": []any{map[string]any{"type": "text", "text": prompt}},
					})
				case "2":
					var result struct {
						Turn struct{ ID string } `json:"turn"`
					}
					if err = json.Unmarshal(msg.Result, &result); err != nil {
						return err
					}
					turnID = result.Turn.ID
				}
				if err != nil {
					return err
				}
				continue
			}
			var p appParams
			switch msg.Method {
			case "thread/started", "turn/started", "turn/completed", "error", "configWarning",
				"item/agentMessage/delta", "item/started", "item/completed", "item/reasoning/summaryTextDelta",
				"item/plan/delta", "item/commandExecution/outputDelta", "item/fileChange/outputDelta":
				if err = json.Unmarshal(msg.Params, &p); err != nil {
					return fmt.Errorf("app-server %s: %w", msg.Method, err)
				}
			}
			if p.ThreadID != "" && threadID != "" && p.ThreadID != threadID {
				continue
			}
			if msg.Method == "turn/started" {
				turnID = p.Turn.ID
			}
			if err = notify(msg.Method, p); err != nil {
				return err
			}
			if msg.Method == "error" && !p.WillRetry && p.Error != nil {
				return errors.New(p.Error.Message)
			}
			if msg.Method == "turn/completed" {
				if stage != 2 {
					return errors.New("turn completed before it was started")
				}
				if p.Turn.Status == "completed" {
					return nil
				}
				if p.Turn.Error != nil {
					return errors.New(p.Turn.Error.Message)
				}
				return fmt.Errorf("app-server turn %s", p.Turn.Status)
			}
		}
	}
}
