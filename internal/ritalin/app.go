package ritalin

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func codexCommand(c Config, args []string) (*exec.Cmd, error) {
	if len(c.Command) == 0 {
		return nil, errors.New("Codex 启动命令为空")
	}
	if strings.TrimSuffix(filepath.Base(c.Command[0]), ".exe") == "codex-ritalin" {
		return nil, errors.New("Codex 启动命令不能指向 codex-ritalin 自身")
	}
	argv := append(append([]string{}, c.Command[1:]...), args...)
	return exec.Command(c.Command[0], argv...), nil
}
func Run(args []string) (int, error) {
	s := OpenStore()
	c, e := s.Load()
	if e != nil {
		return 1, e
	}
	if len(args) > 0 && args[0] == "dosing" {
		if len(args) > 1 {
			return 2, errors.New("dosing 不接受额外参数；请在 TUI 设置中配置")
		}
		e = runTUI(s, c)
		if e != nil {
			return 1, e
		}
		return 0, nil
	}
	cmd, e := codexCommand(c, args)
	if e != nil {
		return 1, e
	}
	env := os.Environ()
	if os.Getenv("CODEX_HOME") == "" && os.Getenv("CODEXHOME") != "" {
		env = setEnv(env, "CODEX_HOME", s.Home)
	}
	if state := findState(&c, c.Active); state != nil && state.Status == "usable" && c.Replace {
		w, e := startWarp(s, state.Value, c.Replace, c.Upstream)
		if e != nil {
			return 1, e
		}
		defer w.Close()
		env = w.Env(env)
	}
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	startup(os.Stderr, s, c, time.Sleep)
	if e = cmd.Start(); e != nil {
		return 1, e
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	for {
		select {
		case sig := <-signals:
			if sig == syscall.SIGTERM {
				_ = cmd.Process.Signal(sig)
			} // terminal SIGINT already reaches the foreground child
		case e := <-finished:
			if e == nil {
				return 0, nil
			}
			var exit *exec.ExitError
			if errors.As(e, &exit) {
				code := exit.ExitCode()
				if code < 0 {
					code = 130
				}
				return code, nil
			}
			return 1, e
		}
	}
}
