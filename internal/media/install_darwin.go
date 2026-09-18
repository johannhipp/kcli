package media

import "golang.org/x/sys/unix"

func installNoReplace(fd int, temporary, name string) error {
	return unix.RenameatxNp(fd, temporary, fd, name, unix.RENAME_EXCL)
}
