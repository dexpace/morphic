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

// engineFor builds the engine opts ask for: one keeping its documents in the
// directory --cache-dir names, under this executable's identity, or the plain
// one. An executable that cannot be read, as a mode of 0111 allows, leaves no
// key sound, which costs the run its cache and a warning, and nothing else. A
// stop meanwhile ends the run, as it would at any other step.
func engineFor(ctx context.Context, opts specOptions, stderr io.Writer) (*engine.Engine, error) {
	if opts.cacheDir == "" {
		return newEngine()
	}
	build, err := buildID(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	dir, dirErr := cache.NewDir(opts.cacheDir)
	if err = errors.Join(err, dirErr); err != nil {
		warnCache(stderr, err)
		return newEngine()
	}
	return newEngine(engine.WithCache(dir, build))
}

// warnCache reports err, why the run could not use its cache, on one line of
// stderr. The run goes on without the cache, so it is a warning, not a failure,
// and one line however many failures err joins.
func warnCache(stderr io.Writer, err error) {
	emitf(stderr, "morphic: warning: %s\n", strings.ReplaceAll(err.Error(), "\n", "; "))
}
