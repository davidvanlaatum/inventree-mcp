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

func ResolveLocalFile(ctx context.Context, source LocalFileSource, opts LocalFileOptions) (ResolvedSource, error) {
	if opts.Mode == ModeHTTP {
		return ResolvedSource{}, errHTTPModeLocalPath
	}
	if strings.TrimSpace(source.Path) == "" {
		return ResolvedSource{}, errors.New("local upload path is required")
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
		return errors.New("local upload allowlist requires at least one root")
	}
	for _, root := range roots {
		resolvedRoot, err := canonicalPath(fs, root)
		if err != nil {
			return err
		}
		if pathWithinRoot(candidate, resolvedRoot) {
			return nil
		}
	}
	return errors.New("local upload path is outside allowlisted roots")
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
