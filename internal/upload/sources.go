package upload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	DefaultMaxBytes = int64(5 * 1024 * 1024)
	DefaultTimeout  = 30 * time.Second
)

type Mode string

const (
	ModeStdio Mode = "stdio"
	ModeHTTP  Mode = "http"
)

type SourceKind string

const (
	SourceInline SourceKind = "inline"
	SourceLocal  SourceKind = "local_path"
	SourceURL    SourceKind = "url"
)

type ResolvedSource struct {
	Kind        SourceKind
	Filename    string
	ContentType string
	Size        int64
	Content     []byte
}

type ReadOptions struct {
	MaxBytes int64
	Timeout  time.Duration
}

type InlineSource struct {
	Content     []byte
	Filename    string
	ContentType string
}

func ResolveInline(ctx context.Context, source InlineSource, opts ReadOptions) (ResolvedSource, error) {
	return resolveBytes(ctx, SourceInline, source.Content, source.Filename, source.ContentType, opts)
}

func resolveBytes(ctx context.Context, kind SourceKind, content []byte, filename string, contentType string, opts ReadOptions) (ResolvedSource, error) {
	maxBytes := effectiveMaxBytes(opts.MaxBytes)
	if int64(len(content)) > maxBytes {
		return ResolvedSource{}, fmt.Errorf("%s upload source exceeds maxBytes %d", kind, maxBytes)
	}
	if err := ctx.Err(); err != nil {
		return ResolvedSource{}, err
	}
	return ResolvedSource{
		Kind:        kind,
		Filename:    cleanFilename(filename),
		ContentType: strings.TrimSpace(contentType),
		Size:        int64(len(content)),
		Content:     append([]byte(nil), content...),
	}, nil
}

func readBounded(ctx context.Context, reader io.Reader, opts ReadOptions) ([]byte, error) {
	maxBytes := effectiveMaxBytes(opts.MaxBytes)
	timeout := effectiveTimeout(opts.Timeout)
	readCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok && timeout > 0 {
		readCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	type result struct {
		content []byte
		err     error
	}
	done := make(chan result, 1)
	go func() {
		content, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
		if err == nil && int64(len(content)) > maxBytes {
			err = fmt.Errorf("upload source exceeds maxBytes %d", maxBytes)
		}
		done <- result{content: content, err: err}
	}()

	select {
	case <-readCtx.Done():
		return nil, readCtx.Err()
	case result := <-done:
		if result.err != nil {
			return nil, result.err
		}
		return result.content, nil
	}
}

func effectiveMaxBytes(value int64) int64 {
	if value > 0 {
		return value
	}
	return DefaultMaxBytes
}

func effectiveTimeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return DefaultTimeout
}

func cleanFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return path.Base(strings.ReplaceAll(value, "\\", "/"))
}

func filenameFromHTTP(resp *http.Response, fallback string) string {
	if disposition := resp.Header.Get("Content-Disposition"); disposition != "" {
		_, params, err := mime.ParseMediaType(disposition)
		if err == nil {
			if filename := cleanFilename(params["filename"]); filename != "" {
				return filename
			}
		}
	}
	return cleanFilename(fallback)
}

func redactURLForError(raw string) string {
	if raw == "" {
		return ""
	}
	return "[REDACTED_URL]"
}

var errHTTPModeLocalPath = errors.New("HTTP mode rejects local upload paths")
