//go:build darwin || linux

package selfupdate

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

func runtimeGOOS() string { return runtime.GOOS }

func validateOwnedRegular(path string, executable bool) error {
	if os.Getuid() != os.Geteuid() || os.Getgid() != os.Getegid() {
		return fmt.Errorf("real and effective credentials differ")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("not a regular non-symlink file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("ownership metadata unavailable")
	}
	if int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("owner UID %d does not match effective UID %d", stat.Uid, os.Geteuid())
	}
	if stat.Nlink != 1 {
		return fmt.Errorf("hardlink count is %d, want 1", stat.Nlink)
	}
	if info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
		return fmt.Errorf("special permission bits are unsupported")
	}
	if executable && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("file is not executable")
	}
	if executable && info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("executable is group- or world-writable")
	}
	return nil
}

func validateTrustedAncestorChain(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("ancestor %q is not a non-symlink directory", current)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("ancestor %q ownership metadata unavailable", current)
		}
		if int(stat.Uid) != os.Geteuid() && stat.Uid != 0 {
			return fmt.Errorf("ancestor %q is owned by untrusted UID %d", current, stat.Uid)
		}
		writable := info.Mode().Perm()&0o022 != 0
		rootSticky := stat.Uid == 0 && info.Mode()&fs.ModeSticky != 0
		if writable && !rootSticky {
			return fmt.Errorf("ancestor %q is group- or world-writable", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}

func validateOwnedDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("not a non-symlink directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("ownership metadata unavailable")
	}
	if int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("directory owner UID %d does not match effective UID %d", stat.Uid, os.Geteuid())
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("directory is group- or world-writable")
	}
	return nil
}
