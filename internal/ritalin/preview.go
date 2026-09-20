package ritalin

import (
	"context"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

type previewTask struct {
	id, png string
	cancel  context.CancelFunc
}
type previewDone struct {
	task *previewTask
	err  error
}

func (m *ui) cancelPreview() {
	if m.preview != nil {
		m.preview.cancel()
		m.preview = nil
	}
}

// Preview is an optional background task, not a prerequisite for review.
func (m *ui) startPreview() tea.Cmd {
	s := findState(&m.c, m.review)
	if s == nil || s.HTML == "" || m.preview != nil {
		return nil
	}
	if info, err := os.Stat(s.PNG); err == nil && info.Mode().IsRegular() {
		m.notice = "PNG: " + s.PNG
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &previewTask{id: s.ID, png: filepath.Join(filepath.Dir(s.HTML), "pelican.png"), cancel: cancel}
	m.preview = task
	c, store, html := m.c, m.store, s.HTML
	return func() tea.Msg {
		defer cancel()
		err := render(ctx, store, c, html, task.png, func(string) {})
		return previewDone{task: task, err: err}
	}
}

func (m *ui) finishPreview(v previewDone) {
	if m.preview != v.task {
		return
	} // Ignore a cancelled or superseded task.
	m.preview = nil
	s := findState(&m.c, v.task.id)
	if s == nil {
		return
	}
	if v.err != nil {
		m.notice = m.t("预览失败，不影响审核")
		m.log += safeText(v.err.Error()) + "\n"
		m.refreshOutput()
		return
	}
	s.PNG = v.task.png
	m.save()
}
