//go:build !unix

package fsx

func processDead(int) bool { return false }

func removeStaleLock(string) error { return nil }
