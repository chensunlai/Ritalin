package ritalin

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
)

func cleanProxyEnv(env []string) []string {
	out := []string{}
	for _, v := range env {
		k, _, _ := strings.Cut(v, "=")
		switch strings.ToLower(k) {
		case "http_proxy", "https_proxy", "all_proxy", "no_proxy":
		default:
			out = append(out, v)
		}
	}
	return out
}
func setEnv(env []string, key, val string) []string {
	out := []string{}
	for _, s := range env {
		k, _, _ := strings.Cut(s, "=")
		if k != key {
			out = append(out, s)
		}
	}
	return append(out, key+"="+val)
}
func getenvAny(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// Explicit mode: no ProxyFromEnvironment and no fallback on failure.
func transport(proxyURL string) (*http.Transport, error) {
	tr := &http.Transport{DialContext: (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 45 * time.Second, ForceAttemptHTTP2: true}
	if proxyURL != "" {
		u, e := parseProxy(proxyURL)
		if e != nil {
			return nil, e
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return tr, nil
}
func systemProxy(req *http.Request) (*url.URL, error) {
	c := httpproxy.FromEnvironment()
	all := getenvAny("ALL_PROXY", "all_proxy")
	if c.HTTPProxy == "" {
		c.HTTPProxy = all
	}
	if c.HTTPSProxy == "" {
		c.HTTPSProxy = all
	}
	return c.ProxyFunc()(req.URL)
}
func systemTransport() *http.Transport { tr, _ := transport(""); tr.Proxy = systemProxy; return tr }
func parseProxy(raw string) (*url.URL, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil {
		return nil, errors.New("代理 URL 无效")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("仅支持 http://、https://、socks5:// 代理")
	}
	if u.Hostname() == "" || u.Port() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("代理格式应为 scheme://[user:pass@]host:port")
	}
	return u, nil
}
func proxyLabel(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return "proxy"
	}
	return u.Scheme + "://" + u.Host
}
func readSource(ctx context.Context, input string) ([]byte, error) {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "http://") {
		req, e := http.NewRequestWithContext(ctx, "GET", input, nil)
		if e != nil {
			return nil, errors.New("订阅 URL 无效")
		}
		req.Header.Set("User-Agent", "Clash.Meta")
		cl := &http.Client{Transport: systemTransport(), Timeout: 45 * time.Second}
		defer cl.CloseIdleConnections()
		r, e := cl.Do(req)
		if e != nil {
			return nil, errors.New("订阅下载失败（检查系统代理和地址）")
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return nil, fmt.Errorf("订阅 HTTP %d", r.StatusCode)
		}
		return readLimit(r.Body, 8<<20)
	}
	return os.ReadFile(ExpandPath(input))
}
func readLimit(r io.Reader, limit int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("数据超过大小限制")
	}
	return b, e
}
func proxyDial(ctx context.Context, upstream *url.URL, address string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if upstream == nil {
		return d.DialContext(ctx, "tcp", address)
	}
	if strings.HasPrefix(upstream.Scheme, "socks5") {
		var a *proxy.Auth
		if upstream.User != nil {
			p, _ := upstream.User.Password()
			a = &proxy.Auth{User: upstream.User.Username(), Password: p}
		}
		sd, e := proxy.SOCKS5("tcp", upstream.Host, a, d)
		if e != nil {
			return nil, e
		}
		return sd.(proxy.ContextDialer).DialContext(ctx, "tcp", address)
	}
	c, e := d.DialContext(ctx, "tcp", upstream.Host)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if upstream.Scheme == "https" {
		tc := tls.Client(c, &tls.Config{ServerName: upstream.Hostname(), MinVersion: tls.VersionTLS12})
		if e = tc.HandshakeContext(ctx); e != nil {
			return nil, e
		}
		c = tc
	}
	r := &http.Request{Method: "CONNECT", URL: &url.URL{Opaque: address}, Host: address, Header: make(http.Header)}
	if upstream.User != nil {
		p, _ := upstream.User.Password()
		r.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(upstream.User.Username()+":"+p)))
	}
	if e = r.Write(c); e != nil {
		return nil, e
	}
	br := bufio.NewReader(c)
	resp, e := http.ReadResponse(br, r)
	if e != nil {
		return nil, e
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("上游 CONNECT HTTP %d", resp.StatusCode)
	}
	_ = c.SetDeadline(time.Time{})
	ok = true
	return &bufferedConn{Conn: c, r: br}, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.r.Read(b) }
