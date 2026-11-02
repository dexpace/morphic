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

// newRecorder returns a recorder reading through docs, with nothing read yet.
func newRecorder(docs compilers.DocumentReader) *recorder {
	return &recorder{docs: docs, reads: map[compilers.DocumentRequest]string{}}
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
