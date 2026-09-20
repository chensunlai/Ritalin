package ritalin

import (
	"fmt"
	"path/filepath"

	"github.com/go-rod/rod/lib/launcher"
)

func configureBrowserDownload(b *launcher.Browser, goos, goarch string) {
	if goos != "linux" || goarch != "arm64" {
		return
	}
	// Rod's snapshot hosts have no Linux ARM64 mapping. Use the official
	// Playwright v1.55.1 Chromium build and mirrors instead.
	// https://github.com/microsoft/playwright/blob/v1.55.1/packages/playwright-core/browsers.json
	b.Revision = 1193
	b.RootDir = filepath.Join(b.RootDir, "linux-arm64")
	b.Hosts = nil
	for _, mirror := range []string{
		"https://cdn.playwright.dev/dbazure/download/playwright",
		"https://playwright.download.prss.microsoft.com/dbazure/download/playwright",
		"https://cdn.playwright.dev",
	} {
		b.Hosts = append(b.Hosts, func(revision int) string {
			return fmt.Sprintf("%s/builds/chromium/%d/chromium-linux-arm64.zip", mirror, revision)
		})
	}
}
