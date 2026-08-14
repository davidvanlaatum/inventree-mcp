package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
)

// HTTPDoer is the narrow HTTP seam used by the updater.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type release struct {
	Version     string
	ArchiveName string
	ArchiveURL  string
	ChecksumURL string
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func (u *Updater) resolveRelease(ctx context.Context, target, token string) (release, error) {
	endpoint := strings.TrimRight(u.deps.APIBaseURL, "/") + "/repos/" + repositoryOwner + "/" + repositoryName + "/releases/"
	if target == "" {
		endpoint += "latest"
	} else {
		endpoint += "tags/" + url.PathEscape(target)
	}
	body, err := u.get(ctx, endpoint, token, u.deps.Limits.MetadataBytes)
	if err != nil {
		return release{}, fmt.Errorf("resolve GitHub release: %w", err)
	}
	var metadata githubRelease
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&metadata); err != nil {
		return release{}, fmt.Errorf("decode GitHub release metadata: %w", err)
	}
	if err := requireJSONEOF(dec); err != nil {
		return release{}, fmt.Errorf("decode GitHub release metadata: %w", err)
	}
	if metadata.Draft || metadata.Prerelease {
		return release{}, fmt.Errorf("release %q is draft or prerelease: %w", metadata.TagName, ErrVersionPolicy)
	}
	version, err := canonicalStableVersion(metadata.TagName)
	if err != nil {
		return release{}, fmt.Errorf("release tag: %w", err)
	}
	if target != "" && version != target {
		return release{}, fmt.Errorf("release tag %s does not match requested %s", version, target)
	}
	archiveName := releaseArchiveName(u.deps.GOOS, u.deps.GOARCH)
	var archiveURL, checksumURL string
	for _, asset := range metadata.Assets {
		switch asset.Name {
		case archiveName:
			if archiveURL != "" {
				return release{}, fmt.Errorf("duplicate release asset %q", archiveName)
			}
			archiveURL = asset.BrowserDownloadURL
		case "checksums.txt":
			if checksumURL != "" {
				return release{}, fmt.Errorf("duplicate release asset checksums.txt")
			}
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if archiveURL == "" || checksumURL == "" {
		return release{}, fmt.Errorf("release %s is missing %q or checksums.txt", version, archiveName)
	}
	if err := validateAssetURL(archiveURL, version, archiveName); err != nil {
		return release{}, fmt.Errorf("archive asset URL: %w", err)
	}
	if err := validateAssetURL(checksumURL, version, "checksums.txt"); err != nil {
		return release{}, fmt.Errorf("checksum asset URL: %w", err)
	}
	return release{Version: version, ArchiveName: archiveName, ArchiveURL: archiveURL, ChecksumURL: checksumURL}, nil
}

func releaseArchiveName(goos, goarch string) string {
	osName := map[string]string{"darwin": "Darwin", "linux": "Linux"}[goos]
	archName := map[string]string{"amd64": "x86_64", "arm64": "arm64"}[goarch]
	return binaryName + "_" + osName + "_" + archName + ".tar.gz"
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func validateAssetURL(raw, version, filename string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("untrusted canonical GitHub asset URL")
	}
	wantPath := "/" + repositoryOwner + "/" + repositoryName + "/releases/download/" + version + "/" + filename
	if path.Clean(u.Path) != wantPath || u.RawQuery != "" {
		return fmt.Errorf("asset URL does not match release %s asset %q", version, filename)
	}
	return nil
}

func (u *Updater) download(ctx context.Context, rawURL, token string, limit int64) ([]byte, error) {
	return u.get(ctx, rawURL, token, limit)
}

func (u *Updater) get(ctx context.Context, rawURL, token string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	if token != "" && req.URL.Scheme == "https" && req.URL.Host == "api.github.com" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := u.deps.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		closeErr := resp.Body.Close()
		return nil, errors.Join(fmt.Errorf("GET %s returned %s", req.URL.Hostname(), resp.Status), closeErr)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, errors.Join(err, closeErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func newHTTPClient(timeout time.Duration, extraAllowedHosts []string) *http.Client {
	allowed := map[string]bool{
		"api.github.com":                       true,
		"github.com":                           true,
		"release-assets.githubusercontent.com": true,
		"objects.githubusercontent.com":        true,
	}
	for _, host := range extraAllowedHosts {
		allowed[host] = true
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Scheme != "https" || !allowed[req.URL.Host] || req.URL.User != nil {
				return fmt.Errorf("redirect to untrusted origin")
			}
			if req.URL.Host != "api.github.com" {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

func verifyChecksum(filename string, archive, checksumFile []byte) error {
	want := ""
	seen := map[string]bool{}
	for lineNumber, line := range strings.Split(string(checksumFile), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return fmt.Errorf("invalid checksum line %d", lineNumber+1)
		}
		name := strings.TrimPrefix(fields[1], "*")
		if seen[name] {
			return fmt.Errorf("duplicate checksum entry %q", name)
		}
		seen[name] = true
		if name == filename {
			if len(fields[0]) != sha256.Size*2 {
				return fmt.Errorf("invalid SHA-256 for %q", filename)
			}
			if _, err := hex.DecodeString(fields[0]); err != nil {
				return fmt.Errorf("invalid SHA-256 for %q: %w", filename, err)
			}
			want = strings.ToLower(fields[0])
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %q", filename)
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("SHA-256 mismatch for %q", filename)
	}
	return nil
}
