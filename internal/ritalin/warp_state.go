package ritalin

import (
	"fmt"
	"net/http"
	"strings"
)

// Header snapshots only: never retain requests, credentials, or stream bodies.
type warpStateEvent struct {
	RequestID  int64
	Method     string
	Path       string
	Response   bool
	HTTPStatus int
	Present    bool
	Values     []string
}

func newWarpStateEvent(id int64, req *http.Request, resp *http.Response, response bool) warpStateEvent {
	e := warpStateEvent{RequestID: id, Method: req.Method, Path: req.URL.EscapedPath(), Response: response}
	header := req.Header
	if response {
		header = nil
		if resp != nil {
			header = resp.Header
			e.HTTPStatus = resp.StatusCode
		}
	}
	values, present := header[http.CanonicalHeaderKey(stateHeader)]
	e.Present, e.Values = present, append([]string(nil), values...)
	return e
}

func (e warpStateEvent) text(language string) string {
	direction := uiText(language, "发送（替换后）")
	if e.Response {
		direction = uiText(language, "接收（服务器原始，未替换）")
	}
	prefix := fmt.Sprintf("[state #%d] %s %s %s", e.RequestID, direction, e.Method, e.Path)
	if e.Response {
		if e.HTTPStatus == 0 {
			return prefix + " · " + uiText(language, "未收到 HTTP 响应")
		}
		prefix += fmt.Sprintf(" · HTTP %d", e.HTTPStatus)
	}
	if !e.Present {
		return prefix + " · x-codex-turn-state: " + uiText(language, "无")
	}
	var out strings.Builder
	out.WriteString(prefix)
	for _, value := range e.Values {
		metrics, err := parseState(value)
		fmt.Fprintf(&out, "\n"+uiText(language, "%d 字符"), len(value))
		if err == nil {
			fmt.Fprintf(&out, " · "+uiText(language, "密文 %d 字节 · %d 块"), metrics.CipherBytes, metrics.Blocks)
		} else {
			out.WriteString(" · " + uiText(language, "布局无法解析"))
		}
		// Quote untrusted header values so terminal control characters stay inert.
		fmt.Fprintf(&out, "\nx-codex-turn-state: %q", value)
	}
	return out.String()
}
