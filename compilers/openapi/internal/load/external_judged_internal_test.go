// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// scriptedReader answers each request by a function of it, and records the
// reach of each request by its location.
type scriptedReader struct {
	answer func(compilers.DocumentRequest) (compilers.DocumentRead, error)
	mu     sync.Mutex
	reqs   []compilers.DocumentRequest
}

// ReadDocument implements compilers.DocumentReader.
func (r *scriptedReader) ReadDocument(_ context.Context, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	return r.answer(req)
}

// asked returns the reach each request for location was made at.
func (r *scriptedReader) asked(location string) []compilers.AddressSpace {
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

// externalWithin reads through documentReader beside a source at self, charging each
// document it reads to documents.
func externalWithin(documentReader compilers.DocumentReader, self string, documents budget.Limit) *external {
	src := sourceDocument{path: self, judged: judgment{checker: specrules.NewChecker()}}
	return newExternal(Options{Reader: documentReader}, newExternalReads(src, documents), &aside{})
}

const (
	judgedA = "https://pub.example/a.yaml"
	judgedB = "https://pub.example/b.yaml"
	judgedC = "http://10.0.0.5/c.yaml"
)

// TestExternal_AStatedBoundPastTheReachRefusesTheReference pins the judgement
// of a document whose read stated no bound when it was first read: asked again
// at the reach of a public document's reference, the read's own bound, past
// that reach, refuses the reference, though the read itself succeeded.
func TestExternal_AStatedBoundPastTheReachRefusesTheReference(t *testing.T) {
	t.Parallel()
	scriptedReaderValue := &scriptedReader{answer: func(req compilers.DocumentRequest) (compilers.DocumentRead, error) {
		switch {
		case req.Location == judgedA && req.Reach != compilers.SpaceUnstated:
			return compilers.DocumentRead{Data: []byte("X: {type: string}\n"), Space: compilers.SpacePublic, Bound: compilers.SpacePrivate}, nil
		case req.Location == judgedA:
			return compilers.DocumentRead{Data: []byte("X: {type: string}\n")}, nil
		}
		return compilers.DocumentRead{Data: []byte("Y: {type: string}\n"), Space: compilers.SpacePublic, Bound: compilers.SpacePublic}, nil
	}}
	external := externalWithin(scriptedReaderValue, "spec.yaml", budget.Unbounded())
	_, err := external.document(t.Context(), judgedA, "spec.yaml")
	require.NoError(t, err, "the source reads it as far as it likes")
	_, err = external.document(t.Context(), judgedB, "spec.yaml")
	require.NoError(t, err)

	_, err = external.document(t.Context(), judgedA, judgedB)

	barredWith(t, err, "it is at a private address, which a reference in a document read from a public one does not reach")
	assert.Equal(t, []compilers.AddressSpace{compilers.SpaceUnstated, compilers.SpacePublic}, scriptedReaderValue.asked(judgedA),
		"the judging read was made at the asker's reach")
}

// TestExternal_AnUnparsableDocumentAtAPrivateAddressIsRefusedToAPublicAsker
// pins that a document whose read stated where it was, and failed to parse,
// fails its first asker with the parse error and a public asker with the
// address refusal alone, judged by what the first read stated, unread.
func TestExternal_AnUnparsableDocumentAtAPrivateAddressIsRefusedToAPublicAsker(t *testing.T) {
	t.Parallel()
	scriptedReaderValue := &scriptedReader{answer: func(req compilers.DocumentRequest) (compilers.DocumentRead, error) {
		if req.Location == judgedC {
			if req.Reach != compilers.SpaceUnstated && req.Reach < compilers.SpacePrivate {
				return compilers.DocumentRead{}, &compilers.AddressSpaceError{Address: req.Location, Space: compilers.SpacePrivate}
			}
			return compilers.DocumentRead{Data: []byte("{unclosed: ["), Space: compilers.SpacePrivate, Bound: compilers.SpacePrivate}, nil
		}
		return compilers.DocumentRead{Data: []byte("Y: {type: string}\n"), Space: compilers.SpacePublic, Bound: compilers.SpacePublic}, nil
	}}
	external := externalWithin(scriptedReaderValue, "spec.yaml", budget.Unbounded())
	_, err := external.document(t.Context(), judgedC, "spec.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse", "the first asker is told why it failed")
	_, err = external.document(t.Context(), judgedB, "spec.yaml")
	require.NoError(t, err)

	_, err = external.document(t.Context(), judgedC, judgedB)

	barredWith(t, err, "it is at a private address, which a reference in a document read from a public one does not reach")
	assert.NotContains(t, err.Error(), "parse", "the public asker is not told how the document fails")
	assert.Equal(t, []compilers.AddressSpace{compilers.SpaceUnstated}, scriptedReaderValue.asked(judgedC),
		"the stated bound judged it, with no read made for the asker")
}

// TestExternal_ADocumentRefusedToOneAskerAndReadByAnotherIsChargedOnce pins
// that the document budget counts documents: one refused to a public asker
// unread and then read for a wider one is a single document, so a budget of
// one admits it and reports nothing, and a budget of none refuses it.
func TestExternal_ADocumentRefusedToOneAskerAndReadByAnotherIsChargedOnce(t *testing.T) {
	t.Parallel()
	const self = "https://pub.example/spec.yaml"
	type asked struct {
		e                   *external
		asPublic, asFileErr error
	}
	read := func(limit budget.Limit) asked {
		external := externalWithin(spacedReads(), self, limit)
		_, asPublic := external.document(t.Context(), reachPriv, self)
		_, asFile := external.document(t.Context(), reachPriv, "spec.yaml")
		return asked{e: external, asPublic: asPublic, asFileErr: asFile}
	}

	got := read(budget.AtMost(1))
	externalValue := got.e
	barredWith(t, got.asPublic, "private address")
	require.NoError(t, got.asFileErr, "the second ask is the same document, already charged")
	assert.Equal(t, 1, externalValue.read.documents.Spent())
	assert.Empty(t, externalValue.read.exhausted(provenance.Doc{Source: ir.NoSource}), "a budget of one is not crossed by one document")

	got = read(budget.AtMost(0))
	externalValue = got.e
	require.Error(t, got.asPublic)
	assert.Contains(t, got.asPublic.Error(), "past the 0-document budget")
	assert.NotEmpty(t, externalValue.read.exhausted(provenance.Doc{Source: ir.NoSource}), "a budget of none is crossed by one")
}
