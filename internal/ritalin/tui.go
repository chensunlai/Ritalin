package ritalin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gofrs/flock"
)

var titles = []string{"HTTP / SOCKS", "Clash 节点", "Compact 探测", "鹈鹕测试", "可用状态", "设置"}

type tickMsg time.Time
type logMsg string
type jobDone struct {
	config   Config
	err      error
	kind, id string
	ids      []string
}
type entry struct{ label, action, id string }
type ui struct {
	store                            *Store
	c                                Config
	tab, cursor, width, height       int
	input                            textarea.Model
	viewport                         viewport.Model
	form, formTitle, confirm         string
	notice, log, modelOutput, review string
	busy                             bool
	cancel                           context.CancelFunc
	events                           chan tea.Msg
	started                          time.Time
	lastJob                          string
	batch                            bool
	quitAfter                        bool
	probeIDs                         []string
	confirmFn                        func()
	frame                            int
}

func runTUI(s *Store, c Config) error {
	if e := os.MkdirAll(s.Root, 0700); e != nil {
		return e
	}
	lock := flock.New(filepath.Join(s.Root, "dosing.lock"))
	ok, e := lock.TryLock()
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("另一个 dosing 界面正在使用此配置")
	}
	defer lock.Unlock()
	input := textarea.New()
	input.SetHeight(5)
	input.CharLimit = 1024 * 1024
	input.ShowLineNumbers = false
	m := &ui{store: s, c: c, width: 100, height: 32, input: input, viewport: viewport.New(96, 10), notice: "各功能区独立使用。导入/探测不跟随系统代理；鹈鹕和下载使用系统代理。"}
	_, e = tea.NewProgram(m, tea.WithAltScreen()).Run()
	if m.cancel != nil {
		m.cancel()
	}
	return e
}
func (m *ui) Init() tea.Cmd { return tick() }
func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}
func (m *ui) save() {
	if e := m.store.Save(m.c); e != nil {
		m.notice = "保存失败：" + e.Error()
	} else {
		m.notice = "已保存"
	}
}
func (m *ui) openForm(key, title, value string) {
	m.form = key
	m.formTitle = title
	m.input.SetValue(value)
	m.input.Focus()
}
func (m *ui) ask(text string, fn func()) { m.confirm = text; m.confirmFn = fn }
func (m *ui) entries() []entry {
	out := []entry{}
	switch m.tab {
	case 0, 1:
		kind := "proxy"
		if m.tab == 1 {
			kind = "clash"
		}
		out = append(out, entry{"＋ 导入并探测 models", "import", kind}, entry{"清空此类节点…", "clear", kind})
		for _, n := range m.c.Nodes {
			if n.Kind == kind {
				out = append(out, entry{safeText(n.Name), "node", n.ID})
			}
		}
	case 2:
		out = []entry{{"对所有已保存节点执行 compact（最多并发 2）", "probe", ""}, {"指定探测 CODEX_HOME（空白使用默认）", "home", ""}, {"设置探测模型（空白读取 config.toml）", "model", ""}}
	case 3:
		out = []entry{{"开始 / 继续逐个测试未确认状态", "batch", ""}, {fmt.Sprintf("首句关键词快速过滤：%t（实验规则）", m.c.KeywordFilter), "keyword", ""}}
		for _, s := range m.c.States {
			if s.Status != "usable" {
				out = append(out, entry{fmt.Sprintf("%s  %s  %d 字符 / %d 块 · %s", s.ID, s.Status, s.Metrics.Characters, s.Metrics.Blocks, safeText(s.Node)), "trial", s.ID})
			}
		}
	case 4:
		out = []entry{{"＋ 直接添加可用 turn-state（手动，未经实验确认）", "manual", ""}, {"停用当前状态，直接运行 Codex", "deactivate", ""}, {fmt.Sprintf("双向替换：%t（关闭用于对照）", m.c.Replace), "replace", ""}}
		for _, s := range m.c.States {
			if s.Status == "usable" {
				mark := "  "
				if m.c.Active == s.ID {
					mark = "● "
				}
				out = append(out, entry{fmt.Sprintf("%s%s  %d 字符 / %d 块 · %s", mark, s.ID, s.Metrics.Characters, s.Metrics.Blocks, safeText(s.Node)), "select", s.ID})
			}
		}
	case 5:
		cmd, _ := json.Marshal(m.c.Command)
		out = []entry{{"Codex 命令（JSON argv）：" + string(cmd), "command", ""}, {"探测 HOME：" + probeHome(m.store, m.c), "home", ""}, {"探测模型：" + m.c.Model, "model", ""}, {"鹈鹕推理强度：" + m.c.Effort, "effort", ""}, {"浏览器路径（空白自动）：" + m.c.Browser, "browser", ""}, {"Mihomo 路径（空白自动）：" + m.c.Mihomo, "mihomo", ""}, {"日常 warp 上游（空白=系统代理）：" + m.c.Upstream, "upstream", ""}}
		out = append(out, entry{fmt.Sprintf("无沙箱渲染：%t（安全风险，默认关闭）", m.c.BrowserNoSandbox), "browser-sandbox", ""})
	}
	return out
}
func (m *ui) wait() tea.Cmd { ch := m.events; return func() tea.Msg { return <-ch } }
func (m *ui) start(kind, id string, fn func(context.Context, *Config, Emit) ([]string, error)) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.busy = true
	m.started = time.Now()
	m.lastJob = kind
	m.log = ""
	m.modelOutput = ""
	m.review = ""
	m.events = make(chan tea.Msg, 256)
	c := clone(m.c)
	ch := m.events
	go func() {
		ids, e := fn(ctx, &c, func(s string) {
			select {
			case ch <- logMsg(s):
			case <-ctx.Done():
			}
		})
		ch <- jobDone{c, e, kind, id, ids}
	}()
	return m.wait()
}
func (m *ui) nextTrial() tea.Cmd {
	for _, s := range m.c.States {
		if s.Status != "usable" {
			id := s.ID
			return m.start("pelican", id, func(ctx context.Context, c *Config, emit Emit) ([]string, error) {
				return nil, pelican(ctx, m.store, c, id, emit)
			})
		}
	}
	m.batch = false
	m.notice = "没有未确认状态；所有进度已保存"
	return nil
}
func (m *ui) applyForm() tea.Cmd {
	key, value := m.form, strings.TrimSpace(m.input.Value())
	m.form = ""
	m.input.Blur()
	switch key {
	case "proxy", "clash":
		return m.start("import", "", func(ctx context.Context, c *Config, emit Emit) ([]string, error) {
			return nil, importNodes(ctx, m.store, c, key, value, emit)
		})
	case "command":
		var a []string
		if e := json.Unmarshal([]byte(value), &a); e != nil || len(a) == 0 || a[0] == "" {
			m.notice = `格式示例：["codex"] 或 ["node","/path/to/codex.js"]`
			return nil
		}
		test := m.c
		test.Command = a
		if _, e := codexCommand(test, nil); e != nil {
			m.notice = e.Error()
			return nil
		}
		m.c.Command = a
	case "home":
		m.c.ProbeHome = value
	case "model":
		m.c.Model = value
	case "effort":
		switch value {
		case "low", "medium", "high", "xhigh", "max":
			m.c.Effort = value
		default:
			m.notice = "请输入 low / medium / high / xhigh / max"
			return nil
		}
	case "browser":
		m.c.Browser = value
	case "mihomo":
		m.c.Mihomo = value
	case "upstream":
		if value != "" {
			if _, e := parseProxy(value); e != nil {
				m.notice = e.Error()
				return nil
			}
		}
		m.c.Upstream = value
	case "manual":
		value = strings.ReplaceAll(value, `\_`, "_")
		metrics, e := parseState(value)
		if e != nil {
			m.notice = e.Error()
			return nil
		}
		for _, s := range m.c.States {
			if s.Value == value {
				m.notice = "此状态已存在：" + s.ID
				return nil
			}
		}
		m.c.States = append(m.c.States, State{ID: newID(), Value: value, Node: "手动添加（未经测试）", Created: stamp(), Status: "usable", Metrics: metrics, AuthHome: probeHome(m.store, m.c)})
		m.save()
		m.notice = "已添加可用列表；选择该项并按 Enter 才会启用"
		return nil
	}
	m.save()
	return nil
}
func (m *ui) activate(e entry) tea.Cmd {
	switch e.action {
	case "browser-sandbox":
		if m.c.BrowserNoSandbox {
			m.c.BrowserNoSandbox = false
			m.save()
		} else {
			m.ask("确认关闭 Chromium 沙箱？仅用于无法启用沙箱的隔离环境；JS/外部资源仍阻止。", func() { m.c.BrowserNoSandbox = true; m.save() })
		}
	case "import":
		title := "粘贴代理列表（每行一个）或本地文件路径"
		if e.id == "clash" {
			title = "Clash YAML 文件路径或订阅 URL（只提取 proxies）"
		}
		m.openForm(e.id, title, "")
	case "clear":
		kind := e.id
		m.ask("确认清空此类节点？已采集状态和实验文件保留。", func() {
			out := []Node{}
			for _, n := range m.c.Nodes {
				if n.Kind != kind {
					out = append(out, n)
				}
			}
			m.c.Nodes = out
			m.cursor = 0
			m.save()
		})
	case "node":
		id := e.id
		m.ask("删除此节点？对应实验和 turn-state 保留。", func() {
			out := []Node{}
			for _, n := range m.c.Nodes {
				if n.ID != id {
					out = append(out, n)
				}
			}
			m.c.Nodes = out
			m.cursor = 0
			m.save()
		})
	case "probe":
		return m.start("compact", "", func(ctx context.Context, c *Config, emit Emit) ([]string, error) {
			return probeAll(ctx, m.store, c, emit)
		})
	case "home":
		m.openForm("home", "探测凭证 CODEX_HOME（不会复制凭证；空白默认）", m.c.ProbeHome)
	case "model":
		m.openForm("model", "探测模型（空白读取指定 HOME/config.toml 的 model）", m.c.Model)
	case "keyword":
		m.c.KeywordFilter = !m.c.KeywordFilter
		m.save()
	case "replace":
		m.c.Replace = !m.c.Replace
		m.save()
	case "batch":
		m.batch = true
		return m.nextTrial()
	case "trial":
		id := e.id
		m.batch = false
		return m.start("pelican", id, func(ctx context.Context, c *Config, emit Emit) ([]string, error) {
			return nil, pelican(ctx, m.store, c, id, emit)
		})
	case "manual":
		m.openForm("manual", "粘贴完整 turn-state；仅校验布局，不能验证真实性或质量", "")
	case "select":
		m.c.Active = e.id
		m.save()
		m.notice = "当前状态已选择；下次启动 codex-ritalin 生效"
	case "deactivate":
		m.c.Active = ""
		m.save()
	case "command":
		v, _ := json.Marshal(m.c.Command)
		m.openForm("command", "Codex 启动命令 JSON 数组（参数不经过 shell 重新解释）", string(v))
	case "effort":
		m.openForm("effort", "鹈鹕推理强度", m.c.Effort)
	case "browser":
		m.openForm("browser", "浏览器可执行文件路径，空白自动查找/下载", m.c.Browser)
	case "mihomo":
		m.openForm("mihomo", "Mihomo 可执行文件路径，空白自动查找/下载", m.c.Mihomo)
	case "upstream":
		m.openForm("upstream", "日常 warp 上游，空白使用系统代理；不影响节点探测或鹈鹕", m.c.Upstream)
	}
	return nil
}
func (m *ui) filter(team bool) {
	kept := 0
	for _, id := range m.probeIDs {
		state := findState(&m.c, id)
		if state != nil {
			if lengthMatch(state.Metrics, team) {
				kept++
			} else {
				removeState(&m.c, id)
			}
		}
	}
	m.save()
	m.notice = fmt.Sprintf("按实验规则保留 %d 个候选；仍需鹈鹕人工确认", kept)
	m.probeIDs = nil
}
func (m *ui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.input.SetWidth(max(20, v.Width-8))
		m.viewport.Width = max(20, v.Width-6)
		m.viewport.Height = max(4, v.Height-13)
	case tickMsg:
		m.frame++
		return m, tick()
	case logMsg:
		s := string(v)
		if strings.HasPrefix(s, "\x00output:") {
			m.modelOutput += strings.Map(func(r rune) rune {
				if r < 32 && r != '\n' && r != '\t' {
					return -1
				}
				return r
			}, strings.TrimPrefix(s, "\x00output:"))
			if len(m.modelOutput) > 256000 {
				m.modelOutput = m.modelOutput[len(m.modelOutput)-256000:]
			}
		} else {
			m.log += safeText(s) + "\n"
			if len(m.log) > 16000 {
				m.log = m.log[len(m.log)-16000:]
			}
		}
		m.viewport.SetContent(m.log + "\n" + m.modelOutput)
		m.viewport.GotoBottom()
		return m, m.wait()
	case jobDone:
		m.c = v.config
		m.busy = false
		m.cancel = nil
		m.notice = "完成"
		if v.err != nil {
			m.notice = v.err.Error()
			m.batch = false
		}
		if m.quitAfter {
			return m, tea.Quit
		}
		if v.kind == "compact" && len(v.ids) > 0 {
			m.probeIDs = v.ids
			m.confirm = "length-filter"
		}
		if v.kind == "pelican" && v.err == nil {
			state := findState(&m.c, v.id)
			if state != nil && state.Status == "review" {
				m.review = v.id
			} else if m.batch {
				return m, m.nextTrial()
			}
		}
	case tea.KeyMsg:
		key := v.String()
		if m.busy {
			if key == "esc" || key == "ctrl+c" {
				m.cancel()
				m.notice = "正在取消并保存进度…"
				if key == "ctrl+c" {
					m.quitAfter = true
				}
			} else {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		if m.form != "" {
			if key == "esc" {
				m.form = ""
				m.input.Blur()
				return m, nil
			}
			if key == "ctrl+s" {
				return m, m.applyForm()
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if m.confirm != "" {
			if m.confirm == "length-filter" {
				if key == "y" {
					m.confirm = "account-kind"
				} else if key == "n" || key == "esc" {
					m.confirm = ""
					m.probeIDs = nil
					m.notice = "未过滤；候选已保存为未确认"
				}
				return m, nil
			}
			if m.confirm == "account-kind" {
				if key == "1" || key == "2" {
					m.filter(key == "2")
					m.confirm = ""
				} else if key == "esc" {
					m.confirm = ""
				}
				return m, nil
			}
			if key == "y" {
				fn := m.confirmFn
				m.confirm = ""
				if fn != nil {
					fn()
				}
			} else if key == "n" || key == "esc" {
				m.confirm = ""
			}
			return m, nil
		}
		if m.review != "" {
			if key == "g" || key == "b" {
				if key == "g" {
					if s := findState(&m.c, m.review); s != nil {
						s.Status = "usable"
					}
				} else {
					removeState(&m.c, m.review)
				}
				m.save()
				m.review = ""
				if m.batch {
					return m, m.nextTrial()
				}
			}
			if key == "esc" || key == "s" {
				m.review = ""
				m.batch = false
				m.notice = "保留待确认结果，下次可继续"
			}
			return m, nil
		}
		entries := m.entries()
		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab", "right":
			m.tab = (m.tab + 1) % len(titles)
			m.cursor = 0
		case "shift+tab", "left":
			m.tab = (m.tab + len(titles) - 1) % len(titles)
			m.cursor = 0
		case "1", "2", "3", "4", "5", "6":
			m.tab = int(key[0] - '1')
			m.cursor = 0
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(len(entries)-1, m.cursor+1)
		case "enter":
			if m.cursor >= 0 && m.cursor < len(entries) {
				return m, m.activate(entries[m.cursor])
			}
		case "d":
			if m.cursor >= 0 && m.cursor < len(entries) {
				item := entries[m.cursor]
				if item.action == "select" || item.action == "trial" {
					id := item.id
					m.ask("确认删除此 turn-state？HTML/截图和实验记录保留。", func() { removeState(&m.c, id); m.cursor = 0; m.save() })
				}
			}
		}
	}
	return m, nil
}
func (m *ui) View() string {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	var b strings.Builder
	b.WriteString(accent.Render("Ritalin · 利他林") + "  dosing\n")
	nav := []string{}
	for i, t := range titles {
		s := fmt.Sprintf("%d %s", i+1, t)
		if i == m.tab {
			s = accent.Render("[" + s + "]")
		}
		nav = append(nav, s)
	}
	b.WriteString(strings.Join(nav, "  ") + "\n")
	pending, usable := 0, 0
	for _, s := range m.c.States {
		if s.Status == "usable" {
			usable++
		} else {
			pending++
		}
	}
	b.WriteString(fmt.Sprintf("节点 %d  ·  待确认 %d  ·  可用 %d  ·  当前 %s  ·  替换 %t\n", len(m.c.Nodes), pending, usable, m.c.Active, m.c.Replace))
	b.WriteString(strings.Repeat("─", max(10, min(m.width-2, 100))) + "\n")
	if m.form != "" {
		b.WriteString(m.formTitle + "\n\n" + m.input.View() + "\n\nCtrl+S 保存/开始 · Esc 返回\n")
		return b.String()
	}
	if m.confirm != "" {
		text := m.confirm
		if text == "length-filter" {
			text = "是否按密文长度快速过滤本轮候选？[y/n]\n这是用户提供的实验启发式，不证明模型质量。"
		} else if text == "account-kind" {
			text = "选择账户类型：\n[1] 个人 Free/Plus/Pro：292 字符、10 块\n[2] Team/Business：332 字符、12 块\n不匹配的本轮候选将移除。Esc 不过滤。"
		} else {
			text += " [y/n]"
		}
		b.WriteString(text + "\n")
		return b.String()
	}
	if m.busy {
		elapsed := time.Since(m.started).Round(time.Second)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		b.WriteString(fmt.Sprintf("%s %s · 已等待 %s · Esc 取消并保留进度\n", frames[m.frame%len(frames)], m.lastJob, elapsed))
		if elapsed >= 5*time.Second {
			pulse := m.frame % 20
			b.WriteString("[" + strings.Repeat("░", pulse) + "█" + strings.Repeat("░", 19-pulse) + "]  等待/处理中；下载开始后显示字节进度。\n")
		}
		b.WriteString(m.viewport.View())
		return b.String()
	}
	if m.review != "" {
		if s := findState(&m.c, m.review); s != nil {
			b.WriteString("请打开文件检查动画与截图：\nHTML: " + s.HTML + "\nPNG:  " + s.PNG + "\n\n[g] 可用  [b] 降智/删除  [s/Esc] 稍后确认\n")
			b.WriteString(m.viewport.View())
		}
		return b.String()
	}
	entries := m.entries()
	height := max(3, m.height-12)
	start := max(0, m.cursor-height+1)
	for i := start; i < len(entries) && i < start+height; i++ {
		label := entries[i].label
		if i == m.cursor {
			b.WriteString(accent.Render("› "+label) + "\n")
		} else {
			b.WriteString("  " + label + "\n")
		}
	}
	b.WriteString("\n" + safeText(m.notice) + "\n")
	b.WriteString("数据：" + m.store.Root + "\nTab/1–6 切区 · ↑↓ 选择 · Enter 操作 · d 删除状态 · q 退出\n")
	return b.String()
}
