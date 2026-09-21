package ritalin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const pelicanPrompt = `创建一个HTML，内容是SVG绘制一个鹈鹕骑自行车的2D动画。`

// Only collect files produced in this attempt, never HTML quoted in a reply or
// another trial's output. WalkDir does not follow symlinks.
func generatedHTML(dir string) (string, error) {
	var result string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".html" && ext != ".htm" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 0 && result == "" {
			result = path
		}
		return nil
	})
	return result, err
}

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
	configureBrowserDownload(b, runtime.GOOS, runtime.GOARCH)
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
			state.Status = "review"
			return s.Save(*c)
		}
	}
	// A candidate's provenance is informational; tests use the current login.
	home := probeHome(s, *c)
	a, e := readAuth(home)
	if e != nil {
		return e
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
	w, closeWarp, e := startTestWarp(runctx, s, *c, state, emit)
	if e != nil {
		return e
	}
	defer closeWarp()
	cmd, e := codexCommand(*c, []string{"app-server", "--listen", "stdio://", "-c", `cli_auth_credentials_store="file"`, "-c", `sandbox_mode="danger-full-access"`, "-c", `approval_policy="never"`})
	if e != nil {
		return e
	}
	cmd.Dir = filepath.Join(dir, "workspace")
	if e = os.MkdirAll(cmd.Dir, 0700); e != nil {
		return e
	}
	cmd.Env = setEnv(w.Env(os.Environ()), "CODEX_HOME", home)
	for _, key := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN"} {
		var env []string
		for _, v := range cmd.Env {
			if !strings.HasPrefix(v, key+"=") {
				env = append(env, v)
			}
		}
		cmd.Env = env
	}
	events, e := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer events.Close()
	enc := json.NewEncoder(events)
	started := time.Now()
	redact := func(v string) string {
		for _, secret := range []string{a.Token, a.Account, state.Value} {
			if secret != "" {
				v = strings.ReplaceAll(v, secret, "[REDACTED]")
			}
		}
		return v
	}
	outputBytes := 0
	display := func(v string) error {
		outputBytes += len(v)
		if outputBytes > 32<<20 {
			return errors.New("app-server output exceeds 32 MiB")
		}
		v = redact(v)
		if _, err := io.WriteString(output, v); err != nil {
			return err
		}
		emit("\x00output:" + v)
		return nil
	}
	texts := map[string]string{}
	toolOutput := map[string]string{}
	var firstID string
	filtered := false
	filterErr := errors.New("keyword filter")
	// All callbacks run on the app-server reader loop, in notification order.
	notify := func(method string, p appParams) error {
		message, info := p.Message, ""
		if p.Error != nil {
			message = p.Error.Message
			info = string(p.Error.Info)
		}
		if p.Turn.Error != nil {
			message = p.Turn.Error.Message
			info = string(p.Turn.Error.Info)
		}
		message = ansi.Strip(message)
		toolJSON, _ := json.Marshal(p.Item)
		if err := enc.Encode(map[string]any{
			"at": stamp(), "elapsed_seconds": time.Since(started).Seconds(), "type": method,
			"thread_id": p.ThreadID, "turn_id": p.TurnID, "item_type": p.Item.Type,
			"message": redact(message), "error_info": redact(info), "will_retry": p.WillRetry,
			"turn_status": p.Turn.Status,
			"item_id":     p.ItemID, "item": json.RawMessage(redact(string(toolJSON))),
		}); err != nil {
			return err
		}
		if strings.TrimSpace(message) != "" {
			emit("Codex: " + safeText(redact(message)))
		}
		switch method {
		case "initialized":
			emit(uiText(c.Language, "Codex 已连接"))
		case "thread/started":
			emit(uiText(c.Language, "会话已创建"))
		case "turn/started":
			emit(uiText(c.Language, "正在生成…"))
		case "item/started":
			return displayTool(p.Item, false, toolOutput, display)
		case "item/agentMessage/delta", "item/completed":
			id := p.ItemID
			if method == "item/completed" {
				if p.Item.Type != "agentMessage" {
					return displayTool(p.Item, true, toolOutput, display)
				}
				id = p.Item.ID
			}
			if id == "" {
				id = "message"
			}
			previous, exists := texts[id]
			if !exists {
				if firstID != "" {
					if err := display("\n\n"); err != nil {
						return err
					}
				}
				if firstID == "" {
					firstID = id
				}
			}
			chunk := p.Delta
			if method == "item/completed" {
				texts[id] = p.Item.Text
				// The completed item is authoritative, but must not duplicate deltas.
				chunk = ""
				if strings.HasPrefix(p.Item.Text, previous) {
					chunk = strings.TrimPrefix(p.Item.Text, previous)
				}
			} else {
				texts[id] = previous + chunk
			}
			if len(texts[id]) > 32<<20 {
				return errors.New("app-server output exceeds 32 MiB")
			}
			if chunk != "" {
				if err := display(chunk); err != nil {
					return err
				}
			}
			if id == firstID && c.KeywordFilter && keywordRejected(texts[id]) {
				filtered = true
				return filterErr
			}
		case "item/commandExecution/outputDelta":
			toolOutput[p.ItemID] += p.Delta
			return display(p.Delta)
		case "item/reasoning/summaryTextDelta", "item/plan/delta", "item/fileChange/outputDelta":
			return display(p.Delta)
		}
		return nil
	}
	emit(fmt.Sprintf(uiText(c.Language, "测试：%s · %s / %s"), state.Node, model, c.Effort))
	emit(uiText(c.Language, "正在启动 Codex…"))
	runErr := appServerTurn(runctx, cmd, model, c.Effort, pelicanPrompt, notify)
	if runErr != nil && !filtered {
		if e = enc.Encode(map[string]any{"at": stamp(), "type": "client/error", "message": redact(runErr.Error())}); e != nil {
			return e
		}
	}
	meta := map[string]any{"state_id": id, "model": model, "effort": c.Effort, "transport": "app-server/stdio",
		"prompt": pelicanPrompt, "cwd": cmd.Dir, "sandbox": "danger-full-access", "approval_policy": "never",
		"started": started.UTC().Format(time.RFC3339), "seconds": time.Since(started).Seconds(),
		"keyword_rejected": filtered, "cancelled": ctx.Err() != nil, "completed": runErr == nil,
		"automatic_retries": "Codex controlled; emitted error/retry events recorded"}
	if e = s.Record(dir, "attempt.json", meta); e != nil {
		return e
	}
	if filtered {
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
	if runErr != nil {
		state.LastError = redact(runErr.Error())
		_ = s.Save(*c)
		return errors.New(state.LastError)
	}
	html, e := generatedHTML(cmd.Dir)
	if e != nil {
		return e
	}
	if html == "" {
		removeState(c, id)
		emit(uiText(c.Language, "模型完成但无完整 HTML，已移除候选"))
		return s.Save(*c)
	}
	state.HTML = html
	state.PNG = ""
	state.Status = "review"
	state.LastError = ""
	return s.Save(*c)
}
