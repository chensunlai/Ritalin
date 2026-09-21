package ritalin

import (
	"bufio"
	"bytes"
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
func TestReplaceExistingState(t *testing.T) {
	for _, initial := range []http.Header{
		{},
		{http.CanonicalHeaderKey(stateHeader): {""}},
		{http.CanonicalHeaderKey(stateHeader): {"original"}},
	} {
		_, exists := initial[http.CanonicalHeaderKey(stateHeader)]
		replaceExistingState(initial, syntheticState())
		if got, present := initial[http.CanonicalHeaderKey(stateHeader)]; present != exists || (present && got[0] != syntheticState()) {
			t.Fatalf("existing=%v, result=%v", exists, initial)
		}
	}
}

func TestWarpMissingHeadersStayMissing(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, exists := r.Header[http.CanonicalHeaderKey(stateHeader)]; exists {
			t.Error("missing request state was added")
		}
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Error("Accept-Encoding changed")
		}
		fmt.Fprint(w, "unchanged")
	}))
	defer origin.Close()
	observed := make(chan warpStateEvent, 2)
	warp, err := startWarpObserved(&Store{Root: t.TempDir()}, syntheticState(), true, "", func(e warpStateEvent) { observed <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer warp.Close()
	warp.tr.Proxy = nil
	warp.tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(origin.URL, "https://"))
	}
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	warp.tr.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com"}
	ca, err := os.ReadFile(warp.CA)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	u, _ := url.Parse(warp.URL)
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u), TLSClientConfig: &tls.Config{RootCAs: pool}}, Timeout: 3 * time.Second}
	defer cl.CloseIdleConnections()
	req, _ := http.NewRequest("GET", "https://chatgpt.com/backend-api/codex/responses", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, exists := resp.Header[http.CanonicalHeaderKey(stateHeader)]; exists {
		t.Fatal("missing response state was added")
	}
	requestState, responseState := nextWarpState(t, observed), nextWarpState(t, observed)
	if requestState.Present || responseState.Present || requestState.Response || !responseState.Response || responseState.HTTPStatus != 200 {
		t.Fatal("absent headers must be observed as absent in both directions")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "unchanged" {
		t.Fatalf("body changed: %q, %v", body, err)
	}
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
			observed := make(chan warpStateEvent, 2)
			warp, e := startWarpObserved(store, syntheticState(), enabled, up.URL, func(e warpStateEvent) { observed <- e })
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
			requestState, responseState := nextWarpState(t, observed), nextWarpState(t, observed)
			if requestState.Response || !requestState.Present || len(requestState.Values) != 1 || requestState.Values[0] != expectedReq {
				t.Fatal("observer must see the actual outbound state")
			}
			if !responseState.Response || !responseState.Present || len(responseState.Values) != 1 || responseState.Values[0] != "original-response" || responseState.HTTPStatus != 200 {
				t.Fatal("observer must see the original server state, not the replacement")
			}
			if requestState.RequestID != responseState.RequestID || requestState.Method != "POST" || requestState.Path != "/backend-api/codex/responses" {
				t.Fatal("request/response correlation lost")
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
		})
	}
}
func TestWarpWebSocketUpgrade(t *testing.T) {
	delta := []byte(`{"type":"response.output_text.delta","delta":"websocket text"}`)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(stateHeader) != syntheticState() {
			t.Error("WS request header not replaced")
		}
		if r.Header.Get("Accept-Encoding") != "gzip" || r.Header.Get("Sec-WebSocket-Extensions") != "permessage-deflate" {
			t.Error("compression negotiation modified")
		}
		conn, rw, e := w.(http.Hijacker).Hijack()
		if e != nil {
			t.Error(e)
			return
		}
		defer conn.Close()
		fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n%s: server-original\r\n", stateHeader)
		for i := 0; i < 24; i++ {
			fmt.Fprintf(rw, "X-Test-%d: value\r\n", i)
		}
		rw.WriteString("\r\n")
		rw.Write(append([]byte{0x81, byte(len(delta))}, delta...))
		rw.Flush()
	}))
	defer origin.Close()
	observed := make(chan warpStateEvent, 2)
	warp, e := startWarpObserved(&Store{Root: t.TempDir()}, syntheticState(), true, "", func(e warpStateEvent) { observed <- e })
	if e != nil {
		t.Fatal(e)
	}
	defer warp.Close()
	warp.tr.Proxy = nil
	warp.tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(origin.URL, "https://"))
	}
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	warp.tr.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com"}
	ca, _ := os.ReadFile(warp.CA)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	u, _ := url.Parse(warp.URL)
	conn, e := net.DialTimeout("tcp", u.Host, 3*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprint(conn, "CONNECT chatgpt.com:443 HTTP/1.1\r\nHost: chatgpt.com:443\r\n\r\n")
	if _, e := http.ReadResponse(bufio.NewReader(conn), nil); e != nil {
		t.Fatal(e)
	}
	client := tls.Client(conn, &tls.Config{RootCAs: pool, ServerName: "chatgpt.com"})
	defer client.Close()
	fmt.Fprintf(client, "GET /backend-api/codex/responses HTTP/1.1\r\nHost: chatgpt.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nAccept-Encoding: gzip\r\nSec-WebSocket-Extensions: permessage-deflate\r\n%s: client-original\r\n\r\n", stateHeader)
	var head []byte
	buffer := make([]byte, 4096)
	for reads := 1; !bytes.Contains(head, []byte("\r\n\r\n")); reads++ {
		n, err := client.Read(buffer)
		if err != nil {
			t.Fatal(err)
		}
		head = append(head, buffer[:n]...)
		// Match the strict WebSocket client's small-packet handshake guard.
		if reads > 64 && reads*128 > len(head) {
			t.Fatal("handshake fragmented into excessive small TLS records")
		}
	}
	end := bytes.Index(head, []byte("\r\n\r\n")) + 4
	resp, e := http.ReadResponse(bufio.NewReader(bytes.NewReader(head[:end])), nil)
	if e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != 101 || resp.Header.Get(stateHeader) != syntheticState() {
		t.Fatal("upgrade changed or missing replaced header")
	}
	requestState, responseState := nextWarpState(t, observed), nextWarpState(t, observed)
	if len(requestState.Values) != 1 || requestState.Values[0] != syntheticState() || len(responseState.Values) != 1 || responseState.Values[0] != "server-original" || responseState.HTTPStatus != 101 {
		t.Fatal("WebSocket handshake headers were not observed before response replacement")
	}
	b, e := io.ReadAll(io.MultiReader(bytes.NewReader(head[end:]), client))
	if e != nil {
		t.Fatal(e)
	}
	if string(b[2:]) != string(delta) {
		t.Fatal("WS payload modified")
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
