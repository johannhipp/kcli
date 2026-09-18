//go:build darwin || linux

package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/johannhipp/kcli/internal/domain"
	"golang.org/x/sys/unix"
)

// downloadDirectory holds the authorized directory across network waits. Every
// write uses its descriptor rather than resolving its original path again.
type downloadDirectory struct {
	file *os.File
	path string
}

func openDownloadDirectory(path string) (*downloadDirectory, error) {
	const flags = directorySearchFlag | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next, openErr := unix.Openat(fd, part, flags, 0)
		if errors.Is(openErr, unix.ENOENT) {
			if mkdirErr := unix.Mkdirat(fd, part, 0700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				unix.Close(fd)
				return nil, mkdirErr
			}
			next, openErr = unix.Openat(fd, part, flags, 0)
		}
		unix.Close(fd)
		if openErr != nil {
			return nil, &domain.Error{Code: domain.CodeInvalidInput, Message: "output directory must contain directories only, with no symlink components", Cause: openErr}
		}
		fd = next
	}
	// Search handles preserve traversal through ancestors without list permission.
	// Restore permissions through the held directory before opening it for I/O.
	if err := unix.Fchmodat(fd, ".", 0700, 0); err != nil {
		unix.Close(fd)
		return nil, err
	}
	readFD, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	unix.Close(fd)
	if err != nil {
		return nil, err
	}
	return &downloadDirectory{file: os.NewFile(uintptr(readFD), path), path: path}, nil
}

func (d *downloadDirectory) Close() error { return d.file.Close() }

func (d *downloadDirectory) writeAtomic(ctx context.Context, name string, body []byte, overwrite bool) error {
	if err := ctx.Err(); err != nil {
		return &domain.Error{Code: domain.CodeInterrupted, Message: "image download interrupted", Cause: err}
	}
	if name == "." || name == ".." || filepath.Base(name) != name {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid image destination"}
	}
	// Avoid returning a stale public path if the directory changed during fetch.
	held, err := d.file.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(d.path)
	if err != nil || !os.SameFile(held, current) {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "image output directory changed during download", Cause: err}
	}
	fd := int(d.file.Fd())
	var info unix.Stat_t
	if err := unix.Fstatat(fd, name, &info, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if info.Mode&unix.S_IFMT != unix.S_IFREG {
			return &domain.Error{Code: domain.CodeInvalidInput, Message: "image destination is not a regular file"}
		}
		if !overwrite {
			return &domain.Error{Code: domain.CodeInvalidInput, Message: "image destination already exists; use --overwrite to replace it"}
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "inspect image destination", Cause: err}
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	temporary := name + ".tmp-" + hex.EncodeToString(suffix)
	temporaryFD, err := unix.Openat(fd, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "create image temporary file", Cause: err}
	}
	defer unix.Unlinkat(fd, temporary, 0)
	file := os.NewFile(uintptr(temporaryFD), temporary)
	defer file.Close()
	if _, err := file.Write(body); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "write image temporary file", Cause: err}
	}
	if err := ctx.Err(); err != nil {
		return &domain.Error{Code: domain.CodeInterrupted, Message: "image download interrupted", Cause: err}
	}
	if err := file.Sync(); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "sync image temporary file", Cause: err}
	}
	if err := file.Close(); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "close image temporary file", Cause: err}
	}
	if !overwrite {
		// Install without replacing a concurrently created destination.
		if err := installNoReplace(fd, temporary, name); err != nil {
			if errors.Is(err, unix.EEXIST) {
				return &domain.Error{Code: domain.CodeInvalidInput, Message: "image destination already exists; use --overwrite to replace it", Cause: err}
			}
			return &domain.Error{Code: domain.CodeUnavailable, Message: "install downloaded image", Cause: err}
		}
	} else if err := unix.Renameat(fd, temporary, fd, name); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "replace downloaded image", Cause: err}
	}
	return nil
}
