//go:build wasip1 || wasip2

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package os

import (
	"syscall"
	"time"
)

func fillFileStatFromSys(fs *fileStat, name string) {
	fs.name = basename(name)
	fs.size = fs.sys.Size
	fs.modTime = timespecToTime(fs.sys.Mtim)
	switch fs.sys.Mode & syscall.S_IFMT {
	case syscall.S_IFBLK:
		fs.mode |= ModeDevice
	case syscall.S_IFCHR:
		fs.mode |= ModeDevice | ModeCharDevice
	case syscall.S_IFDIR:
		fs.mode |= ModeDir
	case syscall.S_IFIFO:
		fs.mode |= ModeNamedPipe
	case syscall.S_IFLNK:
		fs.mode |= ModeSymlink
	case syscall.S_IFREG:
		// nothing to do
	case syscall.S_IFSOCK:
		fs.mode |= ModeSocket
	}
	// WASI does not expose unix-style permission bits. Go programs commonly
	// check Mode.Perm() (including path-walking helpers that gate on the
	// executable bit), and observing all-zero perms produces spurious
	// "permission denied"-style failures. Match mainline Go's
	// src/os/stat_wasip1.go fallback: 0700 for directories, 0600 for
	// everything else.
	if fs.sys.Mode&syscall.S_IFMT == syscall.S_IFDIR {
		fs.mode |= 0700
	} else {
		fs.mode |= 0600
	}
}

func timespecToTime(ts syscall.Timespec) time.Time {
	return time.Unix(int64(ts.Sec), int64(ts.Nsec))
}

// For testing.
func atime(fi FileInfo) time.Time {
	return timespecToTime(fi.Sys().(*syscall.Stat_t).Atim)
}
