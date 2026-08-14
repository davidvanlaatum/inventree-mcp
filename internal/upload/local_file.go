package upload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

type LocalFileSource struct {
	Path        string
	Filename    string
	ContentType string
}

type LocalFileOptions struct {
	ReadOptions
	Mode       Mode
	Fs         afero.Fs
	AllowRoots []string
}

// ErrLocalUploadAllowlistRequired means no trusted local upload root is configured.
var ErrLocalUploadAllowlistRequired = errors.New("local upload allowlist requires at least one root")

// ErrLocalUploadOutsideAllowlist means the resolved local source is outside every trusted root.
var ErrLocalUploadOutsideAllowlist = errors.New("local upload path is outside allowlisted roots")

func ResolveLocalFile(ctx context.Context, source LocalFileSource, opts LocalFileOptions) (ResolvedSource, error) {
	if opts.Mode == ModeHTTP {
		return ResolvedSource{}, errHTTPModeLocalPath
	}
	if strings.TrimSpace(source.Path) == "" {
		return ResolvedSource{}, errors.New("local upload path is required")
	}
	if len(opts.AllowRoots) == 0 {
		return ResolvedSource{}, ErrLocalUploadAllowlistRequired
	}
	fs := opts.Fs
	if fs == nil {
		fs = afero.NewOsFs()
	}

	resolvedPath, err := canonicalPath(fs, source.Path)
	if err != nil {
		return ResolvedSource{}, err
	}
	if err := requireAllowedPath(fs, resolvedPath, opts.AllowRoots); err != nil {
		return ResolvedSource{}, err
	}

	file, err := fs.Open(resolvedPath)
	if err != nil {
		return ResolvedSource{}, errors.New("open local upload source failed")
	}
	defer func() {
		_ = file.Close()
	}()
	info, err := file.Stat()
	if err != nil {
		return ResolvedSource{}, errors.New("stat local upload source failed")
	}
	if !info.Mode().IsRegular() {
		return ResolvedSource{}, errors.New("local upload source must be a regular file")
	}

	content, err := readBounded(ctx, file, opts.ReadOptions)
	if err != nil {
		return ResolvedSource{}, err
	}
	filename := source.Filename
	if filename == "" {
		filename = filepath.Base(resolvedPath)
	}
	return ResolvedSource{
		Kind:        SourceLocal,
		Filename:    cleanFilename(filename),
		ContentType: strings.TrimSpace(source.ContentType),
		Size:        int64(len(content)),
		Content:     content,
	}, nil
}

func requireAllowedPath(fs afero.Fs, candidate string, roots []string) error {
	if len(roots) == 0 {
		return ErrLocalUploadAllowlistRequired
	}
	resolvedRoots, err := CanonicalAllowRoots(fs, roots)
	if err != nil {
		return err
	}
	for _, resolvedRoot := range resolvedRoots {
		if pathWithinRoot(candidate, resolvedRoot) {
			return nil
		}
	}
	return ErrLocalUploadOutsideAllowlist
}

// CanonicalAllowRoots returns the effective roots used by local upload policy.
// It preserves configured order while removing canonical duplicates.
func CanonicalAllowRoots(fs afero.Fs, roots []string) ([]string, error) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	canonical := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		resolvedRoot, err := canonicalPath(fs, root)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[resolvedRoot]; ok {
			continue
		}
		seen[resolvedRoot] = struct{}{}
		canonical = append(canonical, resolvedRoot)
	}
	return canonical, nil
}

func pathWithinRoot(candidate string, root string) bool {
	candidate = filepath.Clean(candidate)
	root = filepath.Clean(root)
	if candidate == root {
		return true
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func canonicalPath(fs afero.Fs, value string) (string, error) {
	cleaned := filepath.Clean(value)
	if cleaned == "." {
		return "", errors.New("path is empty after cleaning")
	}
	if osFs, ok := fs.(*afero.OsFs); ok {
		abs, err := filepath.Abs(cleaned)
		if err != nil {
			return "", fmt.Errorf("resolve absolute path: %w", err)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", errors.New("resolve local upload symlinks failed")
		}
		_ = osFs
		return filepath.Clean(resolved), nil
	}
	if !filepath.IsAbs(cleaned) {
		cleaned = string(os.PathSeparator) + cleaned
	}
	return filepath.Clean(cleaned), nil
}
