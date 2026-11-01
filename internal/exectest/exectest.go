// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package exectest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/renameio/v2"
)

// coverDirEnv names the directory a coverage build writes its data to.
const coverDirEnv = "GOCOVERDIR"

// Coverage files are named by the runtime (internal/coverage, which no module
// may import): covmeta.<hash> once per binary, covcounters.<hash>.<pid>.<time>
// once per process. Each is written to a "tmp." name and renamed, so a file
// with either prefix is whole.
const (
	metaPrefix     = "covmeta."
	countersPrefix = "covcounters."
)

// copyPerm is the mode of a coverage file copied into the parent's directory.
const copyPerm = 0o644

// Command returns exec.CommandContext(ctx, name, args...) with Env set. When
// the test binary is built for coverage, the child gets a GOCOVERDIR of its
// own, and t's cleanup copies the coverage files it left into this
// process's, so the profile counts what only the child ran. Children sharing
// one directory race: a test-binary child writes its meta-data file from an
// exit hook, through a temporary name taken from the clock, and two children
// finishing together on a clock ticking in microseconds read one value, so
// the second fails to rename. A site extends cmd.Env only with append(cmd.Env, …).
func Command(ctx context.Context, t testing.TB, name string, args ...string) *exec.Cmd {
	t.Helper()
	if ctx == nil {
		t.Fatal("exectest.Command: nil context")
	}
	if name == "" {
		t.Fatal("exectest.Command: empty command name")
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	parent := os.Getenv(coverDirEnv)
	if parent == "" {
		return cmd
	}

	own := t.TempDir()
	// Cleanups run last in, first out, so the copy runs before the directory
	// t.TempDir registered is removed.
	t.Cleanup(func() {
		if err := adopt(own, parent); err != nil {
			t.Errorf("exectest: adopt the child's coverage: %v", err)
		}
	})
	cmd.Env = slices.DeleteFunc(cmd.Env, func(kv string) bool { return strings.HasPrefix(kv, coverDirEnv+"=") })
	cmd.Env = append(cmd.Env, coverDirEnv+"="+own)
	return cmd
}

// adopt copies the coverage files a child left in from into to. A counter
// file is the child's alone. The meta-data file is the same for every child
// of one binary, so one that to holds is left alone: the copies are renamed
// whole, and a duplicate written by a child that raced this one replaces its
// twin unseen.
func adopt(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return fmt.Errorf("read the child's directory: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		isMeta := strings.HasPrefix(name, metaPrefix)
		if !isMeta && !strings.HasPrefix(name, countersPrefix) {
			continue
		}
		dst := filepath.Join(to, name)
		if isMeta {
			held, err := exists(dst)
			if err != nil {
				return err
			}
			if held {
				continue
			}
		}
		if err := copyWhole(filepath.Join(from, name), dst); err != nil {
			return err
		}
	}
	return nil
}

// exists reports whether path names a file. An error other than its absence,
// such as a parent that is not a directory, is returned and not read as one.
func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look for the parent's copy: %w", err)
	}
	return true, nil
}

// copyWhole copies the file src to dst through a temporary name beside dst,
// which no other copy picks, so that dst never holds part of it.
func copyWhole(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read the child's file: %w", err)
	}
	if err := renameio.WriteFile(dst, raw, copyPerm); err != nil {
		return fmt.Errorf("copy the child's file: %w", err)
	}
	return nil
}
