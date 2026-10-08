//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// diskFree reports the free space and size of the filesystem holding path.
func diskFree(path string) (freeBytes, totalBytes uint64, ok bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil || st.Blocks == 0 {
		return 0, 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize), true
}

// chownLike gives path the owner and group of the file described by fi.
func chownLike(path string, fi os.FileInfo) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(path, int(st.Uid), int(st.Gid))
	}
}
