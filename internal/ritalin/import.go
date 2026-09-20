package ritalin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
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

func exitIP(ctx context.Context, proxyURL string) string {
	tr, e := transport(proxyURL)
	if e != nil {
		return ""
	}
	cl := &http.Client{Transport: tr, Timeout: 6 * time.Second}
	defer cl.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.ipify.org", nil)
	resp, e := cl.Do(req)
	if e != nil {
		return ""
	}
	defer resp.Body.Close()
	b, e := readLimit(resp.Body, 128)
	if e != nil {
		return ""
	}
	ip := net.ParseIP(strings.TrimSpace(string(b)))
	if ip == nil {
		return ""
	}
	return ip.String()
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
	return checkImportedNodes(ctx, s, c, nodes, func(ctx context.Context, n Node) (Node, error) {
		reach, err := reachable(ctx, run.URLs[n.ID])
		if err != nil {
			return n, err
		}
		n.Checked = stamp()
		n.Reach = reach
		n.ExitIP = exitIP(ctx, run.URLs[n.ID])
		return n, nil
	}, emit)
}

// Workers only check nodes. Progress and config writes stay on the caller.
func checkImportedNodes(ctx context.Context, s *Store, c *Config, nodes []Node, check func(context.Context, Node) (Node, error), emit Emit) error {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	jobs := make(chan Node, len(nodes))
	for _, n := range nodes {
		jobs <- n
	}
	close(jobs)
	type result struct {
		node Node
		err  error
	}
	results := make(chan result)
	for range min(4, len(nodes)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				if ctx.Err() != nil {
					return
				}
				n, err := check(ctx, n)
				select {
				case results <- result{n, err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()
	saved := map[string]bool{}
	for _, n := range c.Nodes {
		saved[n.ID] = true
	}
	passed, completed := 0, 0
	emit(fmt.Sprintf(uiText(c.Language, "检测 %d 个节点（最多 4 个并行）"), len(nodes)))
	for r := range results {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n := r.node
		completed++
		emit(fmt.Sprintf(uiText(c.Language, "[%d/%d] 已检测 %s"), completed, len(nodes), safeText(n.Name)))
		if r.err != nil {
			emit(uiText(c.Language, "未保存：") + safeText(n.Name))
			continue
		}
		passed++
		if saved[n.ID] {
			continue
		}
		if n.ExitIP != "" {
			emit(uiText(c.Language, "出口 IP：") + n.ExitIP)
		}
		c.Nodes = append(c.Nodes, n)
		if e := s.Save(*c); e != nil {
			c.Nodes = c.Nodes[:len(c.Nodes)-1]
			return e
		}
		saved[n.ID] = true
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	emit(fmt.Sprintf(uiText(c.Language, "检测完成：%d/%d 可用"), passed, len(nodes)))
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
