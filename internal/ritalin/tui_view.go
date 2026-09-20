package ritalin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	uiAccent   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#007F78", Dark: "#5EEAD4"}).Bold(true)
	uiWarning  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}).Bold(true)
	uiMuted    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#8593A8"})
	uiBorder   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#CBD5E1", Dark: "#38475C"})
	uiSelected = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#00665E", Dark: "#99F6E4"}).Background(lipgloss.AdaptiveColor{Light: "#E2F3F0", Dark: "#163C3D"}).Bold(true)
)

type dashboardLayout struct{ width, height, nav, work, detail, main, logs, activity, output int }

func (m *ui) layout() dashboardLayout {
	w, h := m.width, m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 32
	}
	l := dashboardLayout{width: w, height: h, logs: max(4, min(10, h/4)), work: w, output: w}
	l.main = max(4, h-l.logs-7)
	if w >= 72 {
		l.nav = 19
		l.work -= l.nav + 1
	}
	if w >= 118 {
		l.detail = 33
		l.work -= l.detail + 1
	}
	if w >= 100 && m.modelOutput != "" {
		l.activity = w / 3
		l.output = w - l.activity - 1
	}
	return l
}

func fitCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

func fitRows(s string, width, height int) string {
	rows := strings.Split(strings.ReplaceAll(s, "\t", "    "), "\n")
	out := make([]string, max(0, height))
	for i := range out {
		line := ""
		if i < len(rows) {
			line = rows[i]
		}
		out[i] = fitCell(line, width)
	}
	return strings.Join(out, "\n")
}

// Explicit cell sizes keep CJK, ANSI colours and narrow terminals aligned.
func pane(title, body string, width, height int, focused bool) string {
	border := uiBorder
	if focused {
		border = uiAccent
	}
	title = ansi.Truncate(title, max(1, width-6), "…")
	top := border.Render("╭─ ") + uiAccent.Render(title) + border.Render(" "+strings.Repeat("─", max(0, width-lipgloss.Width(title)-5))+"╮")
	rows := []string{top}
	content := strings.Split(fitRows(body, max(1, width-4), max(0, height-2)), "\n")
	for _, line := range content {
		rows = append(rows, border.Render("│")+" "+line+" "+border.Render("│"))
	}
	rows = append(rows, border.Render("╰"+strings.Repeat("─", max(0, width-2))+"╯"))
	return strings.Join(rows, "\n")
}

func (m *ui) resize() {
	l := m.layout()
	m.input.SetWidth(max(1, l.work-4))
	m.input.SetHeight(max(1, l.main-7))
	m.input.Placeholder = m.t("在此输入…")
	m.input.Prompt = "› "
	m.refreshOutput()
}

func (m *ui) refreshOutput() {
	l := m.layout()
	m.viewport.Width, m.viewport.Height = max(1, l.output-4), max(1, l.logs-2)
	m.logViewport.Width, m.logViewport.Height = max(1, l.activity-4), max(1, l.logs-2)
	output := m.modelOutput
	if l.activity == 0 {
		output = m.log + output
	}
	m.viewport.SetContent(ansi.Hardwrap(strings.ToValidUTF8(strings.ReplaceAll(output, "\t", "    "), ""), m.viewport.Width, true))
	m.logViewport.SetContent(ansi.Hardwrap(strings.ToValidUTF8(m.log, ""), m.logViewport.Width, true))
	if !m.paused {
		m.viewport.GotoBottom()
		m.logViewport.GotoBottom()
	}
}

func (m *ui) usableCount() int {
	n := 0
	for _, s := range m.c.States {
		if s.Status == "usable" {
			n++
		}
	}
	return n
}

func (m *ui) pendingCount() int { return len(m.c.States) - m.usableCount() }

func (m *ui) stateLabel(s State) string {
	name := safeText(s.Node)
	if name == "" {
		name = m.t("状态")
	}
	return name + " · " + s.ID[:min(6, len(s.ID))]
}

func (m *ui) selectedEntry() entry {
	items := m.entries()
	if len(items) == 0 {
		return entry{}
	}
	return items[max(0, min(m.cursor, len(items)-1))]
}

func (m *ui) entryLabel(e entry) string {
	if e.action == "node" || e.action == "select" || e.action == "trial" {
		return e.label
	}
	return m.t(e.label)
}

func (m *ui) flowHint() string {
	switch m.tab {
	case tabProxies:
		return m.t("添加代理，自动检测可用性。")
	case tabProbe:
		return m.t("使用已保存的代理获取候选状态。")
	case tabTest:
		return m.t("查看生成效果，保留满意的结果。")
	case tabUse:
		return m.t("Enter 选择，再按一次取消。")
	default:
		return m.t("按需调整，其他保持默认即可。")
	}
}

func (m *ui) navigation(l dashboardLayout) string {
	rows := []string{}
	for i, title := range titles {
		line := fmt.Sprintf("  %d  %s", i+1, m.t(title))
		if i == m.tab {
			line = uiSelected.Render(fitCell("›"+line[1:], l.nav-4))
		} else {
			line = uiMuted.Render(line)
		}
		rows = append(rows, line)
	}
	return pane(m.t("导航"), strings.Join(rows, "\n"), l.nav, l.main, false)
}

func (m *ui) workBody(l dashboardLayout) (string, string) {
	width := l.work - 4
	wrap := func(s string) string { return ansi.Wrap(s, max(1, width), "") }
	if m.form != "" {
		buttons := []string{}
		for i, label := range []string{"保存", "取消"} {
			button := "[ " + m.t(label) + " ]"
			if m.formFocus == i+1 {
				button = uiSelected.Render("› " + button)
			} else {
				button = "  " + uiMuted.Render(button)
			}
			buttons = append(buttons, button)
		}
		return m.t("编辑"), ansi.Truncate(m.formTitle, max(1, width), "…") + "\n\n" + m.input.View() + "\n" + strings.Join(buttons, "  ")
	}
	if m.confirm != "" {
		title, body := m.t("确认"), m.confirm
		if m.confirm == "length-filter" {
			title, body = m.t("过滤候选"), m.t("按长度过滤本轮候选？")
		}
		if m.confirm == "account-kind" {
			return m.t("账户类型"), wrap(m.t("选择账户类型")) + "\n\n" + m.t("[1] 个人账户") + "\n" + m.t("[2] Team / Business")
		}
		return title, wrap(body) + "\n\n" + uiAccent.Render(m.t("[y] 确认   [n / Esc] 取消"))
	}
	if m.review != "" {
		if s := findState(&m.c, m.review); s != nil {
			preview := m.t("[p] 生成图片预览（可选）")
			if s.PNG != "" {
				preview = "PNG: " + safeText(s.PNG)
			}
			if m.preview != nil {
				preview = m.t("正在生成预览…")
			}
			return m.t("审核结果"), wrap(m.t("打开 HTML 后选择：")) + "\n\n" + wrap("HTML: "+safeText(s.HTML)) + "\n\n" + wrap(preview) + "\n\n" + uiAccent.Render(wrap(m.t("[g] 保留   [b] 删除   [s] 稍后")))
		}
	}
	if m.details && l.detail == 0 {
		return m.t("详情"), m.detailBody(width)
	}
	items := m.entries()
	available := max(1, l.main-6)
	cursor := max(0, min(m.cursor, len(items)-1))
	start := max(0, min(cursor-available/2, len(items)-available))
	rows := []string{uiMuted.Render(ansi.Truncate(m.flowHint(), width, "…")), ""}
	for i := start; i < len(items) && i < start+available; i++ {
		label := "  " + m.entryLabel(items[i])
		if i == cursor {
			label = uiSelected.Render(fitCell("› "+m.entryLabel(items[i]), width))
		}
		rows = append(rows, label)
	}
	for len(rows) < l.main-3 {
		rows = append(rows, "")
	}
	rows = append(rows, uiMuted.Render(fmt.Sprintf("%d / %d", cursor+1, len(items))))
	return m.t(titles[m.tab]), strings.Join(rows, "\n")
}

func (m *ui) detailBody(width int) string {
	e := m.selectedEntry()
	rows := []string{}
	add := func(label, value string) {
		if value == "" {
			value = m.t("未设置")
		}
		rows = append(rows, uiMuted.Render(m.t(label)), safeText(value), "")
	}
	for _, n := range m.c.Nodes {
		if e.action == "node" && n.ID == e.id {
			add("节点", n.Name)
			add("出口 IP", n.ExitIP)
			add("可访问", strings.Join(n.Reach, ", "))
			rows = append(rows, m.t("d 删除"))
			return ansi.Wrap(strings.Join(rows, "\n"), max(1, width), "")
		}
	}
	if e.action == "select" || e.action == "trial" {
		if s := findState(&m.c, e.id); s != nil {
			add("来源", s.Node)
			add("状态", m.stateStatus(s.Status))
			add("模型", s.Model)
			hint := m.t("Enter 开始")
			if s.Status == "review" && s.HTML != "" {
				hint = m.t("Enter 审核")
			}
			if e.action == "select" {
				hint = m.t("Enter 使用")
				if m.c.Active == s.ID && m.c.Replace {
					hint = m.t("Enter 取消选择")
				}
			}
			rows = append(rows, uiAccent.Render(hint), m.t("d 删除"))
			return ansi.Wrap(strings.Join(rows, "\n"), max(1, width), "")
		}
	}
	text, value := "", ""
	switch e.action {
	case "import":
		text = "导入 HTTP、HTTPS 或 SOCKS5 代理，自动检测并保存可用节点。"
		if e.id == "clash" {
			text = "导入 Clash 订阅或文件，自动检测并保存可用节点。"
		}
	case "clear":
		text = "删除此分类下的所有节点。"
	case "probe":
		text = "探测已保存节点，收集候选状态。"
		value = fmt.Sprintf("%d %s", len(m.c.Nodes), m.t("节点"))
	case "batch":
		text = "逐个生成动画，完成后由你选择保留或删除。"
		value = fmt.Sprintf("%d %s", m.pendingCount(), m.t("待确认"))
	case "manual":
		text = "粘贴已有状态即可加入列表。"
	case "home":
		text = "选择 Codex 的登录凭证目录。"
		value = probeHome(m.store, m.c)
	case "model":
		text = "留空使用 Codex 配置中的模型。"
		value = m.c.Model
	case "command":
		text = "设置启动 Codex 的命令与参数。"
		b, _ := json.Marshal(m.c.Command)
		value = string(b)
	case "effort":
		text = "选择测试时使用的推理强度。"
		value = m.c.Effort
	case "browser":
		text = "留空自动查找或下载。"
		value = m.c.Browser
	case "mihomo":
		text = "留空自动查找或下载。"
		value = m.c.Mihomo
	case "upstream":
		text = "可选。留空使用当前网络设置。"
		value = m.c.Upstream
		if u, err := url.Parse(value); err == nil && u.User != nil {
			u.User = nil
			value = u.String()
		}
	case "keyword":
		text = "跳过首句含“内联”或“内嵌”的结果。"
		value = m.onOff(m.c.KeywordFilter)
	case "browser-sandbox":
		text = "仅在设备无法启用渲染沙箱时使用。"
		value = m.onOff(m.c.BrowserNoSandbox)
	case "language":
		text = "切换界面显示语言。"
		value = "中文"
		if m.c.Language == "en" {
			value = "English"
		}
	case "advanced":
		text = "按需调整，其他保持默认即可。"
	case "next":
		text = map[string]string{"proxies": "添加代理，自动检测可用性。", "probe": "使用已保存的代理获取候选状态。", "test": "查看生成效果，保留满意的结果。", "use": "Enter 选择，再按一次取消。"}[e.id]
	}
	rows = append(rows, uiAccent.Render(m.entryLabel(e)), "", m.t(text), "")
	if value != "" {
		add("当前值", value)
	}
	return ansi.Wrap(strings.Join(rows, "\n"), max(1, width), "")
}

func (m *ui) languageView(l dashboardLayout) string {
	w := min(56, l.width-4)
	rows := []string{m.t("选择语言"), ""}
	for i, name := range []string{"中文", "English"} {
		label := fmt.Sprintf("  %d  %s", i+1, name)
		if i == m.languageCursor {
			label = uiSelected.Render(fitCell("›"+label[1:], w-4))
		}
		rows = append(rows, label)
	}
	rows = append(rows, "", uiMuted.Render(m.t("↑↓ 选择 · Enter 确认")))
	card := pane("Ritalin", strings.Join(rows, "\n"), w, 9, true)
	return lipgloss.Place(l.width, l.height, lipgloss.Center, lipgloss.Center, card)
}

func (m *ui) warningView(l dashboardLayout) string {
	w := min(86, l.width-4)
	width := w - 4
	banner := "WARNING"
	if width >= 42 && l.height >= 24 {
		banner = "█   █  ███  ████  █   █ █ █   █  ███\n" +
			"█   █ █   █ █   █ ██  █ █ ██  █ █   \n" +
			"█ █ █ █████ ████  █ █ █ █ █ █ █ █ ██\n" +
			"██ ██ █   █ █  █  █  ██ █ █  ██ █   █\n" +
			"█   █ █   █ █   █ █   █ █ █   █  ███"
	}
	rows := []string{}
	center := func(text string) {
		for _, line := range strings.Split(text, "\n") {
			rows = append(rows, lipgloss.PlaceHorizontal(width, lipgloss.Center, line))
		}
	}
	center(uiWarning.Render(banner))
	rows = append(rows, "")
	center(uiWarning.Render(ansi.Wrap("使用本工具可能导致账号被封禁。", width, "")))
	center(uiWarning.Render(ansi.Wrap("Using this tool may result in your account being banned.", width, "")))
	rows = append(rows, "")
	center("Enter 继续 / Continue")
	center("Esc 退出 / Quit")
	if m.notice != "" {
		rows = append(rows, "")
		center(uiMuted.Render(ansi.Truncate(safeText(m.notice), width, "…")))
	}
	card := pane("Ritalin", strings.Join(rows, "\n"), w, len(rows)+2, true)
	return lipgloss.Place(l.width, l.height, lipgloss.Center, lipgloss.Center, card)
}

func (m *ui) View() string {
	l := m.layout()
	if l.width < 40 || l.height < 18 {
		return fitRows("Ritalin\n"+m.t("请将终端放大至 40 × 18"), l.width, l.height)
	}
	if m.warning {
		return m.warningView(l)
	}
	if m.languagePick {
		return m.languageView(l)
	}
	current := m.t("未选择")
	if s := findState(&m.c, m.c.Active); s != nil && s.Status == "usable" && m.c.Replace {
		current = m.stateLabel(*s)
	}
	stats := fmt.Sprintf("%s %d  ·  %s %d  ·  %s %d", m.t("节点"), len(m.c.Nodes), m.t("待确认"), m.pendingCount(), m.t("可用"), m.usableCount())
	if l.width >= 100 {
		stats += "  │  " + m.t("当前使用") + " " + current
	}
	if l.nav == 0 {
		steps := make([]string, len(titles))
		for i, title := range titles {
			steps[i] = fmt.Sprint(i + 1)
			if i == m.tab {
				steps[i] = uiAccent.Render(fmt.Sprintf("%d %s", i+1, m.t(title)))
			}
		}
		stats = strings.Join(steps, "  ·  ")
	}
	header := pane("Ritalin", stats, l.width, 3, false)
	title, body := m.workBody(l)
	if m.review != "" || (m.details && l.detail == 0 && m.form == "" && m.confirm == "") {
		lines := strings.Split(body, "\n")
		offset := max(0, min(m.bodyOffset, len(lines)-(l.main-2)))
		body = strings.Join(lines[offset:], "\n")
	}
	panels := []string{}
	if l.nav > 0 {
		panels = append(panels, m.navigation(l), " ")
	}
	panels = append(panels, pane(title, body, l.work, l.main, true))
	if l.detail > 0 {
		panels = append(panels, " ", pane(m.t("详情"), m.detailBody(l.detail-4), l.detail, l.main, false))
	}
	main := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	logTitle := m.t("运行记录")
	if m.busy {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		elapsed := time.Since(m.started).Round(time.Second)
		logTitle += " · " + frames[m.frame%len(frames)] + " " + elapsed.String()
		if elapsed >= 5*time.Second {
			pulse := m.frame % 8
			logTitle += " " + strings.Repeat("░", pulse) + "█" + strings.Repeat("░", 7-pulse)
		}
	}
	output := m.viewport.View()
	if m.log == "" && m.modelOutput == "" {
		output = uiMuted.Render(m.t("就绪"))
		if m.busy {
			output = uiMuted.Render(m.t("等待输出…"))
		}
	}
	logs := pane(logTitle, output, l.width, l.logs, false)
	if l.activity > 0 {
		outputTitle := m.t("实时输出")
		if m.paused {
			outputTitle += " · " + m.t("已暂停滚动")
		}
		logs = lipgloss.JoinHorizontal(lipgloss.Top, pane(logTitle, m.logViewport.View(), l.activity, l.logs, false), " ", pane(outputTitle, output, l.output, l.logs, false))
	}
	help := m.t("Tab 切区 · ↑↓ 选择 · Enter 操作 · d 删除 · L 语言 · q 退出")
	if l.detail == 0 {
		help = m.t("Tab 切区 · Enter 操作 · 空格详情 · L 语言 · q 退出")
	}
	if l.width < 72 {
		help = m.t("Tab 切区 · Enter 操作 · L 语言")
	}
	if m.form != "" {
		help = m.t("Enter 保存 · Tab 切换 · Esc 取消")
		if m.formFocus != formInput {
			help = m.t("Enter 确认 · Tab 切换 · Esc 取消")
		} else if m.form == "proxy" {
			help = m.t("Enter 换行 · Tab 选择保存 · Esc 取消")
		}
	}
	if m.busy {
		help = m.t("Esc 取消 · PgUp/PgDn 滚动 · End 跟随")
	}
	if m.review != "" {
		help = m.t("[g] 保留   [b] 删除   [s] 稍后") + "  ↑↓"
	}
	if m.confirm != "" {
		help = m.t("[y] 确认   [n / Esc] 取消")
		if m.confirm == "account-kind" {
			help = m.t("[1] 个人账户") + "  " + m.t("[2] Team / Business") + "  Esc"
		}
	}
	footer := fitRows(uiMuted.Render(safeText(m.notice))+"\n"+uiMuted.Render(help), l.width, 2)
	return header + "\n\n" + main + "\n\n" + logs + "\n" + footer
}
