package ritalin

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func syntheticState() string {
	b := make([]byte, 217)
	b[0] = 128
	return base64.URLEncoding.EncodeToString(b)
}
func TestWarpHTTPSUpstreamStreamingAndControl(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			seen := make(chan string, 1)
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- r.Header.Get(stateHeader)
				w.Header().Set(stateHeader, "original-response")
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"one\"}\n\n")
				w.(http.Flusher).Flush()
				time.Sleep(300 * time.Millisecond)
				fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"two\"}\n\n")
			}))
			defer origin.Close()
			connected := make(chan string, 4)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connected <- r.Host
				dst, e := net.Dial("tcp", strings.TrimPrefix(origin.URL, "https://"))
				if e != nil {
					t.Error(e)
					return
				}
				client, rw, e := w.(http.Hijacker).Hijack()
				if e != nil {
					t.Error(e)
					return
				}
				_, _ = rw.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
				rw.Flush()
				go func() { defer dst.Close(); _, _ = io.Copy(dst, rw) }()
				defer client.Close()
				_, _ = io.Copy(client, dst)
			}))
			defer up.Close()
			store := &Store{Root: t.TempDir()}
			deltas := make(chan string, 4)
			warp, e := startWarp(store, syntheticState(), enabled, false, up.URL, func(kind, text string) { deltas <- text })
			if e != nil {
				t.Fatal(e)
			}
			defer warp.Close()
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			warp.tr.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}
			ca, e := os.ReadFile(warp.CA)
			if e != nil {
				t.Fatal(e)
			}
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(ca)
			u, _ := url.Parse(warp.URL)
			cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u), TLSClientConfig: &tls.Config{RootCAs: pool}}, Timeout: 3 * time.Second}
			defer cl.CloseIdleConnections()
			req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader("test"))
			req.Header.Set(stateHeader, "original-request")
			resp, e := cl.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			defer resp.Body.Close()
			expectedReq, expectedResp := "original-request", "original-response"
			if enabled {
				expectedReq = syntheticState()
				expectedResp = syntheticState()
			}
			if got := <-seen; got != expectedReq {
				t.Fatalf("request got %q", got)
			}
			if got := resp.Header.Get(stateHeader); got != expectedResp {
				t.Fatalf("response got %q", got)
			}
			if <-connected != "chatgpt.com:443" {
				t.Fatal("wrong upstream CONNECT")
			}
			sc := bufio.NewScanner(resp.Body)
			var times []time.Time
			for sc.Scan() {
				if strings.HasPrefix(sc.Text(), "data:") {
					times = append(times, time.Now())
				}
			}
			if sc.Err() != nil {
				t.Fatal(sc.Err())
			}
			if len(times) != 2 || times[1].Sub(times[0]) < 150*time.Millisecond {
				t.Fatal("SSE buffered")
			}
			if <-deltas != "one" || <-deltas != "two" {
				t.Fatal("not observed")
			}
		})
	}
}
func TestObserverFragmentedWebSocket(t *testing.T) {
	got := ""
	tap := &observedBody{ws: true, observe: func(kind, text string) { got += text }}
	message := []byte(`{"type":"response.output_text.delta","delta":"hello"}`)
	frame := append([]byte{0x81, byte(len(message))}, message...)
	for _, b := range frame {
		tap.feed([]byte{b})
	}
	if got != "hello" {
		t.Fatal(got)
	}
}
func TestKeywordAndHTML(t *testing.T) {
	if !keywordRejected("我将使用内嵌 SVG。后续") || keywordRejected("第一句。后续内联") {
		t.Fatal("first sentence filter")
	}
	s := "```html\n<!doctype html><html>test</html>\n```"
	if extractHTML(s) != "<!doctype html><html>test</html>" {
		t.Fatal("HTML extraction")
	}
}
func TestNoProxyFallback(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7")
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:8")
	tr, _ := transport("http://127.0.0.1:9")
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://example.com", nil)
	u, _ := tr.Proxy(req)
	if u.Host != "127.0.0.1:9" {
		t.Fatal("explicit proxy lost")
	}
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "example.com")
	t.Setenv("no_proxy", "example.com")
	u, e := systemProxy(req)
	if e != nil || u != nil {
		t.Fatal("ALL_PROXY ignored NO_PROXY", u, e)
	}
}
