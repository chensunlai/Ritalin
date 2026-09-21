package ritalin

import (
	"fmt"
	"io"
	"net/url"
	"time"
)

const ritalinLogo = `
 ____  _ _        _ _
|  _ \(_) |_ __ _| (_)_ __
| |_) | | __/ _` + "`" + ` | | | '_ \
|  _ <| | || (_| | | | | | |
|_| \_\_|\__\__,_|_|_|_| |_|
           Ritalin
`

func startup(w io.Writer, s *Store, c Config, pause func(time.Duration)) {
	t := func(zh, en string) string {
		if c.Language == "en" {
			return en
		}
		return zh
	}
	state := t("未选择", "None")
	if selected := findState(&c, c.Active); selected != nil && selected.Status == "usable" && c.Replace {
		state = selected.Node + " · " + selected.ID
	}
	upstream := t("自动", "Automatic")
	if c.Upstream != "" {
		upstream = t("已配置", "Configured")
		if u, err := url.Parse(c.Upstream); err == nil && u.Host != "" {
			upstream = u.Scheme + "://" + u.Host
		}
	}
	if selected := findState(&c, c.Active); selected != nil && selected.Status == "usable" && c.Replace && c.UseNode {
		upstream = t("请选择节点", "Select a node")
		if n := useNode(&c, selected); n != nil {
			upstream = t("代理节点", "Proxy node") + " · " + n.Name
		}
	}
	command := ""
	if len(c.Command) > 0 {
		command = c.Command[0]
	}
	fmt.Fprint(w, ritalinLogo)
	for _, row := range [][2]string{
		{t("当前状态", "State"), state},
		{"CODEX_HOME", s.Home},
		{t("启动程序", "Executable"), command},
		{t("网络", "Network"), upstream},
	} {
		fmt.Fprintf(w, "  %s: %s\n", row[0], safeText(row[1]))
	}
	fmt.Fprintln(w, "\n  "+t("正在启动 Codex…", "Starting Codex…"))
	pause(time.Second)
}
