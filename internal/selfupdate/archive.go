package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
)

func extractExecutable(archive []byte, limit int64) ([]byte, fs.FileMode, error) {
	compressed := bytes.NewReader(archive)
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, 0, fmt.Errorf("open release gzip: %w", err)
	}
	gz.Multistream(false)
	tarReader := tar.NewReader(gz)

	header, err := tarReader.Next()
	if err != nil {
		_ = gz.Close()
		return nil, 0, fmt.Errorf("read release archive: %w", err)
	}
	if header.Name != binaryName || header.Typeflag != tar.TypeReg || !header.FileInfo().Mode().IsRegular() {
		_ = gz.Close()
		return nil, 0, fmt.Errorf("release archive must contain exactly one regular %q executable", binaryName)
	}
	mode := header.FileInfo().Mode().Perm()
	if mode&0o111 == 0 {
		_ = gz.Close()
		return nil, 0, fmt.Errorf("release executable has no execute bits")
	}
	if header.Size < 1 || header.Size > limit {
		_ = gz.Close()
		return nil, 0, fmt.Errorf("release executable size %d exceeds policy", header.Size)
	}
	binary, err := io.ReadAll(io.LimitReader(tarReader, limit+1))
	if err != nil {
		_ = gz.Close()
		return nil, 0, fmt.Errorf("read release executable: %w", err)
	}
	if int64(len(binary)) != header.Size || int64(len(binary)) > limit {
		_ = gz.Close()
		return nil, 0, fmt.Errorf("release executable is truncated or oversized")
	}
	if next, nextErr := tarReader.Next(); nextErr != io.EOF || next != nil {
		_ = gz.Close()
		return nil, 0, fmt.Errorf("release archive contains unexpected or duplicate entries")
	}
	trailing, readErr := io.ReadAll(io.LimitReader(gz, 2))
	if readErr != nil {
		_ = gz.Close()
		return nil, 0, fmt.Errorf("read archive trailer: %w", readErr)
	}
	if len(trailing) != 0 {
		_ = gz.Close()
		return nil, 0, fmt.Errorf("release tar contains trailing payload")
	}
	if err := gz.Close(); err != nil {
		return nil, 0, fmt.Errorf("close release gzip: %w", err)
	}
	if compressed.Len() != 0 {
		return nil, 0, fmt.Errorf("release archive contains trailing compressed payload")
	}
	return binary, mode, nil
}
