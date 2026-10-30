// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestEncoded_IsTheDocumentsCanonicalJSON pins that Encoded is the document's
// own encoding, not a rendering of its own.
func TestEncoded_IsTheDocumentsCanonicalJSON(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(irtest.ScalarOf("t/x/S", ""))
	doc.IRVersion = ir.IRVersion
	want, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Equal(t, string(want), irtest.Encoded(t, doc))
}

// TestEncoded_FailsTheTestOnADocumentThatDoesNotEncode drives the failure on
// its own goroutine, since the recorder's Fatalf unwinds the one it runs on.
func TestEncoded_FailsTheTestOnADocumentThatDoesNotEncode(t *testing.T) {
	t.Parallel()
	rec := &recordingT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		irtest.EncodedOn(rec, badExtDoc())
	}()
	<-done
	assert.True(t, rec.aborted)
	assert.Contains(t, rec.fatalf, "encode the document")
	assert.GreaterOrEqual(t, rec.helperCalls, 1, "encoded marks itself a helper")
}

// TestRefusedAsCyclic_ReadsOnlyTheCycleRefusal pins both halves: a value that
// contains itself is refused as a cycle, and neither a document that encodes
// nor one refused for another reason is.
func TestRefusedAsCyclic_ReadsOnlyTheCycleRefusal(t *testing.T) {
	t.Parallel()
	c := &ir.CtorValue{Scalar: "t/x/S", Name: "from"}
	self := ir.Value{Kind: ir.ValueCtor, Ctor: c}
	c.Args = []ir.Value{self}
	cyclic := irtest.DocOf(irtest.ModelOf("t/x/M", func(m *ir.Model) {
		m.Examples = []ir.Example{{Value: &self}}
	}))
	cyclic.IRVersion = ir.IRVersion

	assert.True(t, irtest.RefusedAsCyclic(cyclic))
	assert.False(t, irtest.RefusedAsCyclic(&ir.Document{IRVersion: ir.IRVersion}))
	assert.False(t, irtest.RefusedAsCyclic(badExtDoc()), "refused, but not as a cycle")
}
