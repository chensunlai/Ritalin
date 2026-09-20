package ritalin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func dashboardFixture(t *testing.T) *ui {
	t.Helper()
	c := Defaults()
	c.Nodes = []Node{{ID: "node-one", Name: "Tokyo", Kind: "proxy", URL: "http://hidden-user:hidden-password@127.0.0.1:8888", ExitIP: "192.0.2.1"}, {ID: "node-two", Name: "Singapore", Kind: "clash"}}
	metrics, _ := parseState(syntheticState())
	c.States = []State{{ID: "state-one", Node: "Tokyo", Value: syntheticState(), Status: "usable", Metrics: metrics, Model: "test-model"}, {ID: "state-two", Node: "Singapore", Status: "review", Model: "test-model", HTML: "/example/pelican/result.html", PNG: "/example/pelican/result.png"}}
	s := &Store{Home: "/example/codex", Root: t.TempDir()}
	m := &ui{store: s, c: c, width: 120, height: 32, input: textarea.New(), viewport: viewport.New(50, 6), logViewport: viewport.New(30, 6)}
	m.input.ShowLineNumbers = false
	m.resize()
	return m
}

func TestDashboardFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{32, 12}, {40, 18}, {60, 20}, {80, 24}, {100, 32}, {120, 32}, {160, 44}} {
		for _, lang := range []string{"zh", "en"} {
			for _, mode := range []string{"idle", "busy", "form", "confirm", "review", "language", "details"} {
				t.Run(fmt.Sprintf("%dx%d/%s/%s", size[0], size[1], lang, mode), func(t *testing.T) {
					m := dashboardFixture(t)
					m.c.Language = lang
					m.c.Nodes[0].Name = strings.Repeat("测试🌿é", 30)
					m.width, m.height = size[0], size[1]
					switch mode {
					case "busy":
						m.busy = true
						m.started = time.Now().Add(-6 * time.Second)
						m.log = strings.Repeat("checking…\n", 20)
						m.modelOutput = strings.Repeat("SVG 输出 π🌿", 80)
					case "form":
						m.openForm("manual", "粘贴 x-codex-turn-state", syntheticState())
					case "confirm":
						m.confirm = "account-kind"
					case "review":
						m.review = "state-two"
					case "language":
						m.chooseLanguage()
					case "details":
						m.details = true
					}
					m.resize()
					view := m.View()
					rows := strings.Split(view, "\n")
					if len(rows) != size[1] {
						t.Fatalf("height = %d, expected %d\n%s", len(rows), size[1], view)
					}
					for i, row := range rows {
						if lipgloss.Width(row) > size[0] {
							t.Fatalf("row %d: width %d > %d: %s", i, lipgloss.Width(row), size[0], row)
						}
					}
				})
			}
		}
	}
}

func TestLanguageSelectionAndInterface(t *testing.T) {
	m := dashboardFixture(t)
	m.chooseLanguage()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	stored, err := m.store.Load()
	if err != nil || stored.Language != "en" || m.languagePick {
		t.Fatal("language selection was not saved", err)
	}
	for i := range titles {
		m.tab = i
		for j := range m.entries() {
			m.cursor = j
			for _, r := range ansi.Strip(m.View()) {
				if unicode.Is(unicode.Han, r) {
					t.Fatalf("untranslated interface in section %d item %d: %q", i, j, r)
				}
			}
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	if !m.languagePick || m.languageCursor != 1 {
		t.Fatal("language switch lost the saved choice")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.c.Language != "zh" {
		t.Fatal("could not switch back")
	}
	if strings.Contains(m.View(), "利他林") {
		t.Fatal("brand title contains two languages")
	}
}

func TestDashboardANSIAlignment(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := dashboardFixture(t)
	view := m.View()
	if !strings.Contains(view, "\x1b[") {
		t.Fatal("colours were not exercised")
	}
	for _, row := range strings.Split(view, "\n") {
		if lipgloss.Width(row) > m.width {
			t.Fatal("coloured row exceeds the terminal width")
		}
	}
}

func TestWorkflowAndSimpleSelection(t *testing.T) {
	m := dashboardFixture(t)
	if len(titles) != 5 {
		t.Fatal("expected four workflow pages and settings")
	}
	for _, step := range []struct {
		tab  int
		next string
	}{{tabProxies, "probe"}, {tabProbe, "test"}, {tabTest, "use"}} {
		m.tab = step.tab
		found := false
		for _, e := range m.entries() {
			if e.action == "next" && e.id == step.next {
				found = true
				m.activate(e)
				break
			}
		}
		if !found || m.tab != step.tab+1 {
			t.Fatalf("missing next step for %d", step.tab)
		}
	}
	m.tab = tabUse
	for _, e := range m.entries() {
		if e.action != "manual" && e.action != "select" {
			t.Fatalf("redundant use action: %s", e.action)
		}
	}
	m.c.Replace = false
	m.c.Active = "state-one"
	m.activate(entry{action: "select", id: "state-one"})
	if !m.c.Replace || m.c.Active != "state-one" {
		t.Fatal("could not enable a previously disabled state")
	}
	m.activate(entry{action: "select", id: "state-one"})
	if m.c.Active != "" {
		t.Fatal("could not deselect")
	}
	for _, word := range []string{"对照", "实验规则", "双向替换", "直接运行 Codex"} {
		if strings.Contains(m.View(), word) {
			t.Fatalf("unnecessary UI copy: %s", word)
		}
	}
	m.c.Nodes = nil
	m.tab = tabProbe
	if m.entries()[0].action != "next" || m.entries()[0].id != "proxies" {
		t.Fatal("empty probe page does not explain the next step")
	}
}

func TestDashboardOutputAndScroll(t *testing.T) {
	m := dashboardFixture(t)
	m.busy = true
	m.started = time.Now()
	m.Update(logMsg("working"))
	m.Update(logMsg("\x00output:" + strings.Repeat("line one\nline two\n", 30)))
	if !strings.Contains(m.View(), "line two") || !strings.Contains(m.View(), "working") {
		t.Fatal("output or activity is not visible")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	offset := m.viewport.YOffset
	m.Update(logMsg("\x00output:last line\n"))
	if !m.paused || m.viewport.YOffset != offset {
		t.Fatal("new output stole the scroll position")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.paused || !strings.Contains(m.View(), "last line") {
		t.Fatal("End did not resume following")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(m.View(), "last line") {
		t.Fatal("resize hid the latest output")
	}
}

func TestFormValidationKeepsInput(t *testing.T) {
	m := dashboardFixture(t)
	m.openForm("command", "启动命令", "not-json")
	m.applyForm()
	if m.form != "command" || m.input.Value() != "not-json" {
		t.Fatal("invalid input was lost")
	}
	m.input.SetValue(`["codex"]`)
	m.applyForm()
	if m.form != "" {
		t.Fatal("valid input did not close the form")
	}
}

func TestNodeSelectionIsNotDeletion(t *testing.T) {
	m := dashboardFixture(t)
	for i, e := range m.entries() {
		if e.action == "node" {
			m.cursor = i
			break
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirm != "" || len(m.c.Nodes) != 2 {
		t.Fatal("Enter should show details, not delete a node")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if m.confirm == "" {
		t.Fatal("delete did not ask for confirmation")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if len(m.c.Nodes) != 1 {
		t.Fatal("confirmed node was not removed")
	}
}

func TestDashboardNoSecrets(t *testing.T) {
	m := dashboardFixture(t)
	m.c.Upstream = "http://hidden-user:hidden-password@proxy.example:8080"
	m.tab, m.advanced = tabSettings, true
	for i := range m.entries() {
		m.cursor = i
		view := m.View()
		for _, secret := range []string{syntheticState(), "hidden-user", "hidden-password"} {
			if strings.Contains(view, secret) {
				t.Fatal("secret exposed in dashboard")
			}
		}
	}
}

func TestDashboardPreview(t *testing.T) {
	dir := os.Getenv("RITALIN_UI_PREVIEW")
	if dir == "" {
		t.Skip("set RITALIN_UI_PREVIEW to export synthetic terminal previews")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m := dashboardFixture(t)
	for _, lang := range []string{"zh", "en"} {
		m.c.Language = lang
		m.c.Active = "state-one"
		m.tab = tabUse
		m.cursor = 1
		if err := os.WriteFile(filepath.Join(dir, lang+".txt"), []byte(ansi.Strip(m.View())), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.c.Language = "en"
	m.tab = tabTest
	m.busy, m.started = true, time.Now().Add(-12*time.Second)
	m.log = "Testing Singapore\nGenerating animation…\n"
	m.modelOutput = "Creating the SVG animation…\n<svg viewBox=\"0 0 800 500\">\n  <g id=\"pelican\">\n    <path d=\"M 80 180 Q 160 60 220 180\" />\n  </g>\n</svg>"
	m.resize()
	if err := os.WriteFile(filepath.Join(dir, "live.txt"), []byte(ansi.Strip(m.View())), 0600); err != nil {
		t.Fatal(err)
	}
}
