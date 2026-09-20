package ritalin

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestStartupShowsSettingsBeforeOneSecondPause(t *testing.T) {
	for _, lang := range []string{"zh", "en"} {
		m := dashboardFixture(t)
		m.c.Language = lang
		m.c.Active, m.c.Replace = "state-one", true
		m.c.Upstream = "http://secret-user:secret-pass@localhost:8080/secret-path?key=secret-key"
		var output bytes.Buffer
		paused := false
		startup(&output, m.store, m.c, func(d time.Duration) {
			paused = true
			if d != time.Second || !strings.Contains(output.String(), "Ritalin") {
				t.Fatal("banner must precede one-second pause")
			}
		})
		if !paused {
			t.Fatal("no startup delay")
		}
		for _, want := range []string{"Tokyo", "state-one", "CODEX_HOME", "http://localhost:8080", m.c.Command[0]} {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("missing %s", want)
			}
		}
		for _, secret := range []string{syntheticState(), "secret-user", "secret-pass", "secret-path", "secret-key"} {
			if strings.Contains(output.String(), secret) {
				t.Fatal("secret in banner")
			}
		}
	}
}
