// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/convention"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// responding is a document of one operation whose one response declares header.
func responding(header ir.Header, reg ir.TypeRegistry) *ir.Document {
	header.ID = "property/h"
	header.Provenance = ir.Provenance{Pointer: "/paths/~1a/get/responses/200/headers/h", Inferred: "extension-promotion"}
	return oneOperation(ir.Operation{ID: "operation/a", Responses: []ir.Response{{Headers: []ir.Header{header}}}}, registry(reg))
}

// dated is a header named name, holding an instant.
func dated(name string) ir.Header {
	return ir.Header{WireName: name, Type: prim(ir.PrimitiveDatetimeOffset)}
}

// TestHTTPDate_AFieldItsRFCDefinesAsADate pins which response headers are
// written and read as an HTTP-date, marked and reported: one a field an RFC
// defines so, by its wire name in any case, holding an instant, through its
// scalars, that no encoding spells otherwise, and that is one field.
func TestHTTPDate_AFieldItsRFCDefinesAsADate(t *testing.T) {
	t.Parallel()
	prefixed := dated("Date")
	prefixed.Prefix = new("Date")
	content := dated("Last-Modified")
	content.Encoding = &ir.Encoding{MediaType: "application/json"}
	named := ir.Header{Name: ir.NamingFor("Expires"), Type: prim(ir.PrimitiveDatetimeOffset)}
	scalar := dated("Last-Modified")
	scalar.Type = ir.TypeRef{Target: "type/s"}
	encoded := dated("Last-Modified")
	encoded.Type = ir.TypeRef{Target: "type/e"}
	reg := ir.TypeRegistry{
		"type/s": &ir.Scalar{ID: "type/s", Base: &ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveDatetimeOffset)}},
		"type/e": &ir.Scalar{ID: "type/e", Base: &ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveDatetimeOffset)},
			Encoding: &ir.Encoding{Name: "unixTimestamp"}},
	}
	for _, tc := range []struct {
		name    string
		h       ir.Header
		encoded bool
	}{
		{"Date", dated("Date"), true},
		{"Last-Modified in lower case", dated("last-modified"), true},
		{"Expires", dated("EXPIRES"), true},
		{"If-Modified-Since", dated("If-Modified-Since"), true},
		{"If-Unmodified-Since", dated("If-Unmodified-Since"), true},
		{"Retry-After", dated("Retry-After"), true},
		{"If-Range", dated("If-Range"), true},
		{"Sunset", dated("Sunset"), true},
		{"a name and no wire name", named, true},
		{"a scalar over an instant", scalar, true},
		{"another field", dated("X-Date"), false},
		{"a field under a prefix", prefixed, false},
		{"a content header", content, false},
		{"a scalar naming an encoding", encoded, false},
		{"a date", ir.Header{WireName: "Date", Type: prim(ir.PrimitiveDate)}, false},
		{"text", ir.Header{WireName: "Date", Type: prim(ir.PrimitiveString)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := responding(tc.h, reg)
			diags := convention.Apply(convention.Policy{}, defaults(), compileddoc.New(doc))
			header := operationOf(doc).Responses[0].Headers[0]
			if !tc.encoded {
				assert.Empty(t, diags)
				assert.Equal(t, tc.h.Encoding, header.Encoding)
				assert.Equal(t, ir.Inferred("extension-promotion"), header.Provenance.Inferred)
				return
			}
			assert.Equal(t, &ir.Encoding{Name: "http-date"}, header.Encoding)
			assert.Equal(t, ir.Inferred("extension-promotion,http-date-inference"), header.Provenance.Inferred)
			require.Len(t, diags, 1)
			assert.Equal(t, diag.InferredHTTPDate, diags[0].Code)
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
			assert.Equal(t, ir.Provenance{Pointer: "/paths/~1a/get/responses/200/headers/h"}, diags[0].Provenance,
				"reported at the header, with no marker")
		})
	}
}

// TestHTTPDate_ReadsEveryResponseAndNoPart pins where the reading runs: on
// the headers of a response, an error case and the default, and not on those
// an encoding gives a part, a MIME header no RFC 9110 field defines; and that
// with the policy off it runs on none.
func TestHTTPDate_ReadsEveryResponseAndNoPart(t *testing.T) {
	t.Parallel()
	build := func() *ir.Document {
		h := func(id ir.PropertyID) []ir.Header {
			header := dated("Last-Modified")
			header.ID = id
			return []ir.Header{header}
		}
		return oneOperation(ir.Operation{
			ID:        "operation/a",
			Request:   &ir.Payload{Contents: []ir.Content{{Encoding: map[ir.PropertyID]ir.PartEncoding{"property/part": {Headers: h("property/part")}}}}},
			Responses: []ir.Response{{Headers: h("property/response")}},
			Errors:    []ir.ErrorCase{{Headers: h("property/error")}},
			Default:   &ir.Response{Headers: h("property/default")},
		}, registry(nil))
	}
	encoded := func(doc *ir.Document) []ir.PropertyID {
		op := operationOf(doc)
		var ids []ir.PropertyID
		for _, hs := range [][]ir.Header{op.Request.Contents[0].Encoding["property/part"].Headers, op.Responses[0].Headers,
			op.Errors[0].Headers, op.Default.Headers} {
			if hs[0].Encoding != nil {
				ids = append(ids, hs[0].ID)
			}
		}
		return ids
	}

	on := build()
	assert.Len(t, convention.Apply(convention.Policy{}, defaults(), compileddoc.New(on)), 3)
	assert.Equal(t, []ir.PropertyID{"property/response", "property/error", "property/default"}, encoded(on))

	off := build()
	assert.Empty(t, convention.Apply(convention.Policy{HTTPDate: convention.HTTPDateInference{Disabled: true}}, defaults(),
		compileddoc.New(off)))
	assert.Empty(t, encoded(off))
}
