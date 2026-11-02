// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/engine/cache"
)

// buildID names this executable for the cache's keys. It is a package var so
// tests can fail it: no test can make its own binary unreadable.
var buildID = cache.BuildID

// cacheOptions returns the engine options --cache-dir, naming dir, asks for: a
// cache in that directory under this executable's identity, or none without the
// flag. An executable that cannot be read, as a mode of 0111 allows, leaves no
// key sound, which costs the run its cache and a warning, and nothing else. A
// stop meanwhile ends the run, as it would at any other step.
func cacheOptions(ctx context.Context, dir string, stderr io.Writer) ([]engine.Option, error) {
	if dir == "" {
		return nil, nil
	}
	build, err := buildID(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	store, storeErr := cache.NewDir(dir)
	if err = errors.Join(err, storeErr); err != nil {
		warnCache(stderr, err)
		return nil, nil
	}
	return []engine.Option{engine.WithCache(store, build)}, nil
}

// warnCache reports err, why the run could not use its cache, on one line of
// stderr. The run goes on without the cache, so it is a warning, not a failure,
// and one line however many failures err joins.
func warnCache(stderr io.Writer, err error) {
	emitf(stderr, "morphic: warning: %s\n", strings.ReplaceAll(err.Error(), "\n", "; "))
}
