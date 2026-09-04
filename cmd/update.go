package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/itsmostafa/qi/internal/version"
	"github.com/spf13/cobra"
)

const githubRepo = "itsmostafa/qi"

const (
	// One deadline covers connect, headers and body: a stalled mirror must not
	// hang `qi update` forever.
	updateTimeout = 5 * time.Minute
	// Release JSON and SHA256SUMS.txt are a few KB; the archive is one
	// compressed binary. Both caps are generous by orders of magnitude.
	maxMetadataBytes = 1 << 20
	maxArchiveBytes  = 100 << 20
)

var updateClient = &http.Client{Timeout: updateTimeout}

// httpGet fetches url with the command's context and a bounded client, and
// rejects non-200 responses so no caller parses an error page as data.
func httpGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return resp, nil
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update qi to the latest release from GitHub",
	RunE:  runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding current executable: %w", err)
	}
	ctx := cmd.Context()
	release, err := fetchLatestRelease(ctx)
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

	archiveName := fmt.Sprintf("qi-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archiveURL, err := findAssetURL(release.Assets, archiveName)
	if err != nil {
		return fmt.Errorf("finding asset %q: %w", archiveName, err)
	}
	sumsURL, err := findAssetURL(release.Assets, "SHA256SUMS.txt")
	if err != nil {
		return fmt.Errorf("finding SHA256SUMS.txt: %w", err)
	}

	expectedHash, err := fetchExpectedChecksum(ctx, sumsURL, archiveName)
	if err != nil {
		return fmt.Errorf("fetching checksums: %w", err)
	}

	tmp, err := downloadToTemp(ctx, archiveURL)
	if err != nil {
		return fmt.Errorf("downloading archive: %w", err)
	}
	defer os.Remove(tmp)

	if err := verifyChecksum(tmp, expectedHash); err != nil {
		return err
	}

	extracted, err := extractBinaryFromTar(tmp, "qi")
	if err != nil {
		return fmt.Errorf("extracting binary: %w", err)
	}
	defer os.Remove(extracted)

	if err := replaceExecutable(exe, extracted); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("%s is not writable, re-run with: sudo qi update", filepath.Dir(exe))
		}
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

func fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	resp, err := httpGet(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var rel githubRelease
	return &rel, json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&rel)
}

func findAssetURL(assets []githubAsset, name string) (string, error) {
	for _, a := range assets {
		if a.Name == name {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("asset %q not found in release", name)
}

func fetchExpectedChecksum(ctx context.Context, sumsURL, assetName string) (string, error) {
	resp, err := httpGet(ctx, sumsURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes))
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

func downloadToTemp(ctx context.Context, url string) (string, error) {
	resp, err := httpGet(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp("", "qi-update-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err == nil && n > maxArchiveBytes {
		err = fmt.Errorf("archive exceeds %d bytes", int64(maxArchiveBytes))
	}
	if err != nil {
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

func extractBinaryFromTar(archivePath, binaryName string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Name != binaryName {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("archive member %q is not a regular file", hdr.Name)
		}
		if hdr.Size > maxArchiveBytes {
			return "", fmt.Errorf("archive member %q declares %d bytes, over the %d byte limit", hdr.Name, hdr.Size, int64(maxArchiveBytes))
		}
		out, err := os.CreateTemp("", "qi-update-bin-*")
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, maxArchiveBytes)); err != nil {
			out.Close()
			os.Remove(out.Name())
			return "", err
		}
		out.Close()
		return out.Name(), nil
	}
	return "", fmt.Errorf("binary %q not found in archive", binaryName)
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
