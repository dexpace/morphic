// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"context"
	"io/fs"
	"time"
)

// NewDirLimited returns the Dir at path holding limit bytes, in place of
// MaxBytes.
func NewDirLimited(path string, limit int) *Dir { return &Dir{path: path, limit: limit} }

// Prune exposes prune, so a test can fail each of its steps.
func Prune(dir string, limit int, list func(string) ([]fs.DirEntry, error), remove func(string) error) error {
	return prune(dir, limit, time.Now(), list, remove)
}

// BuildIDOf exposes buildID, so a test can fail finding the executable.
func BuildIDOf(ctx context.Context, executable func() (string, error)) (string, error) {
	return buildID(ctx, executable)
}
