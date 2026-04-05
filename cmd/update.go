package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/itsmostafa/qi/internal/version"
	"github.com/spf13/cobra"
)

const githubRepo = "itsmostafa/qi"

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update qi to the latest release from GitHub",
	RunE:  runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	release, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("fetching latest release: %w", err)
	}

	current := version.Version
	latest := release.TagName
	if current == latest {
		fmt.Printf("Already up to date (%s).\n", current)
		return nil
	}
	fmt.Printf("Updating %s → %s\n", current, latest)

	assetName := fmt.Sprintf("qi-%s-%s", runtime.GOOS, runtime.GOARCH)
	binaryURL, err := findAssetURL(release.Assets, assetName)
	if err != nil {
		return fmt.Errorf("finding asset %q: %w", assetName, err)
	}
	sumsURL, err := findAssetURL(release.Assets, "SHA256SUMS.txt")
	if err != nil {
		return fmt.Errorf("finding SHA256SUMS.txt: %w", err)
	}

	expectedHash, err := fetchExpectedChecksum(sumsURL, assetName)
	if err != nil {
		return fmt.Errorf("fetching checksums: %w", err)
	}

	tmp, err := downloadToTemp(binaryURL)
	if err != nil {
		return fmt.Errorf("downloading binary: %w", err)
	}
	defer os.Remove(tmp)

	if err := verifyChecksum(tmp, expectedHash); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding current executable: %w", err)
	}
	if err := replaceExecutable(exe, tmp); err != nil {
		return fmt.Errorf("replacing executable: %w", err)
	}

	fmt.Printf("Updated to %s. Run `qi version` to confirm.\n", latest)
	return nil
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func fetchLatestRelease() (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	var rel githubRelease
	return &rel, json.NewDecoder(resp.Body).Decode(&rel)
}

func findAssetURL(assets []githubAsset, name string) (string, error) {
	for _, a := range assets {
		if a.Name == name {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("asset %q not found in release", name)
}

func fetchExpectedChecksum(sumsURL, assetName string) (string, error) {
	resp, err := http.Get(sumsURL) //nolint:noctx
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum found for %q", assetName)
}

func downloadToTemp(url string) (string, error) {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}
	f, err := os.CreateTemp("", "qi-update-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func verifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, expected)
	}
	return nil
}

func replaceExecutable(dest, src string) error {
	if err := os.Chmod(src, 0755); err != nil {
		return err
	}
	// Try atomic rename first (works when on the same filesystem)
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	// Cross-device fallback: copy into a sibling temp file then rename
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".new"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	return os.Rename(tmp, dest)
}
