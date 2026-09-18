package media

import "golang.org/x/sys/unix"

// O_SEARCH from the macOS SDK sys/fcntl.h; not yet exported by x/sys/unix.
const directorySearchFlag = 0x40000000 | unix.O_DIRECTORY

func installNoReplace(fd int, temporary, name string) error {
	return unix.RenameatxNp(fd, temporary, fd, name, unix.RENAME_EXCL)
}
