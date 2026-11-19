// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// plainKinds are the objects of the rule table the binder reads as an object it
// holds no state for: none places a schema or a reference object.
var plainKinds = []specrules.Kind{
	specrules.Info, specrules.Contact, specrules.License, specrules.Server, specrules.ServerVariable, specrules.ExternalDocs, specrules.Tag,
	specrules.Reference, specrules.Discriminator, specrules.XML, specrules.OAuthFlows, specrules.OAuthFlow,
}

// tableObjects returns each object of the rule table, by its kind.
func tableObjects(t *testing.T) map[specrules.Kind]specrules.Object {
	t.Helper()
	spec, out := specrules.Table(), map[specrules.Kind]specrules.Object{}
	for k := range math.MaxUint8 + 1 {
		if o, ok := spec.Object(specrules.Kind(k)); ok {
			out[specrules.Kind(k)] = o
		}
	}
	require.NotEmpty(t, out)
	return out
}

// TestObjectFields_ReadsEveryObjectOfTheTable holds kindStates to the rule
// table: each object of it has a state of its own or is one plainKinds names,
// never both, and each kind with a state is an object of it, so no object is
// read as a plain one by being left out. Each field of a plain one leads to a
// value or a plain object, so none is left out that places a schema or a
// reference object.
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
		for _, f := range objects[kind].Fields {
			assert.Equal(t, sValue, class(leads(f.Shape, f.Of)), "%s.%s", objects[kind].Name, f.Name)
		}
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
		admits := o.Referenceable != (specrules.MinorVersionRange{}) || slices.ContainsFunc(o.Fields, func(f specrules.Field) bool {
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
		shape specrules.Shape
		of    specrules.Kind
		want  state
	}{
		{specrules.AnyShapeUnchecked, 0, sRaw},
		{specrules.String, 0, sValue},
		{specrules.Boolean, 0, sValue},
		{specrules.Number, 0, sValue},
		{specrules.PositiveNumber, 0, sValue},
		{specrules.Natural, 0, sValue},
		{specrules.StringList, 0, sObject},
		{specrules.StringMap, 0, sObject},
		{specrules.RawMap, 0, sRawEntries},
		{specrules.RawList, 0, sRawEntries},
		{specrules.Nested, specrules.Encoding, sEncoding},
		{specrules.Nested, specrules.Info, sObject},
		{specrules.Map, specrules.Header, sHeaderMap},
		{specrules.Map, specrules.Schema, sSchemaMap},
		{specrules.Map, specrules.Responses, sObject},
		{specrules.Map, specrules.Server, sObject},
		{specrules.List, specrules.Encoding, sEncodingList},
		{specrules.List, specrules.SecurityRequirement, sSecurityList},
		{specrules.List, specrules.Header, sObject},
		{specrules.List, specrules.Tag, sObject},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, leads(tc.shape, tc.of), "shape %d of kind %d", tc.shape, tc.of)
	}
}
