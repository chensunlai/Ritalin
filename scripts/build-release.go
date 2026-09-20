//go:build ignore

// Developer-only release packager. End users install prebuilt release assets.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func must(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func envSet(env []string, k, v string) []string {
	out := []string{}
	for _, e := range env {
		if !strings.HasPrefix(e, k+"=") {
			out = append(out, e)
		}
	}
	return append(out, k+"="+v)
}
func thirdParty() []byte {
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	raw, e := cmd.Output()
	must(e)
	dec := json.NewDecoder(bytes.NewReader(raw))
	var out bytes.Buffer
	for {
		var m struct {
			Path, Version, Dir string
			Main               bool
		}
		e = dec.Decode(&m)
		if e == io.EOF {
			break
		}
		must(e)
		if m.Main || m.Dir == "" {
			continue
		}
		var files []string
		entries, e := os.ReadDir(m.Dir)
		must(e)
		for _, f := range entries {
			name := strings.ToUpper(f.Name())
			if !f.IsDir() && (strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "COPYING") || name == "NOTICE") {
				files = append(files, f.Name())
			}
		}
		if len(files) == 0 {
			// Some small modules declare their license only in README.md.
			if b, err := os.ReadFile(filepath.Join(m.Dir, "README.md")); err == nil && bytes.Contains(bytes.ToLower(b), []byte("license")) {
				files = append(files, "README.md")
			} else {
				fmt.Fprintf(os.Stderr, "Missing license information: %s\n", m.Path)
				os.Exit(1)
			}
		}
		sort.Strings(files)
		fmt.Fprintf(&out, "\n========== %s %s ==========\n", m.Path, m.Version)
		for _, name := range files {
			b, e := os.ReadFile(filepath.Join(m.Dir, name))
			must(e)
			fmt.Fprintf(&out, "\n--- %s ---\n%s\n", name, b)
		}
	}
	root, e := exec.Command("go", "env", "GOROOT").Output()
	must(e)
	b, e := os.ReadFile(filepath.Join(strings.TrimSpace(string(root)), "LICENSE"))
	must(e)
	fmt.Fprintf(&out, "\n========== Go runtime and standard library ==========\n%s\n", b)
	return out.Bytes()
}

type item struct {
	name string
	data []byte
	mode int64
}

func archive(path string, items []item, windows bool) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	if windows {
		z := zip.NewWriter(f)
		for _, i := range items {
			h := &zip.FileHeader{Name: i.name, Method: zip.Deflate}
			h.SetMode(os.FileMode(i.mode))
			w, e := z.CreateHeader(h)
			if e != nil {
				return e
			}
			if _, e = w.Write(i.data); e != nil {
				return e
			}
		}
		return z.Close()
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, i := range items {
		e = tw.WriteHeader(&tar.Header{Name: i.name, Size: int64(len(i.data)), Mode: i.mode, ModTime: time.Unix(0, 0)})
		if e != nil {
			return e
		}
		if _, e = tw.Write(i.data); e != nil {
			return e
		}
	}
	if e = tw.Close(); e != nil {
		return e
	}
	return gz.Close()
}
func main() {
	if len(os.Args) != 2 || !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(os.Args[1]) {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/build-release.go v0.1.0")
		os.Exit(2)
	}
	dest := filepath.Join("dist", os.Args[1])
	if override := os.Getenv("RITALIN_RELEASE_DIR"); override != "" {
		dest = override
	}
	if _, e := os.Stat(dest); e == nil {
		fmt.Fprintln(os.Stderr, "output directory exists; refusing to overwrite release artifacts")
		os.Exit(1)
	}
	must(os.MkdirAll(dest, 0755))
	notices := thirdParty()
	common := []item{{"THIRD_PARTY_NOTICES.txt", notices, 0644}}
	for _, name := range []string{"README.md", "LICENSE", "NOTICE"} {
		b, e := os.ReadFile(name)
		must(e)
		common = append(common, item{name, b, 0644})
	}
	matrix := [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"linux", "386"}, {"linux", "armv7"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}, {"windows", "386"}}
	temp, e := os.MkdirTemp("", "ritalin-build-")
	must(e)
	defer os.RemoveAll(temp)
	for _, target := range matrix {
		platform, arch := target[0], target[1]
		goarch := arch
		if arch == "armv7" {
			goarch = "arm"
		}
		name := "codex-ritalin"
		ext := ".tar.gz"
		if platform == "windows" {
			name += ".exe"
			ext = ".zip"
		}
		fmt.Printf("Building %s/%s\n", platform, arch)
		bin := filepath.Join(temp, name)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", bin, ".")
		env := envSet(os.Environ(), "CGO_ENABLED", "0")
		env = envSet(env, "GOOS", platform)
		env = envSet(env, "GOARCH", goarch)
		env = envSet(env, "GOARM", "7")
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		must(cmd.Run())
		b, e := os.ReadFile(bin)
		must(e)
		items := append([]item{{name, b, 0755}}, common...)
		path := filepath.Join(dest, "codex-ritalin_"+platform+"_"+arch+ext)
		must(archive(path, items, platform == "windows"))
		fmt.Println(path)
	}
	for _, name := range []string{"install.sh", "install.ps1"} {
		b, e := os.ReadFile(name)
		must(e)
		must(os.WriteFile(filepath.Join(dest, name), b, 0644))
	}
}
