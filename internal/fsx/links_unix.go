//go:build unix

package fsx

import (
	"io/fs"
	"syscall"
)

func hardLinked(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && stat.Nlink > 1
}
