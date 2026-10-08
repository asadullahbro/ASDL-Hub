//go:build windows

package cli

import "os"

// The Hub server runs on Linux; on Windows only the client commands are used,
// and these server-side helpers do nothing.

func diskFree(path string) (freeBytes, totalBytes uint64, ok bool) { return 0, 0, false }

func chownLike(path string, fi os.FileInfo) {}
