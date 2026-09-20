package ritalin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const pelicanPrompt = `创建一个完整、可直接用浏览器打开的独立 HTML 文件，内容是用 SVG 绘制的“鹈鹕骑自行车”2D 循环动画。
鹈鹕应有长嘴、喉囊、身体、翅膀和腿，自行车结构完整，车轮、脚踏和腿运动协调；构图美观，自动循环，适应屏幕。
仅使用文件内部的 SVG 与 CSS，不使用 JavaScript、外部资源、字体、图片或第三方库。仅输出完整 HTML 源码，不要解释、Markdown 代码围栏或执行工具。文件由测试程序保存。`

func firstSentence(s string) string {
	if i := strings.IndexAny(s, "。！？!?\n"); i >= 0 {
		return s[:i]
	}
	return s
}
func keywordRejected(s string) bool {
	s = firstSentence(s)
	return strings.Contains(s, "内联") || strings.Contains(s, "内嵌")
}
func extractHTML(s string) string {
	lower := strings.ToLower(s)
	start := strings.Index(lower, "<!doctype html")
	if start < 0 {
		start = strings.Index(lower, "<html")
	}
	end := strings.LastIndex(lower, "</html>")
	if start < 0 || end < start {
		return ""
	}
	return s[start : end+len("</html>")]
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 32768 {
		keep := 32768 - b.Len()
		if len(p) > keep {
			p = p[:keep]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
func browserPath(ctx context.Context, s *Store, c Config, emit Emit) (string, error) {
	if c.Browser != "" {
		return exec.LookPath(ExpandPath(c.Browser))
	}
	if bin, ok := launcher.LookPath(); ok {
		return bin, nil
	}
	emit(uiText(c.Language, "准备截图引擎…"))
	b := launcher.NewBrowser()
	b.RootDir = filepath.Join(s.Root, "browser")
	b.Context = ctx
	b.Logger = log.New(io.Discard, "", 0)
	b.HTTPClient = &http.Client{Transport: systemTransport(), Timeout: 10 * time.Minute}
	return b.Get()
}
func render(ctx context.Context, s *Store, c Config, htmlPath, pngPath string, emit Emit) error {
	emit(uiText(c.Language, "正在渲染图片…"))
	bin, e := browserPath(ctx, s, c, emit)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	profile, e := os.MkdirTemp(filepath.Join(s.Root, "pelican"), "browser-profile-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(profile)
	l := launcher.New().Context(ctx).Bin(bin).Headless(true).NoSandbox(c.BrowserNoSandbox).Leakless(false).UserDataDir(profile).Set("disable-gpu").Set("disable-dev-shm-usage").Env(cleanProxyEnv(os.Environ())...)
	// Retain Chromium sandbox unless the user explicitly opts out in settings.
	control, e := l.Launch()
	if e != nil {
		return fmt.Errorf("浏览器启动失败；可在设置中指定已安装的 Chrome/Chromium: %w", e)
	}
	defer l.Kill()
	browser := rod.New().ControlURL(control).Context(ctx)
	if e = browser.Connect(); e != nil {
		return e
	}
	defer browser.Close()
	page, e := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if e != nil {
		return e
	}
	if e = (proto.EmulationSetScriptExecutionDisabled{Value: true}).Call(page); e != nil {
		return e
	}
	// No external resources, including localhost/file URLs in generated HTML.
	router := page.HijackRequests()
	router.Add("*", "", func(h *rod.Hijack) { h.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
	go router.Run()
	defer router.Stop()
	_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{Width: 1100, Height: 850, DeviceScaleFactor: 1})
	b, e := os.ReadFile(htmlPath)
	if e != nil {
		return e
	}
	if e = page.SetDocumentContent(string(b)); e != nil {
		return e
	}
	select {
	case <-time.After(800 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}
	png, e := page.Screenshot(false, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
	if e != nil {
		return e
	}
	return atomicWrite(pngPath, png, 0600)
}

// Each call completes or resumes one trial; only the TUI decides usable/rejected.
func pelican(ctx context.Context, s *Store, c *Config, id string, emit Emit) error {
	state := findState(c, id)
	if state == nil {
		return errors.New("候选不存在")
	}
	if state.HTML != "" {
		if _, e := os.Stat(state.HTML); e == nil {
			if state.PNG == "" {
				state.PNG = filepath.Join(filepath.Dir(state.HTML), "pelican.png")
			}
			if _, e = os.Stat(state.PNG); e != nil {
				if e = render(ctx, s, *c, state.HTML, state.PNG, emit); e != nil {
					return e
				}
			}
			state.Status = "review"
			return s.Save(*c)
		}
	}
	home := state.AuthHome
	if home == "" {
		home = probeHome(s, *c)
	}
	a, e := readAuth(home)
	if e != nil {
		return e
	}
	if state.AccountHash != "" && state.AccountHash != hash(a.Account) {
		return errors.New("此候选的原账号与当前凭证不同；请恢复原账号后再测")
	}
	dir, e := s.runDir("pelican")
	if e != nil {
		return e
	}
	model := state.Model
	if model == "" {
		model, e = modelFor(*c, home)
		if e != nil {
			return e
		}
	}
	runctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	output, e := os.OpenFile(filepath.Join(dir, "output.txt"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer output.Close()
	var outputMu sync.Mutex
	var streamed, filtered atomic.Bool
	firstStream := ""
	display := func(text string) {
		outputMu.Lock()
		defer outputMu.Unlock()
		_, _ = io.WriteString(output, text)
		emit("\x00output:" + text)
	}
	observer := func(kind, text string) {
		display(text)
		if kind == "response.output_text.delta" {
			streamed.Store(true)
			outputMu.Lock()
			if len(firstStream) < 8192 {
				firstStream += text
			}
			reject := c.KeywordFilter && keywordRejected(firstStream)
			outputMu.Unlock()
			if reject {
				filtered.Store(true)
				cancel()
			}
		}
	}
	w, e := startWarp(s, state.Value, true, "", observer)
	if e != nil {
		return e
	}
	defer w.Close()
	args := []string{"exec", "--json", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "-m", model, "-c", "model_reasoning_effort=" + fmt.Sprintf("%q", c.Effort), "-c", `cli_auth_credentials_store="file"`, "-C", filepath.Join(s.Root, "pelican"), "-"}
	cmd, e := codexCommand(*c, args)
	if e != nil {
		return e
	}
	cmd.Dir = filepath.Join(s.Root, "pelican")
	cmd.Env = w.Env(os.Environ())
	cmd.Env = setEnv(cmd.Env, "CODEX_HOME", home)
	for _, key := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN"} {
		env := []string{}
		for _, v := range cmd.Env {
			if !strings.HasPrefix(v, key+"=") {
				env = append(env, v)
			}
		}
		cmd.Env = env
	}
	cmd.Stdin = strings.NewReader(pelicanPrompt)
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	prepareBackground(cmd)
	if e = cmd.Start(); e != nil {
		return e
	}
	done := make(chan struct{})
	var doneOnce sync.Once
	stopWatcher := func() { doneOnce.Do(func() { close(done) }) }
	go func() {
		select {
		case <-runctx.Done():
			stopBackground(cmd)
		case <-done:
		}
	}()
	defer stopWatcher()
	events, e := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		stopBackground(cmd)
		_ = cmd.Wait()
		return e
	}
	defer events.Close()
	enc := json.NewEncoder(events)
	started := time.Now()
	text, first := "", ""
	var scanErr, errorLog error
	sc := bufio.NewScanner(io.LimitReader(stdout, 32<<20))
	sc.Buffer(make([]byte, 8192), 4<<20)
	emit(fmt.Sprintf(uiText(c.Language, "测试：%s · %s / %s"), state.ID, model, c.Effort))
	for sc.Scan() {
		var ev struct {
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"`
			Item    struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		msg := strings.ReplaceAll(ev.Message, a.Token, "[REDACTED]")
		if len(ev.Error) > 0 {
			var detail struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			}
			if json.Unmarshal(ev.Error, &detail) == nil {
				msg += " " + detail.Code + " " + detail.Message
			} else {
				var text string
				if json.Unmarshal(ev.Error, &text) == nil {
					msg += " " + text
				}
			}
		}
		msg = strings.ReplaceAll(msg, a.Token, "[REDACTED]")
		if a.Account != "" {
			msg = strings.ReplaceAll(msg, a.Account, "[ACCOUNT]")
		}
		if e = enc.Encode(map[string]any{"at": stamp(), "elapsed_seconds": time.Since(started).Seconds(), "type": ev.Type, "item_type": ev.Item.Type, "message": msg}); e != nil {
			errorLog = e
		}
		if ev.Item.Type == "agent_message" && ev.Item.Text != "" {
			if first == "" {
				first = ev.Item.Text
				if c.KeywordFilter && keywordRejected(first) {
					filtered.Store(true)
					stopBackground(cmd)
					break
				}
			}
			if ev.Type == "item.completed" {
				text = ev.Item.Text
			}
		}
		if ev.Item.Text != "" && !streamed.Load() {
			display(ev.Item.Text + "\n")
		}
		if strings.TrimSpace(msg) != "" {
			emit("Codex: " + safeText(msg))
		}
	}
	scanErr = sc.Err()
	waitErr := cmd.Wait()
	stopWatcher()
	meta := map[string]any{"state_id": id, "model": model, "effort": c.Effort, "started": started.UTC().Format(time.RFC3339), "seconds": time.Since(started).Seconds(), "keyword_rejected": filtered.Load(), "cancelled": ctx.Err() != nil, "automatic_retries": "Codex controlled; emitted error/retry events recorded"}
	if e = s.Record(dir, "attempt.json", meta); e != nil {
		return e
	}
	if errorLog != nil {
		return errorLog
	}
	if filtered.Load() {
		removeState(c, id)
		emit(uiText(c.Language, "关键词过滤：已移除候选"))
		return s.Save(*c)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if runctx.Err() != nil {
		return runctx.Err()
	}
	if waitErr != nil || scanErr != nil {
		state.LastError = "Codex 运行失败/响应中断，保留候选可重试"
		_ = s.Save(*c)
		return errors.New(state.LastError)
	}
	html := extractHTML(text)
	if html == "" {
		removeState(c, id)
		emit(uiText(c.Language, "模型完成但无完整 HTML，已移除候选"))
		return s.Save(*c)
	}
	state.HTML = filepath.Join(dir, "pelican.html")
	if e = atomicWrite(state.HTML, []byte(html), 0600); e != nil {
		return e
	}
	state.PNG = filepath.Join(dir, "pelican.png")
	state.Status = "review"
	state.LastError = ""
	if e = s.Save(*c); e != nil {
		return e
	}
	if e = render(ctx, s, *c, state.HTML, state.PNG, emit); e != nil {
		state.LastError = "渲染失败，可恢复重试"
		_ = s.Save(*c)
		return e
	}
	return s.Save(*c)
}
