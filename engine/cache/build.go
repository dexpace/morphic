// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/dexpace/morphic/engine/reader"
)

// BuildID names the running build by the SHA-256 of its executable, for
// engine.WithCache. A module version does not name a build: every dirty tree
// and every go run shares one, and no version says which toolchain built it.
//
// It hashes the file at the executable's path, not the image running, which no
// portable call reads. An executable replaced after the process started and
// before this read is named for its replacement, so what the process compiles
// is kept under the new build's key and answers that build's runs.
func BuildID(ctx context.Context) (string, error) {
	return buildID(ctx, os.Executable)
}

// buildID is BuildID with the executable's location handed in, so a test can
// fail it.
func buildID(ctx context.Context, executable func() (string, error)) (string, error) {
	path, err := executable()
	if err != nil {
		return "", fmt.Errorf("cache: find the executable: %w", err)
	}
	data, err := reader.ReadSource(ctx, path, 0)
	if err != nil {
		return "", fmt.Errorf("cache: read the executable: %w", err)
	}
	return Key(sha256.Sum256(data)).String(), nil
}
