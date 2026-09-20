package ritalin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type Emit func(string)

func parseProxies(input string) ([]Node, error) {
	nodes := []Node{}
	seen := map[string]bool{}
	for _, line := range strings.FieldsFunc(input, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == ';' }) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, e := parseProxy(line)
		if e != nil {
			return nil, e
		}
		v := u.String()
		if seen[v] {
			continue
		}
		seen[v] = true
		nodes = append(nodes, Node{ID: hash(v)[:16], Name: proxyLabel(v), Kind: "proxy", URL: v})
	}
	if len(nodes) == 0 {
		return nil, errors.New("没有代理")
	}
	if len(nodes) > 1000 {
		return nil, errors.New("最多导入 1000 个代理")
	}
	return nodes, nil
}
func reachable(ctx context.Context, proxyURL string) ([]string, error) {
	tr, e := transport(proxyURL)
	if e != nil {
		return nil, e
	}
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	good := []string{}
	for _, target := range []string{"https://api.openai.com/v1/models", "https://chatgpt.com/backend-api/codex/models?client_version=0.155.1"} {
		req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
		req.Header.Set("User-Agent", "codex_cli_rs/0.155.1")
		resp, e := client.Do(req)
		if e != nil {
			continue
		}
		b, e := readLimit(resp.Body, 512<<10)
		resp.Body.Close()
		if e != nil {
			continue
		}
		// 401 JSON proves endpoint reachability, not successful authentication.
		if (resp.StatusCode == 200 || resp.StatusCode == 401) && strings.Contains(resp.Header.Get("Content-Type"), "json") && json.Valid(b) {
			good = append(good, fmt.Sprintf("%s: HTTP %d", req.URL.Host, resp.StatusCode))
		}
	}
	if len(good) == 0 {
		return nil, errors.New("models 未返回 200/401 JSON（不可达/拦截/接口变化）")
	}
	return good, nil
}
func importNodes(ctx context.Context, s *Store, c *Config, kind, input string, emit Emit) error {
	var nodes []Node
	var e error
	if kind == "clash" {
		var b []byte
		b, e = readSource(ctx, input)
		if e == nil {
			nodes, e = parseClash(b)
		}
	} else {
		if b, err := os.ReadFile(ExpandPath(input)); err == nil {
			input = string(b)
		}
		nodes, e = parseProxies(input)
	}
	if e != nil {
		return e
	}
	run, e := startClash(ctx, s, *c, nodes, emit)
	if e != nil {
		return e
	}
	defer run.Close()
	saved := map[string]bool{}
	for _, n := range c.Nodes {
		saved[n.ID] = true
	}
	passed := 0
	for i, n := range nodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		emit(fmt.Sprintf("[%d/%d] %s · models 探测（不跟随系统代理）", i+1, len(nodes), safeText(n.Name)))
		reach, err := reachable(ctx, run.URLs[n.ID])
		if err != nil {
			emit("未保存：" + safeText(n.Name))
			continue
		}
		passed++
		if saved[n.ID] {
			continue
		}
		n.Checked = stamp()
		n.Reach = reach
		c.Nodes = append(c.Nodes, n)
		saved[n.ID] = true
		if e = s.Save(*c); e != nil {
			return e
		}
	}
	emit(fmt.Sprintf("探测结束：%d/%d 可达；401 仅表示鉴权入口可达", passed, len(nodes)))
	return nil
}
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}
