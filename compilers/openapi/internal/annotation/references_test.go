// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/ir"
)

// TestWritesReference_FindsAReferenceMemberAtAnyDepth pins what a base can
// change: a `$ref` or `$dynamicRef` named as a member, however deep, whose value
// is no absolute URI, and never one spelled only as a string value. A value
// that is no string, and text that does not decode, count, since the comparison
// it guards errs towards the bases.
func TestWritesReference_FindsAReferenceMemberAtAnyDepth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{`{"$ref":"n.json"}`, true},
		{`{"not":{"anyOf":[{"type":"string"},{"$ref":"#/$defs/a"}]}}`, true},
		{`{"items":{"$dynamicRef":"#node"}}`, true},
		{`{"default":{"$ref":"data"}}`, true},
		{`{"minLength":1}`, false},
		{`{"const":"$ref","enum":["$dynamicRef"]}`, false},
		{`["$ref",{"a":"$ref"}]`, false},
		{`{"$refs":1,"ref":2}`, false},
		{``, false},
		{`{"a":`, true},
		{`{"$ref":`, true},
		{`{"$ref":{"a":1}}`, true},
		{`{"$ref":"https://ex.test/n.json"}`, false},
		{`{"not":{"$dynamicRef":"https://ex.test/n.json#node"},"$ref":"n.json"}`, true},
		{`{"$dynamicRef":"urn:x:n#node"}`, false},
		{`{"$ref":"c:/n.json"}`, true},
	} {
		assert.Equal(t, tc.want, annotation.WritesReference(ir.RawValue(tc.raw)), tc.raw)
	}
}
