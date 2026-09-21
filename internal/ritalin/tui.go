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
	"github.com/gofrs/flock"
)

const (
	tabProxies = iota
	tabProbe
	tabTest
	tabUse
	tabSettings
)

var titles = []string{"代理", "探测", "测试", "使用", "设置"}

const (
	formInput = iota
	formSave
	formCancel
)

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
	bodyOffset                       int
	store                            *Store
	c                                Config
	tab, cursor, width, height       int
	input                            textarea.Model
	viewport                         viewport.Model
	logViewport                      viewport.Model
	languagePick, details, paused    bool
	warning                          bool
	languageCursor                   int
	advanced                         bool
	form, formTitle, confirm         string
	formFocus                        int
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
	preview                          *previewTask
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
	m := newUI(s, c)
	defer m.cancelPreview()
	_, e = tea.NewProgram(m, tea.WithAltScreen()).Run()
	if m.cancel != nil {
		m.cancel()
	}
	return e
}

func newUI(s *Store, c Config) *ui {
	input := textarea.New()
	input.SetHeight(5)
	input.CharLimit = 1024 * 1024
	input.ShowLineNumbers = false
	m := &ui{store: s, c: c, width: 100, height: 32, input: input, viewport: viewport.New(60, 6), logViewport: viewport.New(32, 6), warning: !c.RiskAcknowledged, languagePick: c.Language != "zh" && c.Language != "en"}
	if m.usableCount() > 0 {
		m.tab = tabUse
	} else if m.pendingCount() > 0 {
		m.tab = tabTest
	} else if len(c.Nodes) > 0 {
		m.tab = tabProbe
	}
	if c.Language == "en" {
		m.languageCursor = 1
	}
	m.resize()
	return m
}
func (m *ui) Init() tea.Cmd { return tick() }
func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}
func (m *ui) save() bool {
	if e := m.store.Save(m.c); e != nil {
		m.notice = m.t("保存失败：") + e.Error()
		return false
	}
	m.notice = m.t("已保存")
	return true
}
func (m *ui) openForm(key, title, value string) tea.Cmd {
	m.form = key
	m.formTitle = m.t(title)
	m.formFocus = formInput
	m.notice = ""
	m.input.SetValue(value)
	m.bodyOffset = 0
	cmd := m.input.Focus()
	m.resize()
	return cmd
}
func (m *ui) cancelForm() {
	m.form = ""
	m.formFocus = formInput
	m.input.Blur()
	m.notice = m.t("已取消")
}

func (m *ui) focusForm(focus int) tea.Cmd {
	m.formFocus = (focus + 3) % 3
	if m.formFocus == formInput {
		return m.input.Focus()
	}
	m.input.Blur()
	return nil
}

func (m *ui) ask(text string, fn func()) { m.confirm = m.t(text); m.confirmFn = fn }
func (m *ui) entries() []entry {
	out := []entry{}
	switch m.tab {
	case tabProxies:
		out = []entry{{"＋ HTTP / SOCKS", "import", "proxy"}, {"＋ Clash", "import", "clash"}}
		if len(m.c.Nodes) > 0 {
			out = append(out, entry{"下一步：探测 →", "next", "probe"})
			out = append(out, entry{"清空全部节点…", "clear", "all"})
		}
		for _, n := range m.c.Nodes {
			out = append(out, entry{safeText(n.Name), "node", n.ID})
		}
		for _, kind := range []string{"proxy", "clash"} {
			for _, n := range m.c.Nodes {
				if n.Kind == kind {
					label := "清空 HTTP / SOCKS…"
					if kind == "clash" {
						label = "清空 Clash…"
					}
					out = append(out, entry{label, "clear", kind})
					break
				}
			}
		}
	case tabProbe:
		out = []entry{{"开始探测", "probe", ""}, {"凭证目录", "home", ""}, {"探测模型", "model", ""}}
		if len(m.c.Nodes) == 0 {
			out[0] = entry{"先添加代理 →", "next", "proxies"}
		}
		if m.pendingCount() > 0 {
			out = append(out, entry{"下一步：测试 →", "next", "test"})
		}
	case tabTest:
		if m.pendingCount() > 0 {
			out = append(out, entry{"开始 / 继续测试", "batch", ""})
		} else {
			out = append(out, entry{"先探测状态 →", "next", "probe"})
		}
		if m.usableCount() > 0 {
			out = append(out, entry{"下一步：使用 →", "next", "use"})
		}
		out = append(out, entry{"导入…", "import-states", "pending"}, entry{"导出…", "export-states", "pending"})
		for _, s := range m.c.States {
			if s.Status != "usable" {
				out = append(out, entry{m.stateLabel(s) + "  · " + m.stateStatus(s.Status), "trial", s.ID})
			}
		}
		if m.pendingCount() > 0 {
			out = append(out, entry{"清空待确认状态…", "clear-states", "pending"})
		}
	case tabUse:
		out = []entry{{"＋ 添加状态", "manual", ""}, {"导入…", "import-states", "usable"}, {"导出…", "export-states", "usable"}}
		for _, s := range m.c.States {
			if s.Status == "usable" {
				mark := "  "
				if m.c.Active == s.ID && m.c.Replace {
					mark = "● "
				}
				out = append(out, entry{mark + m.stateLabel(s), "select", s.ID})
			}
		}
		if m.usableCount() > 0 {
			out = append(out, entry{"清空可用状态…", "clear-states", "usable"})
		}
	case tabSettings:
		out = []entry{{"启动命令", "command", ""}, {"推理强度", "effort", ""}, {"界面语言", "language", ""}, {"高级设置 →", "advanced", ""}}
		if m.advanced {
			out = []entry{{"← 返回设置", "advanced", ""}, {"截图引擎", "browser", ""}, {"Mihomo 路径", "mihomo", ""}, {"上游代理", "upstream", ""}, {"关键词过滤", "keyword", ""}, {"无沙箱渲染", "browser-sandbox", ""}}
		}
	}
	return out
}
func (m *ui) wait() tea.Cmd { ch := m.events; return func() tea.Msg { return <-ch } }
func (m *ui) start(kind, id string, fn func(context.Context, *Config, Emit) ([]string, error)) tea.Cmd {
	if (kind == "probe" || kind == "pelican") && m.c.Active != "" {
		active := m.c.Active
		m.c.Active = ""
		if !m.save() {
			m.c.Active = active
			m.batch = false
			return nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.busy = true
	m.started = time.Now()
	m.lastJob = kind
	m.log = ""
	m.modelOutput = ""
	m.paused = false
	m.refreshOutput()
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
			if s.Status == "review" && s.HTML != "" {
				m.review = id
				return nil
			}
			return m.start("pelican", id, func(ctx context.Context, c *Config, emit Emit) ([]string, error) {
				return nil, pelican(ctx, m.store, c, id, emit)
			})
		}
	}
	m.batch = false
	m.notice = m.t("没有未确认状态；所有进度已保存")
	return nil
}
func (m *ui) applyForm() tea.Cmd {
	key, value := m.form, strings.TrimSpace(m.input.Value())
	m.form = ""
	m.input.Blur()
	reject := func(message string) tea.Cmd {
		m.notice = message
		m.form = key
		return m.focusForm(formInput)
	}
	switch key {
	case "import-pending", "import-usable":
		added, skipped, err := importStates(m.store, &m.c, value, key == "import-usable")
		if err != nil {
			return reject(err.Error())
		}
		m.notice = fmt.Sprintf(m.t("已导入 %d 个状态，跳过 %d 个重复状态"), added, skipped)
		return nil
	case "export-pending", "export-usable":
		count, err := exportStates(m.c, value, key == "export-usable")
		if err != nil {
			return reject(err.Error())
		}
		m.notice = fmt.Sprintf(m.t("已导出 %d 个状态：%s"), count, ExpandPath(value))
		return nil
	case "proxy", "clash":
		return m.start("import", "", func(ctx context.Context, c *Config, emit Emit) ([]string, error) {
			return nil, importNodes(ctx, m.store, c, key, value, emit)
		})
	case "command":
		var a []string
		if e := json.Unmarshal([]byte(value), &a); e != nil || len(a) == 0 || a[0] == "" {
			return reject(m.t(`格式示例：["codex"] 或 ["node","/path/to/codex.js"]`))
		}
		test := m.c
		test.Command = a
		if _, e := codexCommand(test, nil); e != nil {
			return reject(e.Error())
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
			return reject(m.t("请输入 low / medium / high / xhigh / max"))
		}
	case "browser":
		m.c.Browser = value
	case "mihomo":
		m.c.Mihomo = value
	case "upstream":
		if value != "" {
			if _, e := parseProxy(value); e != nil {
				return reject(e.Error())
			}
		}
		m.c.Upstream = value
	case "manual":
		value = strings.ReplaceAll(value, `\_`, "_")
		metrics, e := parseState(value)
		if e != nil {
			return reject(e.Error())
		}
		for _, s := range m.c.States {
			if s.Value == value {
				return reject(m.t("此状态已存在：") + s.ID)
			}
		}
		m.c.States = append(m.c.States, State{ID: newID(), Value: value, Node: m.t("手动添加"), Created: stamp(), Status: "usable", Metrics: metrics, AuthHome: probeHome(m.store, m.c)})
		if !m.save() {
			m.c.States = m.c.States[:len(m.c.States)-1]
			return reject(m.notice)
		}
		for i, item := range m.entries() {
			if item.action == "select" && item.id == m.c.States[len(m.c.States)-1].ID {
				m.cursor = i
				break
			}
		}
		m.notice = m.t("已添加可用列表；选择该项并按 Enter 才会启用")
		return nil
	}
	m.save()
	return nil
}
func (m *ui) activate(e entry) tea.Cmd {
	m.bodyOffset = 0
	switch e.action {
	case "import-states":
		return m.openForm("import-"+e.id, "导入状态 JSON 文件路径", "")
	case "export-states":
		path := filepath.Join(m.store.Root, "exports", e.id+"-"+time.Now().Format("20060102-150405")+"-"+newID()[:6]+".json")
		return m.openForm("export-"+e.id, "导出路径（文件包含完整状态）", path)
	case "next":
		m.tab = map[string]int{"proxies": tabProxies, "probe": tabProbe, "test": tabTest, "use": tabUse}[e.id]
		m.cursor = 0
		m.details = false
	case "advanced":
		m.advanced = !m.advanced
		m.cursor = 0
	case "language":
		m.chooseLanguage()
	case "browser-sandbox":
		if m.c.BrowserNoSandbox {
			m.c.BrowserNoSandbox = false
			m.save()
		} else {
			m.ask("关闭渲染沙箱会降低隔离保护。仍要继续吗？", func() { m.c.BrowserNoSandbox = true; m.save() })
		}
	case "import":
		title := "粘贴代理列表（每行一个）或本地文件路径"
		if e.id == "clash" {
			title = "Clash YAML 文件路径或订阅 URL"
		}
		return m.openForm(e.id, title, "")
	case "clear":
		kind := e.id
		m.ask("确认清空此类节点？已采集状态和实验文件保留。", func() {
			out := []Node{}
			for _, n := range m.c.Nodes {
				if kind != "all" && n.Kind != kind {
					out = append(out, n)
				}
			}
			m.c.Nodes = out
			m.cursor = 0
			m.save()
		})
	case "clear-states":
		usable := e.id == "usable"
		m.ask("确认清空此列表？HTML、图片和实验记录保留。", func() {
			var ids []string
			for _, s := range m.c.States {
				if (s.Status == "usable") == usable {
					ids = append(ids, s.ID)
				}
			}
			for _, id := range ids {
				removeState(&m.c, id)
			}
			m.cursor = 0
			m.save()
		})
	case "delete-node":
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
		if len(m.c.Nodes) == 0 {
			m.notice = m.t("导入节点后即可开始探测")
			return nil
		}
		return m.start("probe", "", func(ctx context.Context, c *Config, emit Emit) ([]string, error) {
			return probeAll(ctx, m.store, c, emit)
		})
	case "home":
		return m.openForm("home", "凭证目录 CODEX_HOME（留空使用默认目录）", m.c.ProbeHome)
	case "model":
		return m.openForm("model", "探测模型（留空使用 Codex 配置）", m.c.Model)
	case "keyword":
		m.c.KeywordFilter = !m.c.KeywordFilter
		m.save()
	case "batch":
		m.batch = true
		return m.nextTrial()
	case "trial":
		id := e.id
		m.batch = false
		if s := findState(&m.c, id); s != nil && s.Status == "review" && s.HTML != "" {
			m.review = id
			return nil
		}
		return m.start("pelican", id, func(ctx context.Context, c *Config, emit Emit) ([]string, error) {
			return nil, pelican(ctx, m.store, c, id, emit)
		})
	case "manual":
		return m.openForm("manual", "粘贴 x-codex-turn-state", "")
	case "select":
		if m.c.Active == e.id && m.c.Replace {
			m.c.Active = ""
		} else {
			m.c.Active = e.id
			m.c.Replace = true
		}
		m.save()
	case "command":
		v, _ := json.Marshal(m.c.Command)
		return m.openForm("command", "Codex 启动命令（JSON 数组）", string(v))
	case "effort":
		return m.openForm("effort", "鹈鹕推理强度", m.c.Effort)
	case "browser":
		return m.openForm("browser", "浏览器可执行文件路径，空白自动查找/下载", m.c.Browser)
	case "mihomo":
		return m.openForm("mihomo", "Mihomo 可执行文件路径，空白自动查找/下载", m.c.Mihomo)
	case "upstream":
		return m.openForm("upstream", "上游代理 URL（留空自动）", m.c.Upstream)
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
	m.notice = fmt.Sprintf(m.t("已保留 %d 个候选，可继续测试"), kept)
	m.probeIDs = nil
}
func (m *ui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.resize()
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
			for _, line := range strings.Split(s, "\n") {
				m.log += safeText(line) + "\n"
			}
			if len(m.log) > 16000 {
				m.log = m.log[len(m.log)-16000:]
			}
		}
		m.refreshOutput()
		return m, m.wait()
	case jobDone:
		m.c = v.config
		m.busy = false
		m.cancel = nil
		m.notice = m.t("完成")
		if v.err != nil {
			m.notice = v.err.Error()
			if errors.Is(v.err, context.Canceled) {
				m.notice = m.t("已取消")
			}
			m.log += safeText(m.notice) + "\n"
			m.refreshOutput()
			m.batch = false
		}
		if m.quitAfter {
			return m, tea.Quit
		}
		if v.kind == "probe" && len(v.ids) > 0 {
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
	case previewDone:
		m.finishPreview(v)
	case tea.KeyMsg:
		key := v.String()
		if m.warning {
			switch key {
			case "q", "esc", "ctrl+c":
				return m, tea.Quit
			case "enter":
				m.c.RiskAcknowledged = true
				if !m.save() {
					m.c.RiskAcknowledged = false
					return m, nil
				}
				m.warning = false
				m.notice = ""
			}
			return m, nil
		}
		if m.languagePick {
			switch key {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "up", "down", "left", "right", "tab", "shift+tab":
				m.languageCursor = 1 - m.languageCursor
			case "1", "2", "enter":
				if key != "enter" {
					m.languageCursor = int(key[0] - '1')
				}
				m.c.Language = []string{"zh", "en"}[m.languageCursor]
				m.languagePick = false
				m.save()
			case "esc":
				m.languagePick = false
			}
			return m, nil
		}
		if m.form == "" && m.confirm == "" && (key == "pgup" || key == "pgdown" || key == "end") {
			if key == "end" {
				m.paused = false
				m.viewport.GotoBottom()
				m.logViewport.GotoBottom()
			} else {
				m.paused = true
				m.viewport, _ = m.viewport.Update(msg)
				m.logViewport, _ = m.logViewport.Update(msg)
			}
			return m, nil
		}
		if m.busy {
			if key == "esc" || key == "ctrl+c" {
				m.cancel()
				m.notice = m.t("正在取消并保存进度…")
				if key == "ctrl+c" {
					m.quitAfter = true
				}
			} else {
				var cmd tea.Cmd
				if key == "up" || key == "down" || key == "k" || key == "j" {
					m.paused = true
				}
				m.viewport, cmd = m.viewport.Update(msg)
				m.logViewport, _ = m.logViewport.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		if m.form != "" {
			switch key {
			case "esc":
				m.cancelForm()
				return m, nil
			case "tab":
				return m, m.focusForm(m.formFocus + 1)
			case "shift+tab":
				return m, m.focusForm(m.formFocus - 1)
			case "enter":
				if m.formFocus == formCancel {
					m.cancelForm()
					return m, nil
				}
				// Only the proxy list needs typed newlines. Bracketed paste
				// remains intact in every field, including multi-line JSON.
				if m.formFocus == formSave || m.form != "proxy" {
					return m, m.applyForm()
				}
			}
			if m.formFocus != formInput {
				return m, nil
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
					m.notice = m.t("未过滤；候选已保存为未确认")
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
			if key == "p" {
				return m, m.startPreview()
			}
			if key == "up" || key == "down" {
				if key == "up" {
					m.bodyOffset = max(0, m.bodyOffset-1)
				} else {
					m.bodyOffset++
				}
				return m, nil
			}
			if key == "g" || key == "b" {
				m.cancelPreview()
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
				m.cancelPreview()
				m.review = ""
				m.batch = false
				m.notice = m.t("保留待确认结果，下次可继续")
			}
			return m, nil
		}
		entries := m.entries()
		if m.details && m.layout().detail == 0 {
			switch key {
			case "up", "k":
				m.bodyOffset = max(0, m.bodyOffset-1)
				return m, nil
			case "down", "j":
				m.bodyOffset++
				return m, nil
			case "esc", " ":
				m.details = false
				m.bodyOffset = 0
				return m, nil
			}
		}
		switch key {
		case "l", "L":
			m.chooseLanguage()
		case " ":
			m.details = !m.details
			m.bodyOffset = 0
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab", "right":
			m.tab = (m.tab + 1) % len(titles)
			m.cursor = 0
			m.details = false
		case "shift+tab", "left":
			m.tab = (m.tab + len(titles) - 1) % len(titles)
			m.cursor = 0
			m.details = false
		case "1", "2", "3", "4", "5":
			m.tab = int(key[0] - '1')
			m.cursor = 0
			m.details = false
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(len(entries)-1, m.cursor+1)
		case "enter":
			if m.cursor >= 0 && m.cursor < len(entries) {
				if entries[m.cursor].action == "node" {
					m.details = !m.details
					return m, nil
				}
				return m, m.activate(entries[m.cursor])
			}
		case "d":
			if m.cursor >= 0 && m.cursor < len(entries) {
				item := entries[m.cursor]
				if item.action == "node" {
					item.action = "delete-node"
					return m, m.activate(item)
				}
				if item.action == "select" || item.action == "trial" {
					id := item.id
					m.ask("确认删除此 turn-state？HTML/截图和实验记录保留。", func() { removeState(&m.c, id); m.cursor = 0; m.save() })
				}
			}
		}
	}
	if m.form != "" {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}
