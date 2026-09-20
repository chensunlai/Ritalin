package ritalin

import (
	"archive/zip"
	"bytes"
	"context"
	"debug/elf"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"
)

func TestBrowserDownloadPlatform(t *testing.T) {
	for _, platform := range [][2]string{{"linux", "arm64"}, {"linux", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}} {
		t.Run(strings.Join(platform[:], "-"), func(t *testing.T) {
			b := launcher.NewBrowser()
			b.RootDir = t.TempDir()
			root, revision := b.RootDir, b.Revision
			var urls []string
			for _, host := range b.Hosts {
				urls = append(urls, host(b.Revision))
			}
			configureBrowserDownload(b, platform[0], platform[1])
			if platform != [2]string{"linux", "arm64"} {
				if b.RootDir != root || b.Revision != revision || len(b.Hosts) != len(urls) {
					t.Fatal("other platform changed")
				}
				for i, host := range b.Hosts {
					if host(b.Revision) != urls[i] {
						t.Fatal("other platform URL changed")
					}
				}
				return
			}
			if b.Revision != 1193 || b.RootDir != filepath.Join(root, "linux-arm64") || len(b.Hosts) != 3 {
				t.Fatal("incorrect ARM64 configuration")
			}
			seen := map[string]bool{}
			for _, host := range b.Hosts {
				u := host(b.Revision)
				if seen[u] || !strings.HasSuffix(u, "/builds/chromium/1193/chromium-linux-arm64.zip") || strings.Contains(u, "azureedge.net") {
					t.Fatal("invalid mirror", u)
				}
				seen[u] = true
			}
		})
	}
}

func TestARM64BrowserArchiveLayout(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux archive layout")
	}
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	h := &zip.FileHeader{Name: "chrome-linux/chrome", Method: zip.Store}
	h.SetMode(0755)
	w, err := z.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	// The downloader's mirror race reads 64 KiB before selecting a URL.
	payload := bytes.Repeat([]byte("synthetic browser\n"), 8192)
	if _, err = w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/builds/chromium/1193/chromium-linux-arm64.zip" {
			t.Error("wrong archive path", r.URL.Path)
		}
		_, _ = w.Write(archive.Bytes())
	}))
	defer srv.Close()
	b := launcher.NewBrowser()
	b.RootDir = t.TempDir()
	configureBrowserDownload(b, "linux", "arm64")
	b.Hosts = []launcher.Host{func(int) string { return srv.URL + "/builds/chromium/1193/chromium-linux-arm64.zip" }}
	b.HTTPClient = srv.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b.Context = ctx
	if err = b.Download(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(b.BinPath())
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("executable not extracted at expected path", err)
	}
	info, err := os.Stat(b.BinPath())
	if err != nil || info.Mode()&0111 == 0 {
		t.Fatal("executable permission lost", err)
	}
}

// Opt-in network smoke test: verify the real downloaded executable's architecture.
// On ARM64 runners, also check that it can start. No account or generated HTML is used.
func TestARM64BrowserDownload(t *testing.T) {
	if os.Getenv("RITALIN_TEST_ARM64_BROWSER_DOWNLOAD") != "1" || runtime.GOOS != "linux" {
		t.Skip("set RITALIN_TEST_ARM64_BROWSER_DOWNLOAD=1 on Linux for the download smoke test")
	}
	b := launcher.NewBrowser()
	b.RootDir = t.TempDir()
	configureBrowserDownload(b, "linux", "arm64")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	b.Context = ctx
	tr := systemTransport()
	defer tr.CloseIdleConnections()
	b.HTTPClient = &http.Client{Transport: tr, Timeout: 10 * time.Minute}
	if err := b.Download(); err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(b.BinPath())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Machine != elf.EM_AARCH64 {
		t.Fatal("wrong architecture", f.Machine)
	}
	if runtime.GOARCH == "arm64" {
		out, err := exec.CommandContext(ctx, b.BinPath(), "--version").CombinedOutput()
		if err != nil {
			t.Fatalf("browser cannot start: %v: %s", err, out)
		}
		t.Log(strings.TrimSpace(string(out)))
	}
	t.Log("official ARM64 archive downloaded and extracted successfully")
}
