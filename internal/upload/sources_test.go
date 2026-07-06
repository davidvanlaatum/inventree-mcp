package upload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveInlineCopiesContentAndEnforcesLimit(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	sourceBytes := []byte("datasheet")

	resolved, err := ResolveInline(ctx, InlineSource{
		Content:     sourceBytes,
		Filename:    "../data sheet.pdf",
		ContentType: "application/pdf",
	}, ReadOptions{MaxBytes: 32})
	r.NoError(err)
	sourceBytes[0] = 'D'

	a.Equal(SourceInline, resolved.Kind)
	a.Equal("data sheet.pdf", resolved.Filename)
	a.Equal("application/pdf", resolved.ContentType)
	a.Equal(int64(9), resolved.Size)
	a.Equal([]byte("datasheet"), resolved.Content)

	_, err = ResolveInline(ctx, InlineSource{Content: []byte("too large")}, ReadOptions{MaxBytes: 3})
	r.Error(err)
	a.Contains(err.Error(), "exceeds maxBytes 3")
}

func TestResolveLocalFileRejectsHTTPModeBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fs := &failingFs{}

	_, err := ResolveLocalFile(ctx, LocalFileSource{Path: "/secrets/token.txt"}, LocalFileOptions{
		Mode:       ModeHTTP,
		Fs:         fs,
		AllowRoots: []string{"/secrets"},
	})

	r.ErrorIs(err, errHTTPModeLocalPath)
	a.False(fs.touched)
}

func TestResolveLocalFileUsesAferoAllowlistAndRegularFileChecks(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fs := afero.NewMemMapFs()
	r.NoError(fs.MkdirAll("/allow/sub", 0o755))
	r.NoError(afero.WriteFile(fs, "/allow/sub/datasheet.txt", []byte("hello"), 0o644))
	r.NoError(fs.MkdirAll("/allow/dir", 0o755))

	resolved, err := ResolveLocalFile(ctx, LocalFileSource{
		Path:        "/allow/../allow/sub/datasheet.txt",
		ContentType: "text/plain",
	}, LocalFileOptions{
		Mode:       ModeStdio,
		Fs:         fs,
		AllowRoots: []string{"/allow"},
		ReadOptions: ReadOptions{
			MaxBytes: 8,
		},
	})
	r.NoError(err)
	a.Equal(SourceLocal, resolved.Kind)
	a.Equal("datasheet.txt", resolved.Filename)
	a.Equal("hello", string(resolved.Content))

	_, err = ResolveLocalFile(ctx, LocalFileSource{Path: "/allow/sub/datasheet.txt"}, LocalFileOptions{
		Mode:        ModeStdio,
		Fs:          fs,
		AllowRoots:  []string{"/allow"},
		ReadOptions: ReadOptions{MaxBytes: 3},
	})
	r.Error(err)
	a.Contains(err.Error(), "exceeds maxBytes 3")

	_, err = ResolveLocalFile(ctx, LocalFileSource{Path: "/outside.txt"}, LocalFileOptions{
		Mode:       ModeStdio,
		Fs:         fs,
		AllowRoots: []string{"/allow"},
	})
	r.Error(err)
	a.Contains(err.Error(), "outside allowlisted roots")

	_, err = ResolveLocalFile(ctx, LocalFileSource{Path: "/allow/dir"}, LocalFileOptions{
		Mode:       ModeStdio,
		Fs:         fs,
		AllowRoots: []string{"/allow"},
	})
	r.Error(err)
	a.Contains(err.Error(), "regular file")
}

func TestResolveLocalFileRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	root := t.TempDir()
	allowRoot := filepath.Join(root, "allow")
	outsideRoot := filepath.Join(root, "outside")
	r.NoError(os.MkdirAll(allowRoot, 0o755))
	r.NoError(os.MkdirAll(outsideRoot, 0o755))
	outsideFile := filepath.Join(outsideRoot, "secret.txt")
	r.NoError(os.WriteFile(outsideFile, []byte("secret"), 0o644))
	linkPath := filepath.Join(allowRoot, "link.txt")
	r.NoError(os.Symlink(outsideFile, linkPath))

	_, err := ResolveLocalFile(ctx, LocalFileSource{Path: linkPath}, LocalFileOptions{
		Mode:       ModeStdio,
		Fs:         afero.NewOsFs(),
		AllowRoots: []string{allowRoot},
	})

	r.Error(err)
	a.Contains(err.Error(), "outside allowlisted roots")
}

func TestURLFetcherFetchesWithoutForwardingAuthHeaders(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	var receivedAuth string
	var receivedCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		receivedAuth = req.Header.Get("Authorization")
		receivedCookie = req.Header.Get("Cookie")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="remote.txt"`)
		_, _ = w.Write([]byte("remote data"))
	}))
	t.Cleanup(server.Close)

	resolved, err := (URLFetcher{
		Resolver:  staticResolver("127.0.0.1"),
		Allowlist: allowServer(server.URL),
	}).Fetch(ctx, URLSource{URL: server.URL + "/file.txt?token=secret"}, ReadOptions{MaxBytes: 32})

	r.NoError(err)
	a.Equal(SourceURL, resolved.Kind)
	a.Equal("remote.txt", resolved.Filename)
	a.Equal("text/plain", resolved.ContentType)
	a.Equal("remote data", string(resolved.Content))
	a.Empty(receivedAuth)
	a.Empty(receivedCookie)
}

func TestURLFetcherRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		rawURL    string
		resolver  DNSResolver
		wantError string
	}{
		{name: "file scheme", rawURL: "file:///etc/passwd", resolver: staticResolver("93.184.216.34"), wantError: "scheme must be http or https"},
		{name: "userinfo", rawURL: "https://user:pass@example.com/file.txt", resolver: staticResolver("93.184.216.34"), wantError: "must not include userinfo"},
		{name: "loopback", rawURL: "http://example.test/file.txt", resolver: staticResolver("127.0.0.1"), wantError: "blocked address"},
		{name: "private", rawURL: "http://example.test/file.txt", resolver: staticResolver("10.0.0.4"), wantError: "blocked address"},
		{name: "link local", rawURL: "http://example.test/file.txt", resolver: staticResolver("169.254.169.254"), wantError: "blocked address"},
		{name: "documentation range", rawURL: "http://example.test/file.txt", resolver: staticResolver("203.0.113.7"), wantError: "blocked address"},
		{name: "ipv6 unique local", rawURL: "http://example.test/file.txt", resolver: staticResolver("fd00::1"), wantError: "blocked address"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)

			_, err := (URLFetcher{Resolver: tt.resolver}).Fetch(ctx, URLSource{URL: tt.rawURL}, ReadOptions{MaxBytes: 16})

			r.Error(err)
			a.Contains(err.Error(), tt.wantError)
		})
	}
}

func TestURLFetcherRevalidatesRedirectsAndLimitsBytes(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	blockedRedirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("blocked"))
	}))
	t.Cleanup(blockedRedirect.Close)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, blockedRedirect.URL+"/next", http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	_, err := (URLFetcher{
		Resolver: resolverByHost(map[string]string{
			hostOnly(redirector.URL):      "93.184.216.34",
			hostOnly(blockedRedirect.URL): "127.0.0.1",
		}),
		Allowlist: allowServer(redirector.URL),
	}).Fetch(ctx, URLSource{URL: redirector.URL + "/start"}, ReadOptions{MaxBytes: 16})
	r.Error(err)
	a.Contains(err.Error(), "failed")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("too large"))
	}))
	t.Cleanup(server.Close)

	_, err = (URLFetcher{
		Resolver:  staticResolver("127.0.0.1"),
		Allowlist: allowServer(server.URL),
	}).Fetch(ctx, URLSource{URL: server.URL + "/file"}, ReadOptions{MaxBytes: 3})
	r.Error(err)
	a.Contains(err.Error(), "exceeds maxBytes 3")
}

func TestReadBoundedHonorsTimeout(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	reader, writer := io.Pipe()
	defer func() {
		_ = reader.Close()
		_ = writer.Close()
	}()

	start := time.Now()
	_, err := readBounded(ctx, reader, ReadOptions{Timeout: 10 * time.Millisecond})

	r.Error(err)
	a.ErrorIs(err, context.DeadlineExceeded)
	a.Less(time.Since(start), time.Second)
}

func TestURLFetcherHonorsTimeout(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
	}))
	t.Cleanup(server.Close)

	start := time.Now()
	_, err := (URLFetcher{
		Resolver:  staticResolver("127.0.0.1"),
		Allowlist: allowServer(server.URL),
	}).Fetch(ctx, URLSource{URL: server.URL + "/slow"}, ReadOptions{
		MaxBytes: 16,
		Timeout:  10 * time.Millisecond,
	})

	r.Error(err)
	a.Contains(err.Error(), "failed")
	a.Less(time.Since(start), time.Second)
}

func TestUploadResolversDoNotLogSensitiveSourceValues(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	fs := afero.NewMemMapFs()
	r.NoError(fs.MkdirAll("/allow", 0o755))
	r.NoError(afero.WriteFile(fs, "/allow/secret-token.txt", []byte("raw uploaded bytes"), 0o644))

	_, err := ResolveInline(ctx, InlineSource{
		Content:  []byte("inline secret bytes"),
		Filename: "inline-secret.txt",
	}, ReadOptions{MaxBytes: 64})
	r.NoError(err)
	_, err = ResolveLocalFile(ctx, LocalFileSource{Path: "/allow/secret-token.txt"}, LocalFileOptions{
		Mode:       ModeStdio,
		Fs:         fs,
		AllowRoots: []string{"/allow"},
	})
	r.NoError(err)

	allLogs := handler.Logs()
	a.Empty(allLogs)
	logText := stringifyLogs(allLogs)
	a.NotContains(logText, "inline secret bytes")
	a.NotContains(logText, "raw uploaded bytes")
	a.NotContains(logText, "/allow/secret-token.txt")
	a.NotContains(logText, "token=secret")
}

func staticResolver(ip string) DNSResolver {
	return func(context.Context, string) ([]netip.Addr, error) {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			return nil, err
		}
		return []netip.Addr{addr}, nil
	}
}

func resolverByHost(values map[string]string) DNSResolver {
	return func(_ context.Context, host string) ([]netip.Addr, error) {
		ip, ok := values[host]
		if !ok {
			return nil, errors.New("unexpected host")
		}
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			return nil, err
		}
		return []netip.Addr{addr}, nil
	}
}

func allowServer(rawURL string) []URLAllowlistEntry {
	parsed := mustParseURL(rawURL)
	return []URLAllowlistEntry{{
		Scheme: parsed.Scheme,
		Host:   parsed.Hostname(),
		Port:   parsed.Port(),
	}}
}

func hostOnly(rawURL string) string {
	return mustParseURL(rawURL).Hostname()
}

func mustParseURL(rawURL string) *url.URL {
	parts, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return parts
}

func stringifyLogs(records []testhandler.LogRecord) string {
	var builder strings.Builder
	for i := range records {
		builder.WriteString(records[i].String())
		builder.WriteString("\n")
	}
	return builder.String()
}

type failingFs struct {
	afero.Fs
	touched bool
}

func (f *failingFs) Name() string {
	return "failing"
}

func (f *failingFs) Create(string) (afero.File, error) {
	return nil, errors.New("unexpected filesystem access")
}

func (f *failingFs) Mkdir(string, os.FileMode) error {
	return errors.New("unexpected filesystem access")
}

func (f *failingFs) MkdirAll(string, os.FileMode) error {
	return errors.New("unexpected filesystem access")
}

func (f *failingFs) Open(string) (afero.File, error) {
	f.touched = true
	return nil, errors.New("unexpected filesystem access")
}

func (f *failingFs) OpenFile(string, int, os.FileMode) (afero.File, error) {
	f.touched = true
	return nil, errors.New("unexpected filesystem access")
}

func (f *failingFs) Remove(string) error {
	return errors.New("unexpected filesystem access")
}

func (f *failingFs) RemoveAll(string) error {
	return errors.New("unexpected filesystem access")
}

func (f *failingFs) Rename(string, string) error {
	return errors.New("unexpected filesystem access")
}

func (f *failingFs) Stat(string) (os.FileInfo, error) {
	f.touched = true
	return nil, errors.New("unexpected filesystem access")
}

func (f *failingFs) Chmod(string, os.FileMode) error {
	return errors.New("unexpected filesystem access")
}

func (f *failingFs) Chown(string, int, int) error {
	return errors.New("unexpected filesystem access")
}

func (f *failingFs) Chtimes(string, time.Time, time.Time) error {
	return errors.New("unexpected filesystem access")
}
