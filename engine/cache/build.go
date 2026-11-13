// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"runtime"

	"github.com/dexpace/morphic/engine/reader"
)

// selfExe is where Linux serves the image this process runs. It reads as a link
// to the executable's path, but opening it opens the file the process started
// from, which a rename, link or removal at that path since leaves in place.
const selfExe = "/proc/self/exe"

// BuildID names the running build by the SHA-256 of the image the process
// runs, for engine.WithCache. A module version does not name a build: every
// dirty tree and every go run shares one, and no version says which toolchain
// built it.
//
// On Linux the image is read through selfExe. Elsewhere it is read at the
// executable's path, which holds it only until a build is installed there: one
// installed after the process started and before this read is named in its
// place, and what the process compiles is kept under that build's key (#1318).
func BuildID(ctx context.Context) (string, error) {
	return buildID(ctx, runtime.GOOS, os.Executable)
}

// buildID is BuildID on the system goos names, the executable's path found by
// executable, so a test can take each system's way and fail finding the path.
func buildID(ctx context.Context, goos string, executable func() (string, error)) (string, error) {
	path, err := imagePath(goos, executable)
	if err != nil {
		return "", err
	}
	data, err := reader.ReadSource(ctx, path, 0)
	if err != nil {
		return "", fmt.Errorf("cache: read the executable: %w", err)
	}
	return Key(sha256.Sum256(data)).String(), nil
}

// imagePath returns the file to read the image this process runs from on goos:
// the one Linux serves it through, or the executable's path elsewhere.
func imagePath(goos string, executable func() (string, error)) (string, error) {
	if goos == "linux" {
		return selfExe, nil
	}
	path, err := executable()
	if err != nil {
		return "", fmt.Errorf("cache: find the executable: %w", err)
	}
	return path, nil
}
