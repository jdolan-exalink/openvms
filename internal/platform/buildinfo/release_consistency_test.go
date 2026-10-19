package buildinfo_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The release versions live in two files: VERSION (OpenVMS) and deploy/agent/VERSION
// (edge agent). Everything else must follow them; this test fails when it does not.

var (
	semver          = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)
	changelogTop    = regexp.MustCompile(`(?m)^## \[(\d[^\]]*)\] - \d{4}-\d{2}-\d{2}\s*$`)
	hardcodedAgent  = regexp.MustCompile(`(?m)AGENT_VERSION(\??=|:-|=)\s*"?\$?\{?\d+\.\d+\.\d+`)
	hardcodedAgent2 = regexp.MustCompile(`\$\{AGENT_VERSION:-\d+\.\d+\.\d+\}`)
)

func repoFile(t *testing.T, rel ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", "..", ".."}, rel...)...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(rel...), err)
	}
	return string(b)
}

func versionFile(t *testing.T, rel ...string) string {
	t.Helper()
	raw := repoFile(t, rel...)
	v := strings.TrimSpace(raw)
	if !semver.MatchString(v) {
		t.Fatalf("%s = %q, want MAJOR.MINOR.PATCH", filepath.Join(rel...), v)
	}
	if raw != v+"\n" {
		t.Fatalf("%s must hold the version and one trailing newline only, got %q", filepath.Join(rel...), raw)
	}
	return v
}

func TestVersionFilesAreSemver(t *testing.T) {
	versionFile(t, "VERSION")
	versionFile(t, "deploy", "agent", "VERSION")
}

func TestChangelogTopReleaseMatchesVersion(t *testing.T) {
	want := versionFile(t, "VERSION")
	changelog := repoFile(t, "CHANGELOG.md")
	if !strings.Contains(changelog, "## [Unreleased]") {
		t.Fatal("CHANGELOG.md must keep an [Unreleased] section")
	}
	m := changelogTop.FindStringSubmatch(changelog)
	if m == nil {
		t.Fatal("CHANGELOG.md has no released entry like '## [X.Y.Z] - YYYY-MM-DD'")
	}
	if !semver.MatchString(m[1]) {
		t.Fatalf("top released changelog entry %q is not MAJOR.MINOR.PATCH", m[1])
	}
	if m[1] != want {
		t.Fatalf("top released changelog entry is %s, VERSION is %s", m[1], want)
	}
}

func TestBuildsDoNotHardcodeVersions(t *testing.T) {
	for _, rel := range [][]string{{"Makefile"}, {"docker-compose.yml"}, {"deploy", "docker", "go.Dockerfile"}, {"deploy", "docker", "web.Dockerfile"}, {"deploy", "deploy.sh"}, {"deploy", "install.sh"}} {
		text := repoFile(t, rel...)
		name := filepath.Join(rel...)
		if hardcodedAgent.MatchString(text) || hardcodedAgent2.MatchString(text) {
			t.Errorf("%s hard-codes an agent version; read deploy/agent/VERSION instead", name)
		}
		if regexp.MustCompile(`\$\{VERSION:-\d+\.\d+\.\d+\}`).MatchString(text) {
			t.Errorf("%s hard-codes the OpenVMS version; read VERSION instead", name)
		}
	}
}

func TestBuildsReadVersionFiles(t *testing.T) {
	checks := []struct {
		file string
		rel  []string
		want []string
	}{
		{"Makefile", []string{"Makefile"}, []string{"VERSION ?= $(shell cat VERSION", "AGENT_VERSION ?= $(shell cat deploy/agent/VERSION"}},
		{"deploy.sh", []string{"deploy", "deploy.sh"}, []string{"< VERSION"}},
		{"install.sh", []string{"deploy", "install.sh"}, []string{"< VERSION"}},
		{"go.Dockerfile", []string{"deploy", "docker", "go.Dockerfile"}, []string{"COPY VERSION", "deploy/agent/VERSION"}},
		{"web.Dockerfile", []string{"deploy", "docker", "web.Dockerfile"}, []string{"COPY VERSION"}},
	}
	for _, c := range checks {
		text := repoFile(t, c.rel...)
		for _, want := range c.want {
			if !strings.Contains(text, want) {
				t.Errorf("%s must contain %q", c.file, want)
			}
		}
	}
	// Compose must hand empty (not "dev") defaults to the Dockerfiles so they read the files.
	compose := repoFile(t, "docker-compose.yml")
	if strings.Contains(compose, `${VERSION:-dev}`) {
		t.Error(`docker-compose.yml must default VERSION to empty, not "dev"`)
	}
}

func TestDockerfilesFallBackToVersionFilesWhenArgsEmptyOrDev(t *testing.T) {
	text := repoFile(t, "deploy", "docker", "go.Dockerfile")
	if !strings.Contains(text, `"dev"`) || !strings.Contains(text, `[ -z`) {
		t.Error("go.Dockerfile must read VERSION files when the build args are empty or dev")
	}
	web := repoFile(t, "deploy", "docker", "web.Dockerfile")
	if !strings.Contains(web, `"dev"`) || !strings.Contains(web, `[ -z`) {
		t.Error("web.Dockerfile must read VERSION when the build arg is empty or dev")
	}
}
