// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// plainKinds are the objects of the rule table the binder reads as an object it
// holds no state for: none places a schema or a reference object.
var plainKinds = []oas.Kind{
	oas.Info, oas.Contact, oas.License, oas.Server, oas.ServerVariable, oas.ExternalDocs, oas.Tag,
	oas.Reference, oas.Discriminator, oas.XML, oas.OAuthFlows, oas.OAuthFlow,
}

// tableObjects returns each object of the rule table, by its kind.
func tableObjects(t *testing.T) map[oas.Kind]oas.Object {
	t.Helper()
	spec, out := oas.Table(), map[oas.Kind]oas.Object{}
	for k := range math.MaxUint8 + 1 {
		if o, ok := spec.Object(oas.Kind(k)); ok {
			out[oas.Kind(k)] = o
		}
	}
	require.NotEmpty(t, out)
	return out
}

// TestObjectFields_ReadsEveryObjectOfTheTable holds kindStates to the rule
// table: each object of it has a state of its own or is one plainKinds names,
// never both, and each kind with a state is an object of it, so no object is
// read as a plain one by being left out.
func TestObjectFields_ReadsEveryObjectOfTheTable(t *testing.T) {
	t.Parallel()
	objects := tableObjects(t)
	for kind, o := range objects {
		_, stated := kindStates[kind]
		assert.NotEqual(t, stated, slices.Contains(plainKinds, kind), "%s has a state or is plain", o.Name)
	}
	for kind := range kindStates {
		assert.Contains(t, objects, kind)
	}
	for _, kind := range plainKinds {
		assert.Contains(t, objects, kind)
	}
}

// TestObjectFields_ReadsAReferenceWhereTheTableAdmitsOne holds refKinds to the
// rule table, both ways: an object state is read as a reference object where
// it writes a $ref exactly where its object is one a Reference Object may
// stand in for at some minor, or defines a $ref of its own, as a Path Item
// Object does. A schema's $ref is a site of its own (finder.visit).
func TestObjectFields_ReadsAReferenceWhereTheTableAdmitsOne(t *testing.T) {
	t.Parallel()
	for kind, o := range tableObjects(t) {
		s, stated := kindStates[kind]
		if !stated || s == sSchema {
			continue
		}
		admits := o.Referenceable != (oas.Span{}) || slices.ContainsFunc(o.Fields, func(f oas.Field) bool {
			return f.Name == "$ref"
		})
		_, refers := refKinds[s]
		assert.Equal(t, admits, refers, o.Name)
	}
}

// TestObjectFields_LeadsEachShapeWhereItsValueIsRead pins leads, a row a shape:
// a scalar is a value, an unchecked value raw YAML, a list or map of strings an
// object, and a nested object, a map or a list of a kind the binder reads its
// state or the container of it, else an object.
func TestObjectFields_LeadsEachShapeWhereItsValueIsRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		shape oas.Shape
		of    oas.Kind
		want  state
	}{
		{oas.Any, 0, sRaw},
		{oas.String, 0, sValue},
		{oas.Boolean, 0, sValue},
		{oas.Number, 0, sValue},
		{oas.PositiveNumber, 0, sValue},
		{oas.Natural, 0, sValue},
		{oas.StringList, 0, sObject},
		{oas.StringMap, 0, sObject},
		{oas.RawMap, 0, sRawEntries},
		{oas.RawList, 0, sRawEntries},
		{oas.Nested, oas.Encoding, sEncoding},
		{oas.Nested, oas.Info, sObject},
		{oas.Map, oas.Header, sHeaderMap},
		{oas.Map, oas.Schema, sSchemaMap},
		{oas.Map, oas.Responses, sObject},
		{oas.Map, oas.Server, sObject},
		{oas.List, oas.Encoding, sEncodingList},
		{oas.List, oas.SecurityRequirement, sSecurityList},
		{oas.List, oas.Header, sObject},
		{oas.List, oas.Tag, sObject},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, leads(tc.shape, tc.of), "shape %d of kind %d", tc.shape, tc.of)
	}
}
