package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/config"
	"github.com/spf13/cobra"
)

// hookMarker identifies a hook file qi wrote, so a reinstall replaces it and a
// hook anyone else wrote is never overwritten.
const hookMarker = "# Installed by `qi hook install`"

// pullHooks run after `git pull` (post-merge) and `git pull --rebase`
// (post-rewrite). Both leave the pre-pull commit in ORIG_HEAD.
var pullHooks = []string{"post-merge", "post-rewrite"}

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Manage git hooks that keep a collection indexed",
}

var hookInstallCmd = &cobra.Command{
	Use:   "install [path|collection]",
	Short: "Reindex a collection after every git pull",
	Args:  cobra.MaximumNArgs(1),
	Long: `Install post-merge and post-rewrite hooks in the git repository that holds
a collection. After a pull, the hooks run ` + "`qi index --changed-since ORIG_HEAD`" + ` in the
background, which returns immediately unless the pull changed a file the
collection indexes. Output is appended to qi-index.log in the git directory.

The argument is resolved like ` + "`qi index`" + `: a path, a collection name, or the current
directory. The hooks directory comes from git, so core.hooksPath is respected.
Running install again replaces qi's hooks; hooks written by anything else are
left alone.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		a := &app.App{Config: cfg}
		known := len(cfg.Collections)
		col, err := resolveCollection(a, args)
		if err != nil {
			return err
		}

		hooksDir, err := gitPath(col.Path, "--git-path", "hooks")
		if err != nil {
			return fmt.Errorf("%s is not in a git repository: %w", col.Path, err)
		}
		gitDir, err := gitPath(col.Path, "--absolute-git-dir")
		if err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locating the qi executable: %w", err)
		}
		cfgPath := ""
		if cfgFile != "" {
			if cfgPath, err = filepath.Abs(cfgFile); err != nil {
				return err
			}
		}
		exe = filepath.Clean(exe)
		logPath := filepath.Join(gitDir, "qi-index.log")
		script := hookScript(exe, cfgPath, col.Path, logPath)

		// Check every hook before writing any, so a refusal leaves nothing
		// half-installed.
		for _, name := range pullHooks {
			existing, err := os.ReadFile(filepath.Join(hooksDir, name))
			if err == nil && !bytes.Contains(existing, []byte(hookMarker)) {
				return fmt.Errorf("%s already exists and was not written by qi; add this line to it instead:\n%s",
					filepath.Join(hooksDir, name), hookCommand(exe, cfgPath, col.Path, logPath))
			}
			if err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			return fmt.Errorf("creating hooks directory: %w", err)
		}
		for _, name := range pullHooks {
			path := filepath.Join(hooksDir, name)
			if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
				return fmt.Errorf("writing %s: %w", path, err)
			}
			// WriteFile keeps an existing file's mode.
			if err := os.Chmod(path, 0o755); err != nil {
				return err
			}
			fmt.Printf("Installed %s\n", path)
		}
		fmt.Printf("Pulls that change files in %q now reindex it in the background; output goes to %s\n",
			col.Name, logPath)
		if len(a.Config.Collections) > known {
			fmt.Printf("Run `qi index %s` once to build the initial index.\n", col.Path)
		}
		return nil
	},
}

// gitPath runs `git rev-parse <args>` in dir and returns the path it prints,
// made absolute: --git-path answers relative to dir.
func gitPath(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir, "rev-parse"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return path, nil
}

func hookScript(exe, cfgPath, colPath, logPath string) string {
	return "#!/bin/sh\n" +
		hookMarker + ": reindex qi in the background after a pull.\n" +
		"[ \"$1\" = amend ] && exit 0  # post-rewrite also runs after commit --amend\n" +
		hookCommand(exe, cfgPath, colPath, logPath) + "\n"
}

// hookCommand is the line that does the work. Paths are absolute so the hook
// behaves the same from a GUI git client with a minimal PATH.
func hookCommand(exe, cfgPath, colPath, logPath string) string {
	args := []string{"nohup", shellQuote(exe)}
	if cfgPath != "" {
		args = append(args, "--config", shellQuote(cfgPath))
	}
	args = append(args, "index", "--changed-since", "ORIG_HEAD", shellQuote(colPath),
		"</dev/null", ">>"+shellQuote(logPath), "2>&1", "&")
	return strings.Join(args, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func init() {
	hookCmd.AddCommand(hookInstallCmd)
}
