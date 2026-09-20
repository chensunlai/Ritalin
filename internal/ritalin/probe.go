package ritalin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const stateHeader = "x-codex-turn-state"

func parseState(value string) (Metrics, error) {
	m := Metrics{Characters: len(value)}
	raw, e := base64.URLEncoding.DecodeString(value + strings.Repeat("=", (4-len(value)%4)%4))
	if e != nil {
		return m, errors.New("turn-state 不是有效 Base64URL")
	}
	m.DecodedBytes = len(raw)
	if len(raw) < 73 {
		return m, errors.New("turn-state 太短，无法按 1.py 布局解析")
	}
	m.Version = int(raw[0])
	m.Timestamp = binary.BigEndian.Uint64(raw[1:9])
	m.CipherBytes = len(raw) - 25 - 32
	m.Blocks = m.CipherBytes / 16
	if m.Version != 128 || m.CipherBytes%16 != 0 {
		return m, errors.New("turn-state 不符合预期布局（不代表服务端实际加密协议）")
	}
	return m, nil
}
func lengthMatch(m Metrics, team bool) bool {
	if team {
		return m.Characters == 332 && m.Blocks == 12
	}
	return m.Characters == 292 && m.Blocks == 10
}

type auth struct{ Token, Account string }

func readAuth(home string) (auth, error) {
	b, e := os.ReadFile(filepath.Join(home, "auth.json"))
	if e != nil {
		return auth{}, errors.New("无法读取指定 CODEX_HOME/auth.json；请先用 Codex 登录并使用 file 凭证存储")
	}
	var a struct {
		AuthMode string `json:"auth_mode"`
		Tokens   struct {
			Access  string `json:"access_token"`
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(b, &a) != nil || a.Tokens.Access == "" {
		return auth{}, errors.New("compact 实验需要 ChatGPT 登录凭证，不支持 API Key / keyring-only")
	}
	return auth{a.Tokens.Access, a.Tokens.Account}, nil
}
func modelFor(c Config, home string) (string, error) {
	if c.Model != "" {
		return c.Model, nil
	}
	b, e := os.ReadFile(filepath.Join(home, "config.toml"))
	if e != nil {
		return "", errors.New("请在设置中填写探测模型，或在指定 CODEX_HOME/config.toml 设置 model")
	}
	var cfg struct {
		Model string `toml:"model"`
	}
	if toml.Unmarshal(b, &cfg) != nil || cfg.Model == "" {
		return "", errors.New("无法读取模型，请在设置中指定")
	}
	return cfg.Model, nil
}

type Attempt struct {
	Node             string   `json:"node"`
	Started          string   `json:"started"`
	HTTP             int      `json:"http_status"`
	Completed        bool     `json:"completed"`
	HasState         bool     `json:"has_state"`
	Metrics          *Metrics `json:"metrics,omitempty"`
	Events           []Event  `json:"events"`
	Error            string   `json:"error,omitempty"`
	Seconds          float64  `json:"seconds"`
	AutomaticRetries bool     `json:"automatic_retries"`
}
type Event struct {
	At   string `json:"at"`
	Type string `json:"type"`
	Code string `json:"code,omitempty"`
}
type probeResult struct {
	state   *State
	attempt Attempt
	err     error
}

func compact(ctx context.Context, n Node, proxyURL, home, model string, a auth) probeResult {
	start := time.Now()
	r := probeResult{attempt: Attempt{Node: n.ID, Started: stamp(), Events: []Event{}}}
	finish := func(err error) probeResult {
		r.err = err
		if err != nil {
			r.attempt.Error = err.Error()
		}
		r.attempt.Seconds = time.Since(start).Seconds()
		return r
	}
	payload := map[string]any{"model": model, "instructions": "Preserve the task state in this synthetic test conversation.", "input": []any{
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "Remember the test marker blue-square."}}},
		map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "The marker is blue-square."}}},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "Keep the marker for the next turn."}}}, map[string]any{"type": "compaction_trigger"}},
		"tools": []any{}, "tool_choice": "auto", "parallel_tool_calls": true, "store": false, "stream": true, "include": []string{"reasoning.encrypted_content"}, "reasoning": map[string]string{"effort": "medium"}}
	b, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://chatgpt.com/backend-api/codex/responses", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("ChatGPT-Account-Id", a.Account)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("User-Agent", "codex_cli_rs/0.155.1")
	req.Header.Set("session_id", newID())
	tr, e := transport(proxyURL)
	if e != nil {
		return finish(e)
	}
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return finish(ctx.Err())
		}
		return finish(errors.New("连接/TLS 请求失败"))
	}
	defer resp.Body.Close()
	r.attempt.HTTP = resp.StatusCode
	state := resp.Header.Get(stateHeader)
	r.attempt.HasState = state != ""
	if state != "" && resp.StatusCode == 200 {
		if m, err := parseState(state); err == nil {
			r.attempt.Metrics = &m
			r.state = &State{ID: newID(), Value: state, Node: n.Name, Created: stamp(), Status: "pending", Metrics: m, AuthHome: home, AccountHash: hash(a.Account), Model: model, LastError: "compact 尚未确认完成"}
		}
	}
	compacted, completed := false, false
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 8<<20))
	sc.Buffer(make([]byte, 4096), 2<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Code string `json:"code"`
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
			Response struct {
				Output []struct {
					Type string `json:"type"`
				} `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &ev) != nil {
			continue
		}
		r.attempt.Events = append(r.attempt.Events, Event{At: stamp(), Type: ev.Type, Code: safeText(ev.Code)})
		if ev.Type == "response.completed" {
			completed = true
		}
		if ev.Type == "response.compaction.compacting" || ev.Item.Type == "compaction" || ev.Item.Type == "context_compaction" {
			compacted = true
		}
		for _, o := range ev.Response.Output {
			if o.Type == "compaction" || o.Type == "context_compaction" {
				compacted = true
			}
		}
	}
	if e = sc.Err(); e != nil {
		return finish(errors.New("响应中断或超过大小限制"))
	}
	r.attempt.Completed = resp.StatusCode == 200 && completed && compacted
	if state == "" {
		return finish(fmt.Errorf("HTTP %d，未返回 turn-state", resp.StatusCode))
	}
	metrics, e := parseState(state)
	if e != nil {
		return finish(e)
	}
	r.attempt.Metrics = &metrics
	if !r.attempt.Completed {
		return finish(errors.New("compact 未完成；有效响应头仍保存为待确认，事件记录保留"))
	}
	if r.state != nil {
		r.state.ProbeCompleted = true
		r.state.LastError = ""
	}
	return finish(nil)
}
func probeAll(ctx context.Context, s *Store, c *Config, emit Emit) ([]string, error) {
	if len(c.Nodes) == 0 {
		return nil, errors.New("请先导入代理")
	}
	home := probeHome(s, *c)
	a, e := readAuth(home)
	if e != nil {
		return nil, e
	}
	model, e := modelFor(*c, home)
	if e != nil {
		return nil, e
	}
	run, e := startClash(ctx, s, *c, c.Nodes, emit)
	if e != nil {
		return nil, e
	}
	defer run.Close()
	dir, e := s.runDir("probes")
	if e != nil {
		return nil, e
	}
	jobs := make(chan Node)
	results := make(chan probeResult, 2)
	var wg sync.WaitGroup
	// Exactly two workers at most; no automatic retry of a paid request.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				if ctx.Err() != nil {
					return
				}
				results <- compact(ctx, n, run.URLs[n.ID], home, model, a)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, n := range c.Nodes {
			select {
			case jobs <- n:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	ids := []string{}
	done := 0
	var saveErr error
	for r := range results {
		done++
		if e = s.Record(dir, r.attempt.Node+".json", r.attempt); e != nil {
			saveErr = e
		}
		if r.state != nil {
			duplicate := false
			for _, v := range c.States {
				if v.Value == r.state.Value {
					duplicate = true
					break
				}
			}
			if !duplicate {
				c.States = append(c.States, *r.state)
				ids = append(ids, r.state.ID)
				if e = s.Save(*c); e != nil {
					saveErr = e
				}
			}
		}
		status := "候选已保存"
		if r.err != nil {
			status = r.err.Error()
		}
		emit(fmt.Sprintf("compact %d/%d · %s", done, len(c.Nodes), status))
	}
	emit("探测记录：" + dir)
	if saveErr != nil {
		return ids, saveErr
	}
	return ids, ctx.Err()
}
