package ritalin

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/gofrs/flock"
)

type Warp struct {
	URL, CA string
	server  *http.Server
	ln      *trackingListener
	tr      *http.Transport
}
type trackingListener struct {
	net.Listener
	mu          sync.Mutex
	connections map[*trackedConn]bool
}
type trackedConn struct {
	net.Conn
	owner *trackingListener
}

func (l *trackingListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	t := &trackedConn{c, l}
	l.mu.Lock()
	l.connections[t] = true
	l.mu.Unlock()
	return t, nil
}
func (c *trackedConn) Close() error {
	c.owner.mu.Lock()
	delete(c.owner.connections, c)
	c.owner.mu.Unlock()
	return c.Conn.Close()
}
func (l *trackingListener) closeAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for c := range l.connections {
		_ = c.Conn.Close()
	}
}
func (w *Warp) Close() { _ = w.server.Close(); w.ln.closeAll(); w.tr.CloseIdleConnections() }
func loadCA(root string) (tls.Certificate, string, error) {
	dir := filepath.Join(root, "ca")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return tls.Certificate{}, "", e
	}
	lock := flock.New(filepath.Join(dir, ".lock"))
	if e := lock.Lock(); e != nil {
		return tls.Certificate{}, "", e
	}
	defer lock.Unlock()
	certfile, keyfile := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "key.pem")
	if _, e := os.Stat(certfile); e == nil {
		cert, e := tls.LoadX509KeyPair(certfile, keyfile)
		return cert, certfile, e
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return tls.Certificate{}, "", e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return tls.Certificate{}, "", e
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Ritalin local CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(5, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		return tls.Certificate{}, "", e
	}
	if e = atomicWrite(keyfile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); e != nil {
		return tls.Certificate{}, "", e
	}
	if e = atomicWrite(certfile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		return tls.Certificate{}, "", e
	}
	cert, e := tls.LoadX509KeyPair(certfile, keyfile)
	return cert, certfile, e
}
func targetRequest(r *http.Request) bool {
	return r != nil && r.URL.Scheme == "https" && ((r.URL.Hostname() == "chatgpt.com" && strings.HasPrefix(r.URL.Path, "/backend-api/codex/")) || (r.URL.Hostname() == "api.openai.com" && strings.HasPrefix(r.URL.Path, "/v1/")))
}

// System upstream is chosen before overriding the child environment. inject is only
// used for independent pelican trials whose first request has no prior state.
func startWarp(s *Store, value string, replace, inject bool, upstream string, observers ...func(string, string)) (*Warp, error) {
	if value != "" {
		if _, e := parseState(value); e != nil {
			return nil, e
		}
	}
	cert, ca, e := loadCA(s.Root)
	if e != nil {
		return nil, e
	}
	tr := systemTransport()
	if upstream != "" {
		tr, e = transport(upstream)
		if e != nil {
			return nil, e
		}
	}
	tr.ResponseHeaderTimeout = 0
	tr.DisableCompression = true
	tr.ForceAttemptHTTP2 = false
	// Preserve an existing private upstream CA configuration, when present.
	if path := getenvAny("CODEX_CA_CERTIFICATE", "SSL_CERT_FILE"); path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		pool, e := x509.SystemCertPool()
		if e != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, errors.New("现有 CA 文件无有效证书")
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	p := goproxy.NewProxyHttpServer()
	p.Tr = tr
	p.Logger = log.New(io.Discard, "", 0)
	p.KeepAcceptEncoding = true
	p.ConnectDial = nil
	p.ConnectDialWithReq = func(req *http.Request, network, address string) (net.Conn, error) {
		u := &url.URL{Scheme: "https", Host: address}
		r := req.Clone(req.Context())
		r.URL = u
		var up *url.URL
		var e error
		if tr.Proxy != nil {
			up, e = tr.Proxy(r)
		}
		if e != nil {
			return nil, e
		}
		return proxyDial(req.Context(), up, address)
	}
	p.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		h, _, e := net.SplitHostPort(host)
		if e == nil && (h == "chatgpt.com" || h == "api.openai.com") {
			return &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(&cert)}, host
		}
		return goproxy.OkConnect, host
	})
	p.OnRequest().DoFunc(func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		// Trial observation needs uncompressed WS frames; normal Codex stays untouched.
		if len(observers) > 0 && targetRequest(r) {
			r.Header.Del("Sec-WebSocket-Extensions")
			r.Header.Del("Accept-Encoding")
		}
		if replace && value != "" && targetRequest(r) && (inject || r.Header.Get(stateHeader) != "") {
			r.Header.Set(stateHeader, value)
		}
		return r, nil
	})
	p.OnResponse().DoFunc(func(r *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if r != nil && replace && value != "" && targetRequest(ctx.Req) && r.Header.Get(stateHeader) != "" {
			r.Header.Set(stateHeader, value)
		}
		if r != nil && targetRequest(ctx.Req) && len(observers) > 0 {
			if r.StatusCode == 101 || strings.Contains(r.Header.Get("Content-Type"), "text/event-stream") {
				tap := &observedBody{ReadCloser: r.Body, ws: r.StatusCode == 101, observe: observers[0]}
				if writer, ok := r.Body.(io.Writer); ok && r.StatusCode == 101 {
					r.Body = &observedSocket{observedBody: tap, Writer: writer}
				} else if r.Header.Get("Content-Encoding") == "" {
					r.Body = tap
				}
			}
		}
		return r
	})
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	tl := &trackingListener{Listener: ln, connections: map[*trackedConn]bool{}}
	server := &http.Server{Handler: p, ReadHeaderTimeout: 15 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	w := &Warp{URL: "http://" + ln.Addr().String(), CA: ca, server: server, ln: tl, tr: tr}
	go server.Serve(tl)
	return w, nil
}
func (w *Warp) Env(env []string) []string {
	env = cleanProxyEnv(env)
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		env = setEnv(env, k, w.URL)
	}
	env = setEnv(env, "NO_PROXY", "localhost,127.0.0.1,::1")
	env = setEnv(env, "no_proxy", "localhost,127.0.0.1,::1")
	return setEnv(env, "CODEX_CA_CERTIFICATE", w.CA)
}
