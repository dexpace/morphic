// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/cache"
	"github.com/dexpace/morphic/ir"
)

// Cache keeps what runs compiled, for WithCache. It holds opaque bytes, and
// overlapping calls must be safe; cache.Dir is the implementation on disk. An
// entry may be lost or damaged at any time, which costs a compile and never a
// wrong document. It is trusted as the executable is: a hash tells damage from
// a sound entry, not a forgery from a compile.
type Cache interface {
	// Load returns the entry named key, and false when there is none.
	Load(ctx context.Context, key cache.Key) (entry []byte, ok bool, err error)
	// Store keeps entry as the one named key, replacing any there.
	Store(ctx context.Context, key cache.Key, entry []byte) error
}

// entrySeparator ends an entry's head. A head is compact JSON, which holds no
// raw newline, so the first one is always the end of it.
const entrySeparator = '\n'

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

// compileCached is compile, answered from the engine's cache where the run lets
// it be, which skips detection too: that parses the spec. A failed lookup or
// store costs the work and goes on Result.CacheErr, and never fails the run.
func (e *Engine) compileCached(ctx context.Context, spec compilers.Source, opts RunOptions,
	docs compilers.DocumentReader, p plan,
) (*Result, error) {
	// A verified run exists to run its stages, and a value of a compiler's own
	// options type says nothing the engine can key on.
	if e.cache == nil || opts.Verify || opts.FormatOptions != nil {
		return e.compile(ctx, spec, configurer(opts, docs), p)
	}
	key, cacheErr := e.cache.keyOf(spec, opts, p)
	keyed := cacheErr == nil
	if keyed {
		res, hit, err := e.cache.fetch(ctx, key, docs)
		if hit {
			return res, ctxAfterCache(ctx)
		}
		cacheErr = err
	}
	reads := &recorder{docs: docs, reads: map[compilers.DocumentRequest]string{}}
	res, err := e.compile(ctx, spec, configurer(opts, reads), p)
	if err != nil {
		return nil, err
	}
	if keyed {
		cacheErr = errors.Join(cacheErr, e.cache.keep(ctx, key, res, reads))
	}
	res.CacheErr = cacheErr
	return res, nil
}

// ctxAfterCache is ctx's error as a run ending at the cache fails with: a run
// cancelled meanwhile returns no document, as one cancelled in a stage does.
func ctxAfterCache(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("engine: stop after the cache: %w", err)
	}
	return nil
}

// keyOf derives the key of a run: everything the document depends on but the
// documents it reads, which no key made before the compile can name, so an
// entry lists them instead (entryHead.Reads). FailOn is absent: the verdict is
// judged afresh. The format is too: it follows from the bytes, and finding it
// is what a hit skips. The working directory is present because a compile reads
// it, to relate a relative path to an absolute one, and so reaches type IDs
// (#1297).
func (b *cacheBinding) keyOf(spec compilers.Source, opts RunOptions, p plan) (cache.Key, error) {
	dir, err := b.workingDir()
	if err != nil {
		return cache.Key{}, fmt.Errorf("engine: cache: %w", err)
	}
	passes := make([]string, 0, len(p.passes))
	for _, np := range p.passes {
		passes = append(passes, np.name)
	}
	return keyFor(keyInputs{
		build: b.build, irVersion: ir.IRVersion, dir: dir, path: spec.Path, specHash: ir.SourceHash(spec.Data),
		passes: passes, settings: opts.CompilerOptions,
	}), nil
}

// keyInputs is what a run's key covers, a field per input.
type keyInputs struct {
	build, irVersion, dir, path, specHash string
	passes                                []string
	settings                              map[string]string
}

// keyFor frames in as cache.KeyOf wants: the passes after their count, then the
// settings, last and so needing none, in sorted order.
func keyFor(in keyInputs) cache.Key {
	fields := []string{in.build, in.irVersion, in.dir, in.path, in.specHash, strconv.Itoa(len(in.passes))}
	fields = append(fields, in.passes...)
	for _, name := range slices.Sorted(maps.Keys(in.settings)) {
		fields = append(fields, name, in.settings[name])
	}
	return cache.KeyOf(fields...)
}

// entryHead is the first line of an entry: what a run needs to tell whether the
// document after it still answers, ahead of decoding that document.
type entryHead struct {
	// Format is Result.Format, which a hit does not detect.
	Format compilers.SourceFormat `json:"format"`
	// Reads is every document the compile read. An entry answers only a run
	// reading the same bytes from each.
	Reads []documentRead `json:"reads,omitempty"`
	// Hash is the document's bytes as ir.SourceHash spells them, which is what
	// tells a damaged entry from a sound one.
	Hash string `json:"hash"`
}

// documentRead is one read a compile made, and the bytes it got.
type documentRead struct {
	Request compilers.DocumentRequest `json:"request"`
	Hash    string                    `json:"hash"`
}

// fetch answers a run from the entry named key. ok is false for no entry, for
// one a changed document outdates, and for one that fails, which err then says.
// Rereading costs what the compile's reads cost, the network's among them.
func (b *cacheBinding) fetch(ctx context.Context, key cache.Key, docs compilers.DocumentReader) (*Result, bool, error) {
	entry, ok, err := b.load(ctx, key)
	if err != nil || !ok {
		return nil, false, err
	}
	head, body, err := decodeEntry(entry)
	if err != nil {
		return nil, false, fmt.Errorf("engine: cache: entry %s: %w", key, err)
	}
	if !stillRead(ctx, docs, head.Reads) {
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
		return nil, false, fmt.Errorf("engine: cache: load: %w", perr)
	}
	return entry, ok, err
}

// keep stores what a run compiled. A run that compiled nothing, or that read
// something the entry could not vouch for, leaves no entry.
func (b *cacheBinding) keep(ctx context.Context, key cache.Key, res *Result, reads *recorder) error {
	listed, sound := reads.settled()
	if res.Document == nil || !sound {
		return nil
	}
	entry, err := encodeEntry(res, listed)
	if err != nil {
		return fmt.Errorf("engine: cache: %w", err)
	}
	if perr := barrier.Catch(func() { err = b.store.Store(ctx, key, entry) }); perr != nil {
		return fmt.Errorf("engine: cache: store: %w", perr)
	}
	return err
}

// encodeEntry writes res as an entry: the head, then the compact document.
func encodeEntry(res *Result, reads []documentRead) ([]byte, error) {
	body, err := json.Marshal(res.Document)
	if err != nil {
		return nil, fmt.Errorf("encode document: %w", err)
	}
	head, err := json.Marshal(entryHead{Format: res.Format, Reads: reads, Hash: ir.SourceHash(body)})
	if err != nil {
		return nil, fmt.Errorf("encode head: %w", err)
	}
	return slices.Concat(head, []byte{entrySeparator}, body), nil
}

// decodeEntry splits entry into its head and document, refusing an entry whose
// document is not the one its head names. An entry cut before its separator
// fails here too: what is left is no head, or the hash of no document.
func decodeEntry(entry []byte) (entryHead, []byte, error) {
	line, body, _ := bytes.Cut(entry, []byte{entrySeparator})
	var head entryHead
	if err := json.Unmarshal(line, &head); err != nil {
		return entryHead{}, nil, fmt.Errorf("decode head: %w", err)
	}
	if ir.SourceHash(body) != head.Hash {
		return entryHead{}, nil, errors.New("document does not match its hash")
	}
	return head, body, nil
}

// stillRead reports whether each read returns what the compile got from it. A
// read that fails, ctx's end among the ways, answers no: the compile it sends
// the run to reports that as it should.
func stillRead(ctx context.Context, docs compilers.DocumentReader, reads []documentRead) bool {
	for _, read := range reads {
		data, err := docs.ReadDocument(ctx, read.Request)
		if err != nil || ir.SourceHash(data) != read.Hash {
			return false
		}
	}
	return true
}

// recorder reads through docs and notes what each read returned, which is what
// makes an entry sound: it lists what the compile actually got, whatever
// changed around it, and a compiler reads nothing but through its Reader.
type recorder struct {
	docs compilers.DocumentReader

	mu sync.Mutex
	// reads holds each request's bytes as ir.SourceHash spells them.
	reads map[compilers.DocumentRequest]string
	// unsound is set by a read no replay could vouch for: one that failed, since
	// the document may appear later and nothing stored would notice, or one that
	// answered a repeat with other bytes.
	unsound bool
}

// ReadDocument implements compilers.DocumentReader.
func (r *recorder) ReadDocument(ctx context.Context, req compilers.DocumentRequest) ([]byte, error) {
	data, err := r.docs.ReadDocument(ctx, req)
	hash := ""
	if err == nil {
		hash = ir.SourceHash(data)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seen, repeated := r.reads[req]
	r.unsound = r.unsound || err != nil || (repeated && seen != hash)
	r.reads[req] = hash
	return data, err
}

// settled returns every read in one order, and false when the record is
// unsound. A compile that read concurrently has finished by now, but the lock
// is what says so to the race detector.
func (r *recorder) settled() ([]documentRead, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reads := make([]documentRead, 0, len(r.reads))
	for req, hash := range r.reads {
		reads = append(reads, documentRead{Request: req, Hash: hash})
	}
	slices.SortFunc(reads, func(a, b documentRead) int {
		return cmp.Or(strings.Compare(a.Request.Location, b.Request.Location),
			strings.Compare(a.Request.Within, b.Request.Within), cmp.Compare(a.Request.Limit, b.Request.Limit))
	})
	return reads, !r.unsound
}
