package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

// gitRepo creates a repository with one commit holding files, and returns its
// path. Tests skip when git is not installed.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	for name, body := range files {
		writeRepoFile(t, dir, name, body)
	}
	commitAll(t, dir)
	return dir
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
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

// hookTestConfig writes a config whose database lives in a temp dir, points
// the commands at it, and returns its path.
func hookTestConfig(t *testing.T, collections string) string {
	t.Helper()
	cfgPath := writeIndexTestConfig(t, "database_path: qi.db\n"+collections)
	withIndexTestConfig(t, cfgPath)
	return cfgPath
}

func installHooks(t *testing.T, args ...string) error {
	t.Helper()
	var err error
	captureIndexTestOutput(t, func() { err = hookInstallCmd.RunE(hookInstallCmd, args) })
	return err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestHookInstall(t *testing.T) {
	repo := gitRepo(t, map[string]string{"README.md": "# Repo\n", "docs/a.md": "# A\n"})
	cfgPath := hookTestConfig(t, "collections: []\n")

	if err := installHooks(t, repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	hooks := map[string]string{}
	for _, name := range gitHooks {
		path := filepath.Join(repo, ".git", "hooks", name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s not installed: %v", name, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("%s is not executable: %v", name, info.Mode())
		}
		body := readFile(t, path)
		for _, want := range []string{hookMarker, "exec ", "--config " + shellQuote(cfgPath), "hook run " + name + ` "$@" </dev/null`} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s lacks %q:\n%s", name, want, body)
			}
		}
		if strings.Contains(body, repo) {
			t.Fatalf("%s names a collection; hooks must cover every collection in the repo:\n%s", name, body)
		}
		hooks[name] = body
	}

	// Installing for a second collection in the same repository rewrites the
	// same hooks and registers that collection too.
	if err := installHooks(t, filepath.Join(repo, "docs")); err != nil {
		t.Fatalf("install second collection: %v", err)
	}
	for name, body := range hooks {
		if got := readFile(t, filepath.Join(repo, ".git", "hooks", name)); got != body {
			t.Fatalf("%s changed on reinstall:\n%s\nwant:\n%s", name, got, body)
		}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Collections) != 2 {
		t.Fatalf("want both collections registered, got %+v", cfg.Collections)
	}

	// A hook from an older qi carries the marker and is replaced.
	old := filepath.Join(repo, ".git", "hooks", "post-merge")
	writeRepoFile(t, repo, filepath.Join(".git", "hooks", "post-merge"), "#!/bin/sh\n"+hookMarker+": old\nnohup qi index &\n")
	if err := installHooks(t, repo); err != nil {
		t.Fatalf("reinstall over an old qi hook: %v", err)
	}
	if got := readFile(t, old); got != hooks["post-merge"] {
		t.Fatalf("old qi hook not replaced:\n%s", got)
	}

	// core.hooksPath is where git looks, so it is where the hooks go.
	runGit(t, repo, "config", "core.hooksPath", ".githooks")
	if err := installHooks(t, repo); err != nil {
		t.Fatalf("install with core.hooksPath: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".githooks", "post-commit")); err != nil {
		t.Fatalf("hook not installed under core.hooksPath: %v", err)
	}
}

// A refused install writes no hook and registers no collection.
func TestHookInstallRefusesForeignHook(t *testing.T) {
	repo := gitRepo(t, map[string]string{"README.md": "# Repo\n"})
	cfgPath := hookTestConfig(t, "collections: []\n")
	before := readFile(t, cfgPath)

	foreign := filepath.Join(repo, ".git", "hooks", "post-commit")
	writeRepoFile(t, repo, filepath.Join(".git", "hooks", "post-commit"), "#!/bin/sh\necho mine\n")
	err := installHooks(t, repo)
	if err == nil || !strings.Contains(err.Error(), "not written by qi") {
		t.Fatalf("expected refusal for a foreign hook, got %v", err)
	}
	// The line to add must not exec, or the rest of their hook never runs.
	if !strings.Contains(err.Error(), "hook run post-commit") || strings.Contains(err.Error(), "exec ") {
		t.Fatalf("refusal should print a plain qi line to add, got %v", err)
	}
	if got := readFile(t, foreign); got != "#!/bin/sh\necho mine\n" {
		t.Fatalf("foreign hook was modified: %q", got)
	}
	for _, name := range []string{"post-merge", "post-rewrite"} {
		if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", name)); !os.IsNotExist(err) {
			t.Fatalf("%s written despite refusal: %v", name, err)
		}
	}
	if got := readFile(t, cfgPath); got != before {
		t.Fatalf("config changed by a refused install:\n%s", got)
	}
}

func TestHookInstallOutsideGitRepo(t *testing.T) {
	requireGit(t)
	cfgPath := hookTestConfig(t, "collections: []\n")
	before := readFile(t, cfgPath)
	err := installHooks(t, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not in a git repository") {
		t.Fatalf("expected a not-a-repository error, got %v", err)
	}
	if got := readFile(t, cfgPath); got != before {
		t.Fatalf("config changed by a failed install:\n%s", got)
	}
}

// A collection reached through a symlink gets hooks in the repository the
// link points into, not in a directory beside the link.
func TestHookInstallSymlinkedCollection(t *testing.T) {
	repo := gitRepo(t, map[string]string{"docs/guide/a.md": "# A\n"})
	hookTestConfig(t, "collections: []\n")
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "guide")
	if err := os.Symlink(filepath.Join(repo, "docs", "guide"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := installHooks(t, link); err != nil {
		t.Fatalf("install through symlink: %v", err)
	}
	for _, name := range gitHooks {
		if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", name)); err != nil {
			t.Fatalf("%s not in the real hooks dir: %v", name, err)
		}
	}
	entries, _ := os.ReadDir(linkDir)
	if len(entries) != 1 {
		t.Fatalf("install wrote beside the symlink: %v", entries)
	}
}

func TestPlanHookRun(t *testing.T) {
	repo := gitRepo(t, map[string]string{
		"docs/a.md":     "# A\n",
		"handbook/b.md": "# B\n",
		"main.go":       "package main\n",
	})
	canonicalRepo, err := config.CanonicalPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	// A sibling whose path merely starts with the repo's must not match.
	sibling := repo + "-notes"
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sibling) })
	cfg := &config.Config{Collections: []config.Collection{
		{Name: "docs", Path: filepath.Join(repo, "docs")},
		{Name: "handbook", Path: filepath.Join(repo, "handbook")},
		{Name: "elsewhere", Path: t.TempDir()},
		{Name: "sibling", Path: sibling},
		// Deleted by a pull: skipped, or it would fail the other collections' run.
		{Name: "removed", Path: filepath.Join(repo, "removed")},
	}}
	wantPaths := []string{filepath.Join(canonicalRepo, "docs"), filepath.Join(canonicalRepo, "handbook")}
	load := func() (*config.Config, error) { return cfg, nil }
	noLoad := func() (*config.Config, error) {
		t.Fatal("config loaded for a hook that has nothing to do")
		return nil, nil
	}
	plan := func(hook string, load func() (*config.Config, error), args ...string) hookPlan {
		t.Helper()
		p, err := planHookRun(context.Background(), hook, args, repo, load)
		if err != nil {
			t.Fatalf("%s %v: %v", hook, args, err)
		}
		return p
	}

	// Ordinary commits and amends do nothing and read no config.
	for _, p := range []hookPlan{plan("post-rewrite", noLoad, "amend"), plan("post-commit", noLoad), plan("pre-push", noLoad)} {
		if len(p.Paths) != 0 {
			t.Fatalf("expected nothing to do, got %+v", p)
		}
	}

	// A clone that never pulled has no ORIG_HEAD: full index.
	p := plan("post-merge", load, "0")
	if p.Base != "" || !strings.Contains(p.Why, "no ORIG_HEAD") {
		t.Fatalf("no ORIG_HEAD: got %+v, want a full index", p)
	}
	if !slices.Equal(p.Paths, wantPaths) {
		t.Fatalf("paths = %v, want %v", p.Paths, wantPaths)
	}
	if p.GitDir != filepath.Join(canonicalRepo, ".git") && p.GitDir != filepath.Join(repo, ".git") {
		t.Fatalf("git dir = %q", p.GitDir)
	}

	// Merge and rebase pin ORIG_HEAD to a SHA; squash indexes everything.
	first := runGit(t, repo, "rev-parse", "HEAD")
	writeRepoFile(t, repo, "docs/a.md", "# A\nedit\n")
	commitAll(t, repo)
	runGit(t, repo, "update-ref", "ORIG_HEAD", first)
	if p := plan("post-merge", load, "0"); p.Base != first {
		t.Fatalf("post-merge base = %q, want %q", p.Base, first)
	}
	if p := plan("post-rewrite", load, "rebase"); p.Base != first {
		t.Fatalf("post-rewrite base = %q, want %q", p.Base, first)
	}
	if p := plan("post-merge", load, "1"); p.Base != "" || len(p.Paths) != 2 {
		t.Fatalf("squash: got %+v, want a full index of both collections", p)
	}

	// A merge commit (a conflicted pull, once resolved) diffs against its
	// first parent.
	runGit(t, repo, "checkout", "-q", "-b", "side", first)
	writeRepoFile(t, repo, "handbook/c.md", "# C\n")
	commitAll(t, repo)
	runGit(t, repo, "checkout", "-q", "-")
	mainHead := runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "merge", "-q", "--no-ff", "--no-edit", "side")
	if p := plan("post-commit", load); p.Base != mainHead || len(p.Paths) != 2 {
		t.Fatalf("merge commit: got %+v, want base %s", p, mainHead)
	}

	// A repository holding no collection has nothing to do.
	other := gitRepo(t, map[string]string{"x.md": "# X\n"})
	p, err = planHookRun(context.Background(), "post-merge", []string{"1"}, other, load)
	if err != nil || len(p.Paths) != 0 {
		t.Fatalf("repo without collections: got %+v, %v", p, err)
	}
}

// Inside a hook git exports GIT_DIR; the diff must still be scoped to the
// collection, not widened to the whole repository.
func TestDocsChangedSinceIgnoresHookEnvironment(t *testing.T) {
	repo := gitRepo(t, map[string]string{"docs/a.md": "# A\n", "other/b.md": "# B\n"})
	before := runGit(t, repo, "rev-parse", "HEAD")
	writeRepoFile(t, repo, "other/b.md", "# B\nedit\n")
	commitAll(t, repo)

	t.Setenv("GIT_DIR", filepath.Join(repo, ".git"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(repo, ".git", "index"))
	col := config.Collection{Name: "docs", Path: filepath.Join(repo, "docs")}
	got, err := docsChangedSince(context.Background(), col, before)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("a change outside the collection counted because GIT_DIR widened the diff")
	}
}

// A submodule bump is one extensionless gitlink path in the diff; the
// documents it moves must still count as a change.
func TestDocsChangedSinceSubmoduleBump(t *testing.T) {
	sub := gitRepo(t, map[string]string{"guide.md": "# Guide\n"})
	super := gitRepo(t, map[string]string{"README.md": "# Super\n"})
	runGit(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "docs/sub")
	commitAll(t, super)
	before := runGit(t, super, "rev-parse", "HEAD")

	checkout := filepath.Join(super, "docs", "sub")
	writeRepoFile(t, checkout, "guide.md", "# Guide\nedit\n")
	commitAll(t, checkout)
	runGit(t, super, "add", "docs/sub")
	commitAll(t, super)

	col := config.Collection{Name: "docs", Path: filepath.Join(super, "docs")}
	got, err := docsChangedSince(context.Background(), col, before)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("submodule bump under the collection was not treated as a change")
	}
}

// After a fresh clone ORIG_HEAD does not exist; that must read as "full index",
// not as git failing.
func TestResolveRevision(t *testing.T) {
	ctx := context.Background()
	repo := gitRepo(t, map[string]string{"a.md": "# A\n"})

	if _, err := resolveRevision(ctx, repo, "ORIG_HEAD"); !errors.Is(err, errUnknownRevision) {
		t.Fatalf("ORIG_HEAD in a fresh repo: got %v, want errUnknownRevision", err)
	}
	head := runGit(t, repo, "rev-parse", "HEAD")
	if got, err := resolveRevision(ctx, repo, "HEAD"); err != nil || got != head {
		t.Fatalf("HEAD: got %q, %v; want %q", got, err, head)
	}
	if _, err := resolveRevision(ctx, t.TempDir(), "HEAD"); err == nil || errors.Is(err, errUnknownRevision) {
		t.Fatalf("outside a repository: got %v, want a git error", err)
	}
	if _, err := resolveRevision(ctx, repo, "--output=/tmp/x"); err == nil {
		t.Fatal("an option-looking revision must be refused")
	}
}
