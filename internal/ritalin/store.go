package ritalin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Node struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	URL     string         `json:"url,omitempty"`
	Clash   map[string]any `json:"clash,omitempty"`
	Checked string         `json:"checked,omitempty"`
	Reach   []string       `json:"reach,omitempty"`
	ExitIP  string         `json:"exit_ip,omitempty"`
}
type Metrics struct {
	Characters   int    `json:"characters"`
	DecodedBytes int    `json:"decoded_bytes"`
	CipherBytes  int    `json:"cipher_bytes"`
	Blocks       int    `json:"blocks"`
	Version      int    `json:"version"`
	Timestamp    uint64 `json:"timestamp"`
}
type State struct {
	ID             string  `json:"id"`
	Value          string  `json:"value"`
	Node           string  `json:"node"`
	NodeID         string  `json:"node_id,omitempty"`
	UseNodeID      string  `json:"use_node_id,omitempty"`
	Created        string  `json:"created"`
	Status         string  `json:"status"` // pending, review, usable
	Metrics        Metrics `json:"metrics"`
	AuthHome       string  `json:"auth_home"`
	AccountHash    string  `json:"account_hash"`
	Model          string  `json:"model"`
	ProbeCompleted bool    `json:"probe_completed"`
	HTML           string  `json:"html,omitempty"`
	PNG            string  `json:"png,omitempty"`
	LastError      string  `json:"last_error,omitempty"`
}
type Config struct {
	Version          int      `json:"version"`
	Language         string   `json:"language,omitempty"`
	RiskAcknowledged bool     `json:"risk_acknowledged,omitempty"`
	Command          []string `json:"codex_command"`
	ProbeHome        string   `json:"probe_codex_home,omitempty"`
	Model            string   `json:"probe_model,omitempty"`
	Effort           string   `json:"pelican_effort"`
	Browser          string   `json:"browser_path,omitempty"`
	BrowserNoSandbox bool     `json:"browser_no_sandbox"`
	Mihomo           string   `json:"mihomo_path,omitempty"`
	Upstream         string   `json:"warp_upstream,omitempty"`
	UseNode          bool     `json:"use_node,omitempty"`
	Replace          bool     `json:"replace"`
	KeywordFilter    bool     `json:"keyword_filter"`
	Active           string   `json:"active_state,omitempty"`
	Nodes            []Node   `json:"nodes"`
	States           []State  `json:"states"`
}
type Store struct{ Home, Root string }

func ExpandPath(s string) string {
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		h, _ := os.UserHomeDir()
		s = filepath.Join(h, strings.TrimLeft(s[1:], `/\`))
	}
	a, e := filepath.Abs(s)
	if e == nil {
		return a
	}
	return s
}
func DefaultHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return ExpandPath(h)
	}
	if h := os.Getenv("CODEXHOME"); h != "" {
		return ExpandPath(h)
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".codex")
}
func OpenStore() *Store { h := DefaultHome(); return &Store{h, filepath.Join(h, "ritalin")} }
func Defaults() Config {
	return Config{Version: 1, Command: []string{"codex"}, Effort: "low", Replace: true, Nodes: []Node{}, States: []State{}}
}
func (s *Store) Load() (Config, error) {
	c := Defaults()
	b, e := os.ReadFile(filepath.Join(s.Root, "config.json"))
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, fmt.Errorf("配置无效，未覆盖: %w", e)
	}
	if c.Version != 1 {
		return c, fmt.Errorf("不支持的配置版本 %d", c.Version)
	}
	if len(c.Command) == 0 || c.Command[0] == "" {
		return c, errors.New("codex_command 不能为空")
	}
	return c, nil
}
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func (s *Store) Save(c Config) error {
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(filepath.Join(s.Root, "config.json"), append(b, '\n'), 0600)
}
func (s *Store) Record(dir, name string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(filepath.Join(dir, name), b, 0600)
}
func newID() string        { b := make([]byte, 8); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func stamp() string        { return time.Now().UTC().Format(time.RFC3339) }
func hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func (s *Store) runDir(kind string) (string, error) {
	p := filepath.Join(s.Root, kind, time.Now().UTC().Format("20060102T150405")+"-"+newID())
	return p, os.MkdirAll(p, 0700)
}
func clone(c Config) Config {
	b, _ := json.Marshal(c)
	var out Config
	_ = json.Unmarshal(b, &out)
	return out
}
func removeState(c *Config, id string) {
	out := c.States[:0]
	for _, s := range c.States {
		if s.ID != id {
			out = append(out, s)
		}
	}
	c.States = out
	if c.Active == id {
		c.Active = ""
	}
}
func findState(c *Config, id string) *State {
	for i := range c.States {
		if c.States[i].ID == id {
			return &c.States[i]
		}
	}
	return nil
}
func probeHome(s *Store, c Config) string {
	if c.ProbeHome != "" {
		return ExpandPath(c.ProbeHome)
	}
	return s.Home
}
