package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/spf13/cobra"
)

// hookMarker identifies a hook file qi wrote, so a reinstall replaces it and a
// hook anyone else wrote is never overwritten. Hooks from older qi versions
// carry it too.
const hookMarker = "# Installed by `qi hook install`"

// gitHooks are the hooks qi installs. post-merge fires after `git pull` and
// after `git pull --rebase` fast-forwards; post-rewrite after a rebasing pull
// that replays local commits; post-commit after the commit that concludes a
// pull whose merge conflicted, where post-merge never fires.
var gitHooks = []string{"post-merge", "post-rewrite", "post-commit"}

// hookLogName is the file, in the git directory, that hook runs append to.
const hookLogName = "qi-index.log"

// gitLocationVars are the variables git exports to a hook that pin the
// repository, worktree and index. Left in place they make `git -C <dir>` use
// the hook's repository with <dir> as its worktree root, or fail outright
// when GIT_DIR is relative.
var gitLocationVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
	"GIT_PREFIX", "GIT_IMPLICIT_WORK_TREE", "GIT_SHALLOW_FILE", "GIT_GRAFT_FILE",
}

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Manage git hooks that keep collections indexed",
}

var hookInstallCmd = &cobra.Command{
	Use:   "install [path|collection]",
	Short: "Reindex a repository's collections after every git pull",
	Args:  cobra.MaximumNArgs(1),
	Long: `Install post-merge, post-rewrite and post-commit hooks in the git repository
that holds a collection, and register the collection if it is new.

After a pull (merge, rebase, squash, or a conflicted merge once it is committed)
the hooks start one background ` + "`qi index`" + ` for every configured collection inside
the worktree, restricted by --changed-since to runs where the pull changed a
file a collection indexes. Output is appended to qi-index.log in the git
directory. Ordinary commits and amends cost one git call and nothing more.

The hooks name no collection, so installing for a second collection in the same
repository rewrites the same hooks, and every collection in the worktree is
covered. The argument is resolved like ` + "`qi index`" + `: a path, a collection name, or
the current directory. The hooks directory comes from git, so core.hooksPath is
respected. Hooks written by anything else are left alone: install refuses and
prints the line to add to them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		// Opening the database migrates it, which a background index run's
		// write lock would otherwise fail.
		unlock, err := db.LockIndex(cfg.DatabasePath, func() {
			fmt.Printf("Waiting for a qi index run on %s to finish...\n", cfg.DatabasePath)
		})
		if err != nil {
			return err
		}
		defer unlock()
		// app.New moves a legacy collection name's rows before resolveCollection
		// can save the normalized name.
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		arg := "."
		if len(args) > 0 {
			arg = args[0]
		}
		target, err := previewCollection(a.Config, arg)
		if err != nil {
			return err
		}
		// Ask git about the real directory: a relative answer joined to a
		// symlinked path would follow ".." out of the link's parent.
		dir, err := config.CanonicalPath(target.Path)
		if err != nil {
			return err
		}
		out, err := gitOutput(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", "hooks", "--absolute-git-dir")
		if err != nil {
			return fmt.Errorf("%s is not in a git repository: %w", dir, err)
		}
		hooksDir, gitDir, _ := strings.Cut(out, "\n")
		exe, err := qiExecutable()
		if err != nil {
			return fmt.Errorf("locating the qi executable: %w", err)
		}
		cfgPath, err := absConfigPath()
		if err != nil {
			return err
		}

		// Check every hook before writing any, so a refusal leaves nothing
		// half-installed and config untouched.
		for _, name := range gitHooks {
			path := filepath.Join(hooksDir, name)
			existing, err := os.ReadFile(path)
			if err == nil && !bytes.Contains(existing, []byte(hookMarker)) {
				return fmt.Errorf("%s already exists and was not written by qi; add this line to it instead:\n%s",
					path, hookCommand(exe, cfgPath, name))
			}
			if err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			return fmt.Errorf("creating hooks directory: %w", err)
		}
		for _, name := range gitHooks {
			path := filepath.Join(hooksDir, name)
			if err := os.WriteFile(path, []byte(hookScript(exe, cfgPath, name)), 0o755); err != nil {
				return fmt.Errorf("writing %s: %w", path, err)
			}
			// WriteFile keeps an existing file's mode.
			if err := os.Chmod(path, 0o755); err != nil {
				return err
			}
			fmt.Printf("Installed %s\n", path)
		}

		known := len(a.Config.Collections)
		col, err := resolveCollection(a, args)
		if err != nil {
			return err
		}
		fmt.Printf("Pulls that change files in %q now reindex it in the background; output goes to %s\n",
			col.Name, filepath.Join(gitDir, hookLogName))
		if len(a.Config.Collections) > known {
			fmt.Printf("Run `qi index %s` once to build the initial index.\n", col.Path)
		}
		return nil
	},
}

var hookRunCmd = &cobra.Command{
	Use:    "run <hook> [args...]",
	Short:  "Run from a git hook installed by `qi hook install`",
	Hidden: true,
	Args:   cobra.ArbitraryArgs,
	// Runs inside git's hook: it never fails git and never prints, since git
	// shows hook output to the user. Problems go to the log.
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		repoDir, err := os.Getwd()
		if err != nil {
			return nil
		}
		hook := args[0]
		plan, err := planHookRun(context.Background(), hook, args[1:], repoDir, func() (*config.Config, error) {
			return config.Load(cfgFile)
		})
		if plan.GitDir == "" || (err == nil && len(plan.Paths) == 0) {
			return nil
		}
		logFile, logErr := os.OpenFile(filepath.Join(plan.GitDir, hookLogName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if logErr != nil {
			return nil // nowhere to report it
		}
		defer logFile.Close()
		if err != nil {
			hookLog(logFile, "%s: not reindexing: %v", hook, err)
			return nil
		}
		what := "a full qi index"
		if plan.Base != "" {
			what = "qi index --changed-since " + plan.Base
		}
		hookLog(logFile, "%s (%s): starting %s of %s", hook, plan.Why, what, strings.Join(plan.Paths, " "))
		if err := spawnIndex(plan, logFile); err != nil {
			hookLog(logFile, "%s: could not start qi index: %v", hook, err)
		}
		return nil
	},
}

// hookPlan is what one hook invocation should do. No Paths means nothing.
type hookPlan struct {
	GitDir   string   // absolute git directory, home of the log
	TopLevel string   // canonical worktree root
	Base     string   // commit to diff against; empty means a full index
	Why      string   // what the hook saw, for the log
	Paths    []string // canonical paths of the collections to index
}

// planHookRun decides, for git hook hook called with args in repoDir, which
// collections to reindex and against which base commit. It spawns nothing.
// Ordinary commits and amends return before any config is read: post-commit
// fires on every commit, so that path costs one git call at most.
func planHookRun(ctx context.Context, hook string, args []string, repoDir string, loadConfig func() (*config.Config, error)) (hookPlan, error) {
	var plan hookPlan
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	// Each case names the revision holding the pre-pull commit, or leaves it
	// empty for a full index.
	var baseRev string
	switch {
	case hook == "post-merge" && arg == "1":
		// A squash pull leaves HEAD where it was; nothing to diff against.
		plan.Why = "squash merge"
	case hook == "post-merge":
		baseRev, plan.Why = "ORIG_HEAD", "merge"
	case hook == "post-rewrite" && arg == "rebase":
		baseRev, plan.Why = "ORIG_HEAD", "rebase"
	case hook == "post-commit":
		// Only a merge commit concludes a pull: a conflicted merge fires no
		// post-merge, just this hook once the resolution is committed.
		if _, err := gitOutput(ctx, repoDir, "rev-parse", "-q", "--verify", "HEAD^2"); err != nil {
			return hookPlan{}, nil
		}
		baseRev, plan.Why = "HEAD^1", "merge commit"
	default:
		// post-rewrite after `commit --amend`, or a hook qi does not handle.
		return hookPlan{}, nil
	}

	out, err := gitOutput(ctx, repoDir, "rev-parse", "--absolute-git-dir", "--show-toplevel")
	if err != nil {
		return hookPlan{}, err
	}
	gitDir, top, _ := strings.Cut(out, "\n")
	plan.GitDir = gitDir
	if plan.TopLevel, err = config.CanonicalPath(top); err != nil {
		return plan, err
	}
	// Pin the base now: the background run starts later, and a quick second
	// pull or reset would have moved ORIG_HEAD by then.
	if baseRev != "" {
		sha, err := resolveRevision(ctx, repoDir, baseRev)
		if err != nil {
			plan.Why += ", no " + baseRev
		} else {
			plan.Base = sha
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		return plan, fmt.Errorf("loading config: %w", err)
	}
	for _, col := range cfg.Collections {
		p, err := config.CanonicalPath(col.Path)
		if err != nil {
			continue
		}
		if pathWithin(p, plan.TopLevel) && !slices.Contains(plan.Paths, p) {
			plan.Paths = append(plan.Paths, p)
		}
	}
	return plan, nil
}

// pathWithin reports whether path is dir or lies below it. Both are clean.
func pathWithin(path, dir string) bool {
	if path == dir {
		return true
	}
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	return strings.HasPrefix(path, dir)
}

// spawnIndex starts `qi index` for plan in the background and returns without
// waiting, so git finishes the pull while indexing runs.
func spawnIndex(plan hookPlan, logFile *os.File) error {
	exe, err := qiExecutable()
	if err != nil {
		return err
	}
	cfgPath, err := absConfigPath()
	if err != nil {
		return err
	}
	var args []string
	if cfgPath != "" {
		args = append(args, "--config", cfgPath)
	}
	args = append(args, "index")
	if plan.Base != "" {
		args = append(args, "--changed-since", plan.Base)
	}
	args = append(args, plan.Paths...)

	c := exec.Command(exe, args...)
	c.Dir = plan.TopLevel
	c.Stdout, c.Stderr = logFile, logFile // Stdin nil reads /dev/null.
	c.Env = gitEnv()
	detach(c)
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// hookLog appends one timestamped line to the hook log.
func hookLog(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "%s qi hook %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

// gitEnv is the environment minus git's repository-location variables, for
// running git against a directory of qi's choosing rather than the hook's.
func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(gitLocationVars, name) {
			env = append(env, kv)
		}
	}
	return env
}

// gitOutput runs git with args in dir and returns its trimmed output. Every
// call names its directory, so git's repository-location variables are always
// stripped: a hook exports them, and they would re-root `git -C`.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// qiExecutable is the path hooks call qi by. A `qi` on PATH that is this very
// binary wins over os.Executable, which may name a versioned location (a
// Homebrew Cellar, nix store or asdf install) that an upgrade removes.
func qiExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe = filepath.Clean(exe)
	if onPath, err := exec.LookPath("qi"); err == nil {
		if abs, err := filepath.Abs(onPath); err == nil {
			a, errA := os.Stat(abs)
			b, errB := os.Stat(exe)
			if errA == nil && errB == nil && os.SameFile(a, b) {
				return abs, nil
			}
		}
	}
	return exe, nil
}

// hookScript is a whole hook file. It names no collection: `qi hook run`
// finds the collections in the worktree when it fires. The guards keep qi from
// starting at all on the common no-op paths — post-commit fires on every
// commit — and planHookRun repeats them for a line pasted into another hook.
func hookScript(exe, cfgPath, hook string) string {
	guard := ""
	switch hook {
	case "post-commit":
		guard = "git rev-parse -q --verify HEAD^2 >/dev/null 2>&1 || exit 0  # not a merge commit\n"
	case "post-rewrite":
		guard = "[ \"$1\" = rebase ] || exit 0  # commit --amend\n"
	}
	return "#!/bin/sh\n" +
		hookMarker + ": reindex qi collections in this worktree after a pull.\n" +
		guard +
		"exec " + hookCommand(exe, cfgPath, hook) + "\n"
}

// absConfigPath is --config made absolute, so the hook and the background run
// read the same file wherever they start; empty means the default config.
func absConfigPath() (string, error) {
	if cfgFile == "" {
		return "", nil
	}
	return filepath.Abs(cfgFile)
}

// hookCommand is the line that does the work, also what to add to a hook qi
// did not write. Paths are absolute so the hook behaves the same from a GUI git
// client with a minimal PATH.
func hookCommand(exe, cfgPath, hook string) string {
	args := []string{shellQuote(exe)}
	if cfgPath != "" {
		args = append(args, "--config", shellQuote(cfgPath))
	}
	args = append(args, "hook", "run", hook, `"$@"`, "</dev/null")
	return strings.Join(args, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func init() {
	hookCmd.AddCommand(hookInstallCmd)
	hookCmd.AddCommand(hookRunCmd)
}
