package media

import (
	"errors"

	"golang.org/x/sys/unix"
)

const directorySearchFlag = unix.O_PATH

func installNoReplace(fd int, temporary, name string) error {
	err := unix.Renameat2(fd, temporary, fd, name, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
		// Older kernels/filesystems may support atomic linking but not rename flags.
		return unix.Linkat(fd, temporary, fd, name, 0)
	}
	return err
}
