// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"context"
	"io"

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
	if err != nil {
		emitf(stderr, "morphic: warning: %s\n", err)
		return newEngine()
	}
	return newEngine(engine.WithCache(cache.NewDir(opts.cacheDir), build))
}
