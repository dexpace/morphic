// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/cache"
	"github.com/dexpace/morphic/ir"
)

var _ Cache = (*cache.Dir)(nil)

// Cache keeps what runs compiled, for WithCache. It holds opaque bytes, and
// overlapping calls must be safe; cache.Dir is the implementation on disk. An
// entry may be lost, damaged or handed back for the wrong key at any time, which
// costs a compile and never a wrong document, as each entry is sealed to its
// key. It is trusted as the executable is: a seal tells those from a sound
// entry, not a forgery from a compile.
type Cache interface {
	// Load returns the entry named key, and false when there is none.
	Load(ctx context.Context, key cache.Key) (entry []byte, ok bool, err error)
	// Store keeps entry as the one named key, replacing any there.
	Store(ctx context.Context, key cache.Key, entry []byte) error
}

// cacheBinding is the cache an engine reads and what names its build.
type cacheBinding struct {
	store Cache
	build string
	// workingDir names the directory a compile reads, os.Getwd outside a test.
	workingDir func() (string, error)
}

// WithCache answers a run from c when an earlier one had the same inputs, and
// stores what a run compiled there. A run's result is the same either way, bar
// the work and Result.CacheErr: architecture.md §2.6 says what a key covers. A
// run setting Verify or FormatOptions takes no part, nor does RunDocument.
//
// build names everything that decides what this engine compiles, its compilers
// and passes among them, as cache.BuildID does for a whole executable. Reusing
// one across two builds serves one's documents to the other. New refuses a nil
// c and an empty build.
func WithCache(c Cache, build string) Option {
	return func(cfg *config) { cfg.cache = &cacheBinding{store: c, build: build, workingDir: os.Getwd} }
}

// check refuses a binding no run could use.
func (b *cacheBinding) check() error {
	if b.store == nil {
		return errors.New("engine: cache is nil")
	}
	if b.build == "" {
		return errors.New("engine: cache has no build; a key must name what compiled the entry")
	}
	return nil
}

// cacheable reports whether a run under opts takes part in the cache. A
// verified run exists to run its stages, and a value of a compiler's own options
// type says nothing the engine can key on.
func cacheable(opts RunOptions) bool {
	return !opts.Verify && opts.FormatOptions == nil
}

// compileCached is compile, answered from the engine's cache where the run takes
// part, which skips detection too: that parses the spec. A failed key, lookup or
// store costs the work and goes on Result.CacheErr, and never fails the run.
func (e *Engine) compileCached(ctx context.Context, spec compilers.Source, opts RunOptions,
	docs compilers.DocumentReader, p plan,
) (*Result, error) {
	if e.cache == nil || !cacheable(opts) {
		return e.compile(ctx, spec, configurer(opts, docs), p)
	}
	key, keyErr := e.cache.runKey(spec, opts, p)
	if keyErr != nil {
		res, err := e.compile(ctx, spec, configurer(opts, docs), p)
		if err == nil {
			res.CacheErr = keyErr
		}
		return res, err
	}
	res, hit, lookupErr := e.cache.fetch(ctx, key, docs)
	if hit {
		// A run cancelled meanwhile returns no document, as one cancelled in a
		// stage does.
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("engine: stop after the cache: %w", err)
		}
		return res, nil
	}
	reads := newRecorder(docs)
	res, err := e.compile(ctx, spec, configurer(opts, reads), p)
	if err != nil {
		return nil, err
	}
	res.CacheErr = errors.Join(lookupErr, e.cache.keep(ctx, key, res, reads))
	return res, nil
}

// fetch answers a run from the entry named key. ok is false for no entry, for
// one a changed document outdates, and for one that fails, which err then says.
// Rereading costs what the compile's reads cost, the network's among them.
func (b *cacheBinding) fetch(ctx context.Context, key cache.Key, docs compilers.DocumentReader) (*Result, bool, error) {
	entry, ok, err := b.load(ctx, key)
	if err != nil || !ok {
		return nil, false, err
	}
	head, body, err := decodeEntry(key, entry)
	if err != nil {
		return nil, false, fmt.Errorf("engine: cache: entry %s: %w", key, err)
	}
	if !readsUnchanged(ctx, docs, head.Reads) {
		return nil, false, nil
	}
	var doc ir.Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, false, fmt.Errorf("engine: cache: entry %s: %w", key, err)
	}
	return &Result{Document: &doc, Diagnostics: doc.Diagnostics, Sources: doc.Sources, Format: head.Format}, true, nil
}

// load asks the store for key, behind a barrier: a Cache is the caller's code,
// and the run does not depend on it.
func (b *cacheBinding) load(ctx context.Context, key cache.Key) ([]byte, bool, error) {
	var (
		entry []byte
		ok    bool
		err   error
	)
	if perr := barrier.Catch(func() { entry, ok, err = b.store.Load(ctx, key) }); perr != nil {
		err = perr
	}
	if err != nil {
		return nil, false, fmt.Errorf("engine: cache: load: %w", err)
	}
	return entry, ok, nil
}

// keep stores what a run compiled. A run that compiled nothing, or that read
// something the entry could not vouch for, leaves no entry.
func (b *cacheBinding) keep(ctx context.Context, key cache.Key, res *Result, reads *recorder) error {
	listed, sound := reads.settled()
	if res.Document == nil || !sound {
		return nil
	}
	entry, err := encodeEntry(key, res, listed)
	if err != nil {
		return fmt.Errorf("engine: cache: %w", err)
	}
	if perr := barrier.Catch(func() { err = b.store.Store(ctx, key, entry) }); perr != nil {
		err = perr
	}
	if err != nil {
		return fmt.Errorf("engine: cache: store: %w", err)
	}
	return nil
}
