package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

// gitRepo creates a repository with one commit holding files, and returns its
// path. Tests skip when git is not installed.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	for name, body := range files {
		writeRepoFile(t, dir, name, body)
	}
	commitAll(t, dir)
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeRepoFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir string) string {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "change")
	return runGit(t, dir, "rev-parse", "HEAD")
}

func TestDocsChangedSince(t *testing.T) {
	ctx := context.Background()
	repo := gitRepo(t, map[string]string{
		"main.go":        "package main\n",
		"docs/guide.md":  "# Guide\n",
		"docs/intro.md":  "# Intro\n",
		"other/notes.md": "# Notes\n",
	})
	col := config.Collection{Name: "docs", Path: filepath.Join(repo, "docs")}

	cases := []struct {
		name   string
		change func()
		want   bool
	}{
		{"code only", func() { writeRepoFile(t, repo, "main.go", "package main\n// edit\n") }, false},
		{"doc outside the collection", func() { writeRepoFile(t, repo, "other/notes.md", "# Notes\nedit\n") }, false},
		{"doc edited", func() { writeRepoFile(t, repo, "docs/guide.md", "# Guide\nedit\n") }, true},
		{"doc deleted", func() { runGit(t, repo, "rm", "-q", "docs/guide.md") }, true},
		{"unindexed extension", func() { writeRepoFile(t, repo, "docs/page.rst", "Page\n====\n") }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := runGit(t, repo, "rev-parse", "HEAD")
			tc.change()
			commitAll(t, repo)
			got, err := docsChangedSince(ctx, col, before)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("docsChangedSince = %v, want %v", got, tc.want)
			}
		})
	}

	// A configured extension replaces the defaults, and matching ignores case.
	before := runGit(t, repo, "rev-parse", "HEAD")
	writeRepoFile(t, repo, "docs/Setup.RST", "Setup\n=====\n")
	commitAll(t, repo)
	rstCol := col
	rstCol.Extensions = []string{".rst"}
	if got, err := docsChangedSince(ctx, rstCol, before); err != nil || !got {
		t.Fatalf("configured .rst change: got %v, %v; want true", got, err)
	}

	for _, rev := range []string{"no-such-rev", "--output=/tmp/x"} {
		if _, err := docsChangedSince(ctx, col, rev); err == nil {
			t.Fatalf("revision %q: expected an error so the caller indexes everything", rev)
		}
	}
}

func TestHookInstall(t *testing.T) {
	repo := gitRepo(t, map[string]string{"README.md": "# Repo\n"})
	canonicalRepo, err := config.CanonicalPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := writeIndexTestConfig(t, "collections: []\n")
	withIndexTestConfig(t, cfgPath)

	install := func(args ...string) error {
		var err error
		captureIndexTestOutput(t, func() { err = hookInstallCmd.RunE(hookInstallCmd, args) })
		return err
	}

	if err := install(repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, name := range pullHooks {
		path := filepath.Join(repo, ".git", "hooks", name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s not installed: %v", name, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("%s is not executable: %v", name, info.Mode())
		}
		body, _ := os.ReadFile(path)
		for _, want := range []string{hookMarker, "index --changed-since ORIG_HEAD", shellQuote(canonicalRepo), "--config " + shellQuote(cfgPath)} {
			if !strings.Contains(string(body), want) {
				t.Fatalf("%s lacks %q:\n%s", name, want, body)
			}
		}
	}

	// qi's own hooks are replaced on reinstall.
	if err := install(repo); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	// A hook qi did not write blocks the install, and nothing is written.
	other := gitRepo(t, map[string]string{"README.md": "# Other\n"})
	foreign := filepath.Join(other, ".git", "hooks", "post-rewrite")
	writeRepoFile(t, other, filepath.Join(".git", "hooks", "post-rewrite"), "#!/bin/sh\necho mine\n")
	if err := install(other); err == nil || !strings.Contains(err.Error(), "not written by qi") {
		t.Fatalf("expected refusal for a foreign hook, got %v", err)
	}
	if body, _ := os.ReadFile(foreign); string(body) != "#!/bin/sh\necho mine\n" {
		t.Fatalf("foreign hook was modified: %q", body)
	}
	if _, err := os.Stat(filepath.Join(other, ".git", "hooks", "post-merge")); !os.IsNotExist(err) {
		t.Fatalf("post-merge written despite refusal: %v", err)
	}

	// core.hooksPath is where git looks, so it is where the hooks go.
	runGit(t, other, "config", "core.hooksPath", ".githooks")
	if err := install(other); err != nil {
		t.Fatalf("install with core.hooksPath: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, ".githooks", "post-merge")); err != nil {
		t.Fatalf("hook not installed under core.hooksPath: %v", err)
	}
}

func TestHookInstallOutsideGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	withIndexTestConfig(t, writeIndexTestConfig(t, "collections: []\n"))
	var err error
	captureIndexTestOutput(t, func() { err = hookInstallCmd.RunE(hookInstallCmd, []string{t.TempDir()}) })
	if err == nil || !strings.Contains(err.Error(), "not in a git repository") {
		t.Fatalf("expected a not-a-repository error, got %v", err)
	}
}
