// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// spacedDoc is a document a spacedReader serves, and where it lies.
type spacedDoc struct {
	data  string
	space compilers.AddressSpace
	// fails is what a bounded read of it fails with, where it is set.
	fails error
}

// spacedReader serves documents by location, refusing a bounded read of one
// past the request's reach as a reader judging its dial does, and records
// each request.
type spacedReader struct {
	docs map[string]spacedDoc
	mu   sync.Mutex
	reqs []compilers.DocumentRequest
}

// ReadDocument implements compilers.DocumentReader.
func (r *spacedReader) ReadDocument(_ context.Context, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req)
	doc, ok := r.docs[req.Location]
	switch {
	case !ok:
		return compilers.DocumentRead{}, fmt.Errorf("no document %s", req.Location)
	case req.Reach != compilers.SpaceUnstated && doc.fails != nil:
		return compilers.DocumentRead{}, doc.fails
	case req.Reach != compilers.SpaceUnstated && doc.space > req.Reach:
		return compilers.DocumentRead{}, &compilers.AddressSpaceError{Address: req.Location, Space: doc.space}
	}
	return compilers.DocumentRead{Data: []byte(doc.data), Space: doc.space}, nil
}

// asked returns the reach each request for location was made at.
func (r *spacedReader) asked(location string) []compilers.AddressSpace {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []compilers.AddressSpace
	for _, req := range r.reqs {
		if req.Location == location {
			out = append(out, req.Reach)
		}
	}
	return out
}

const (
	reachPub    = "https://pub.example/a.yaml"
	reachPriv   = "http://10.0.0.5/s.yaml"
	reachLocal  = "http://127.0.0.1/l.yaml"
	reachMute   = "https://pub.example/mute.yaml"
	reachUnhook = "https://pub.example/unhook.yaml"
)

// spacedReads is the reader every reach test reads through.
func spacedReads() *spacedReader {
	return &spacedReader{docs: map[string]spacedDoc{
		reachPub:    {data: "X: {type: string}\n", space: compilers.SpacePublic},
		reachPriv:   {data: "S: {type: string}\n", space: compilers.SpacePrivate},
		reachLocal:  {data: "L: {type: string}\n", space: compilers.SpaceLocal, fails: compilers.ErrAddressSpace},
		reachMute:   {data: "M: {type: string}\n"},
		reachUnhook: {data: "U: {type: string}\n", space: compilers.SpacePublic, fails: compilers.ErrUnboundedRead},
	}}
}

// spacedExternal reads through spacedReaderValue beside a source at self.
func spacedExternal(spacedReaderValue *spacedReader, self string) *external {
	src := sourceDocument{path: self, judged: judgment{checker: specrules.NewChecker()}}
	return newExternal(Options{Reader: spacedReaderValue}, newExternalReads(src, budget.Unbounded()), &aside{})
}

// barredWith asserts err refuses to its asker alone, for why.
func barredWith(t *testing.T, err error, why string) {
	t.Helper()
	var barred *reftable.BarredError
	require.ErrorAs(t, err, &barred)
	assert.Contains(t, barred.Error(), why)
}

// TestExternal_AFetchedDocumentReadsNoMorePrivateAddressThanItsOwn pins the
// bound on what a reference in a document read over http(s) reads: no address
// more private than the one its document was read from, refused to that asker
// alone, asked of the reader once per reach that could change its answer, and
// read for a wider asker.
func TestExternal_AFetchedDocumentReadsNoMorePrivateAddressThanItsOwn(t *testing.T) {
	t.Parallel()
	spacedReader := spacedReads()
	external := spacedExternal(spacedReader, "spec.yaml")
	_, err := external.document(t.Context(), reachPub, "spec.yaml")
	require.NoError(t, err)

	_, err = external.document(t.Context(), reachPriv, reachPub)
	barredWith(t, err, "it is at a private address, which a reference in a document read from a public one does not reach")
	_, err = external.document(t.Context(), reachPriv, reachPub)
	barredWith(t, err, "private address")
	assert.Equal(t, []compilers.AddressSpace{compilers.SpacePublic}, spacedReader.asked(reachPriv), "refused once, unread")

	_, err = external.document(t.Context(), reachPriv, "spec.yaml")
	require.NoError(t, err, "a wider asker reads it")
	_, err = external.document(t.Context(), reachPriv, reachPub)
	barredWith(t, err, "private address")
	assert.Len(t, spacedReader.asked(reachPriv), 2, "judged as read, asking nothing more")

	_, err = external.document(t.Context(), reachPub, reachPriv)
	require.NoError(t, err, "a private document reaches a public one read before")

	_, err = external.document(t.Context(), reachLocal, reachPriv)
	barredWith(t, err, "it is at an address more private than a reference in a document read from a private one reaches")
	_, err = external.document(t.Context(), reachUnhook, reachPub)
	barredWith(t, err, "the reader cannot bound the addresses its read dials")
	_, err = external.document(t.Context(), reachUnhook, "spec.yaml")
	require.NoError(t, err, "a reader that cannot bound a read still serves an unbounded one")
	_, err = external.document(t.Context(), reachMute, "spec.yaml")
	require.NoError(t, err)
	_, err = external.document(t.Context(), reachMute, reachPub)
	barredWith(t, err, "the reader did not say what address it was read from")
	_, err = external.document(t.Context(), reachPub, reachMute)
	require.NoError(t, err, "a document whose read stated no space is read as public")
}

// TestExternal_ASourceLocatedAtAURLIsReadAsAPublicDocument pins that a source
// handed with a URL for its location bounds its references as a public
// document's are.
func TestExternal_ASourceLocatedAtAURLIsReadAsAPublicDocument(t *testing.T) {
	t.Parallel()
	const self = "https://pub.example/spec.yaml"
	spacedReader := spacedReads()
	external := spacedExternal(spacedReader, self)

	_, err := external.document(t.Context(), reachPriv, self)

	barredWith(t, err, "private address, which a reference in a document read from a public one")
	_, err = external.document(t.Context(), reachPub, self)
	require.NoError(t, err)
	assert.Equal(t, []compilers.AddressSpace{compilers.SpacePublic}, spacedReader.asked(reachPub))
}

// TestLoad_AFetchedDocumentsReferenceIsJudgedWhereItsTargetWasRead pins the
// judgement a document read before is given for another asker, through Load:
// the source reads the private document, and the public document's reference
// to it is refused, in either order, with the reader asked nothing more.
func TestLoad_AFetchedDocumentsReferenceIsJudgedWhereItsTargetWasRead(t *testing.T) {
	t.Parallel()
	for _, schemas := range []string{
		"    A: {$ref: '" + reachPub + "#/Y'}\n    B: {$ref: '" + reachPriv + "#/S'}\n",
		"    B: {$ref: '" + reachPriv + "#/S'}\n    A: {$ref: '" + reachPub + "#/Y'}\n",
	} {
		spacedReader := spacedReads()
		pub := spacedReader.docs[reachPub]
		pub.data = "Y: {$ref: '" + reachPriv + "#/S'}\n"
		spacedReader.docs[reachPub] = pub
		src := compilers.Source{Path: "spec.yaml", Data: []byte(
			"openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" + schemas)}

		_, diags, err := Load(t.Context(), 0, src, Options{Reader: spacedReader, MaxSourceBytes: budget.Unbounded(),
			MaxSourceNodes: budget.Unbounded(), MaxAliasSurplus: budget.Unbounded(), MaxExternalDocuments: budget.Unbounded()})

		require.NoError(t, err)
		var refused []string
		for _, diagnostic := range diags {
			if diagnostic.Severity == ir.SeverityError {
				refused = append(refused, diagnostic.Message)
			}
		}
		require.Len(t, refused, 1, "%v", diags)
		assert.Contains(t, refused[0], "private address")
		for _, reach := range spacedReader.asked(reachPriv) {
			assert.NotEqual(t, compilers.SpacePublic, reach, "a refusal by where it was read asks nothing")
		}
	}
}
