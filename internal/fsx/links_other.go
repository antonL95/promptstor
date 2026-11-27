//go:build !unix

package fsx

import "io/fs"

func hardLinked(fs.FileInfo) bool { return false }
