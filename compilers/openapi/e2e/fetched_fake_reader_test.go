// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// fetchedDoc is one document a fetchedReader serves: its bytes, and the
// address space it says it was read from. A proxied one is read as a proxy
// reads a name resolving to space, or to none where that is unstated: served
// unbounded stating nothing, and bounded judged by space, stated as its bound.
type fetchedDoc struct {
	data    string
	space   compilers.AddressSpace
	proxied bool
}

// fetchedRead is one request a fetchedReader was asked for.
type fetchedRead struct {
	location string
	reach    compilers.AddressSpace
}

// fetchedReader is a DocumentReader that serves URLs from a map, as a
// network would, with no network: it fails a request whose Reach is stated
// and lies below a document's space with compilers.ErrAddressSpace, and
// records every request it serves and every one it refuses. This file is the
// one place the test reader meets the reader contract.
type fetchedReader struct {
	docs map[string]fetchedDoc
	// slow makes a location answer after the delay, to bias which of two
	// reads a compile sees first.
	slow map[string]time.Duration
	// unhookable fails every bounded read with compilers.ErrUnboundedRead, as
	// a reader whose transport cannot be hooked does.
	unhookable bool

	mu      sync.Mutex
	served  []fetchedRead
	refused []fetchedRead
}

// ReadDocument implements compilers.DocumentReader.
func (f *fetchedReader) ReadDocument(ctx context.Context, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	if d := f.slow[req.Location]; d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return compilers.DocumentRead{}, fmt.Errorf("read %s: %w", req.Location, ctx.Err())
		}
	}
	doc, ok := f.docs[req.Location]
	f.mu.Lock()
	defer f.mu.Unlock()
	read := fetchedRead{location: req.Location, reach: req.Reach}
	switch {
	case !ok:
		return compilers.DocumentRead{}, fmt.Errorf("no such document %s", req.Location)
	case req.Reach != compilers.SpaceUnstated && (f.unhookable || doc.proxied && doc.space == compilers.SpaceUnstated):
		f.refused = append(f.refused, read)
		return compilers.DocumentRead{}, compilers.ErrUnboundedRead
	case req.Reach != compilers.SpaceUnstated && doc.space > req.Reach:
		f.refused = append(f.refused, read)
		return compilers.DocumentRead{}, compilers.ErrAddressSpace
	}
	f.served = append(f.served, read)
	switch {
	case !doc.proxied:
		return compilers.DocumentRead{Data: []byte(doc.data), Space: doc.space}, nil
	case req.Reach == compilers.SpaceUnstated:
		return compilers.DocumentRead{Data: []byte(doc.data)}, nil
	}
	return compilers.DocumentRead{Data: []byte(doc.data), Bound: doc.space}, nil
}

// servedAt returns the reaches the reader served location at, in order.
func (f *fetchedReader) servedAt(location string) []compilers.AddressSpace {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []compilers.AddressSpace
	for _, r := range f.served {
		if r.location == location {
			out = append(out, r.reach)
		}
	}
	return out
}

// refusedAt returns the reaches the reader refused location at, in order.
func (f *fetchedReader) refusedAt(location string) []compilers.AddressSpace {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []compilers.AddressSpace
	for _, r := range f.refused {
		if r.location == location {
			out = append(out, r.reach)
		}
	}
	return out
}

// compileFetched compiles src as the one source, named spec.yaml (or read
// from location, when set), reading what it names through reads, with external
// references allowed.
func compileFetched(t *testing.T, reads *fetchedReader, src, location string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Location: location, Data: []byte(src)}},
		compilers.Options{Reader: reads, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err, "a spec problem is a diagnostic, not a Go error")
	require.NotNil(t, doc)
	return doc, diags
}

// fetchedFindings renders each diagnostic as severity, code, the source it
// lies in and its pointer, which the findings of a compile naming several
// documents are told apart by.
func fetchedFindings(doc *ir.Document, diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		path := "?"
		if d.Provenance.Source >= 0 && d.Provenance.Source < len(doc.Sources) {
			path = doc.Sources[d.Provenance.Source].Path
		}
		out = append(out, fmt.Sprintf("%s %s %s %s", d.Severity, d.Code, path, d.Provenance.Pointer))
	}
	return out
}

// fetchedTypeNamed returns the ID of the one type whose ID ends in suffix, and
// "" for none.
func fetchedTypeNamed(doc *ir.Document, suffix string) ir.TypeID {
	var found ir.TypeID
	for id := range doc.Types {
		if len(id) >= len(suffix) && string(id[len(id)-len(suffix):]) == suffix {
			found = id
		}
	}
	return found
}
