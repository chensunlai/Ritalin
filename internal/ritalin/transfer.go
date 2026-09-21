package ritalin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const stateFileFormat = "codex-ritalin/states"
const stateFileVersion = 2
const stateFileLimit = 8 << 20

// A portable list, not a configuration backup. Never export credentials,
// proxy configuration, local paths, or the active selection.
type portableState struct {
	Value       string `json:"x-codex-turn-state"`
	Node        string `json:"node,omitempty"`
	Model       string `json:"model,omitempty"`
	Created     string `json:"created,omitempty"`
	AccountHash string `json:"account_hash,omitempty"`
}
type stateFile struct {
	Format  string          `json:"format"`
	Version int             `json:"version"`
	States  []portableState `json:"states"`
}

func exportStates(c Config, path string, usable bool) (int, error) {
	data := stateFile{Format: stateFileFormat, Version: stateFileVersion, States: []portableState{}}
	for _, s := range c.States {
		if (s.Status == "usable") == usable {
			data.States = append(data.States, portableState{Value: s.Value, Node: s.Node, Model: s.Model, Created: s.Created, AccountHash: s.AccountHash})
		}
	}
	if len(data.States) == 0 {
		return 0, errors.New(uiText(c.Language, "当前列表为空"))
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return 0, err
	}
	b = append(b, '\n')
	if len(b) > stateFileLimit {
		return 0, errors.New(uiText(c.Language, "状态文件不能超过 8 MiB"))
	}
	path = ExpandPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return 0, errors.New(uiText(c.Language, "文件已存在，请换一个路径"))
	}
	if err != nil {
		return 0, err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return 0, err
	}
	return len(data.States), nil
}

func importStates(s *Store, c *Config, path string, usable bool) (int, int, error) {
	path = ExpandPath(path)
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, 0, errors.New(uiText(c.Language, "请选择 JSON 文件"))
	}
	if info.Size() > stateFileLimit {
		return 0, 0, errors.New(uiText(c.Language, "状态文件不能超过 8 MiB"))
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	b, err := readLimit(f, stateFileLimit)
	if err != nil {
		return 0, 0, err
	}
	var data stateFile
	if json.Unmarshal(b, &data) != nil || data.Format != stateFileFormat || data.Version != stateFileVersion || data.States == nil {
		return 0, 0, errors.New(uiText(c.Language, "无效的 Ritalin 状态文件"))
	}
	next := clone(*c)
	seen := map[string]bool{}
	for _, state := range next.States {
		seen[strings.TrimRight(state.Value, "=")] = true
	}
	added, skipped := 0, 0
	for i, state := range data.States {
		value := strings.TrimSpace(state.Value)
		metrics, err := parseState(value)
		if err != nil {
			return 0, 0, fmt.Errorf(uiText(c.Language, "第 %d 个状态无效，未导入"), i+1)
		}
		key := strings.TrimRight(value, "=")
		if seen[key] {
			skipped++
			continue
		}
		seen[key] = true
		status := "pending"
		if usable {
			status = "usable"
		}
		node := state.Node
		if node == "" {
			node = uiText(c.Language, "导入状态")
		}
		next.States = append(next.States, State{ID: newID(), Value: value, Node: node, Model: state.Model, Created: state.Created, AccountHash: state.AccountHash, Metrics: metrics, Status: status, AuthHome: probeHome(s, *c)})
		added++
	}
	if added > 0 {
		if err := s.Save(next); err != nil {
			return 0, 0, err
		}
		*c = next
	}
	return added, skipped, nil
}
