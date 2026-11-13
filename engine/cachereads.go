// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// documentRead is one read a compile made, and the bytes it got.
type documentRead struct {
	Request compilers.DocumentRequest `json:"request"`
	Hash    string                    `json:"hash"`
}

// readsUnchanged reports whether each read returns what the compile got from it.
// A read that fails, ctx's end among the ways, answers no: the compile it sends
// the run to reports that as it should.
func readsUnchanged(ctx context.Context, docs compilers.DocumentReader, reads []documentRead) bool {
	for _, read := range reads {
		data, err := docs.ReadDocument(ctx, read.Request)
		if err != nil || ir.SourceHash(data) != read.Hash {
			return false
		}
	}
	return true
}

// locatedDirectory is one directory a compile asked its reader to locate, and
// where the reader placed it.
type locatedDirectory struct {
	Directory string `json:"directory"`
	Located   string `json:"located"`
}

// directoriesUnchanged reports whether docs places each directory where the
// compile's reader placed it: a link retargeted since moves the documents a
// compile names through it, and the labels their IDs carry.
func directoriesUnchanged(docs compilers.DocumentReader, dirs []locatedDirectory) bool {
	for _, d := range dirs {
		if locateDirectory(docs, d.Directory) != d.Located {
			return false
		}
	}
	return true
}

// locateDirectory is where docs places dir (compilers.DirectoryLocator), or
// dir itself for a reader placing none.
func locateDirectory(docs compilers.DocumentReader, dir string) string {
	if l, ok := docs.(compilers.DirectoryLocator); ok {
		return l.LocateDirectory(dir)
	}
	return dir
}

// recorder reads through docs and notes what each read returned, and where
// docs placed each directory, which is what makes an entry sound: it lists
// what the compile actually got, whatever changed around it, and a compiler
// reads and places nothing but through its Reader.
type recorder struct {
	docs compilers.DocumentReader

	mu sync.Mutex
	// reads holds each request's bytes as ir.SourceHash spells them.
	reads map[compilers.DocumentRequest]string
	// dirs holds where docs placed each directory asked of it.
	dirs map[string]string
	// unsound is set by a read no replay could vouch for: one that failed, since
	// the document may appear later and nothing stored would notice, or one that
	// answered a repeat with other bytes.
	unsound bool
}

// newRecorder returns a recorder reading through docs, with nothing read yet.
func newRecorder(docs compilers.DocumentReader) *recorder {
	return &recorder{docs: docs, reads: map[compilers.DocumentRequest]string{}, dirs: map[string]string{}}
}

// LocateDirectory implements compilers.DirectoryLocator as docs does, noting
// the answer.
func (r *recorder) LocateDirectory(dir string) string {
	located := locateDirectory(r.docs, dir)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirs[dir] = located
	return located
}

// directories returns every directory placed, in one order.
func (r *recorder) directories() []locatedDirectory {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]locatedDirectory, 0, len(r.dirs))
	for dir, located := range r.dirs {
		out = append(out, locatedDirectory{Directory: dir, Located: located})
	}
	slices.SortFunc(out, func(a, b locatedDirectory) int { return strings.Compare(a.Directory, b.Directory) })
	return out
}

// ReadDocument implements compilers.DocumentReader.
func (r *recorder) ReadDocument(ctx context.Context, req compilers.DocumentRequest) ([]byte, error) {
	data, err := r.docs.ReadDocument(ctx, req)
	var hash string
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
