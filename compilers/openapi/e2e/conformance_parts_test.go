// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// partsOf returns a request content's part encodings by the name the last
// token of each key spells, which is the part's name in these specs, and the
// content.
func partsOf(t *testing.T, doc *ir.Document, op string) (map[string]ir.PartEncoding, ir.Content) {
	t.Helper()
	o, ok := opByName(doc, op)
	require.True(t, ok, op)
	content := requestContent(t, o)
	out := make(map[string]ir.PartEncoding, len(content.Encoding))
	for id, pe := range content.Encoding {
		name := string(id[strings.LastIndex(string(id), "/")+1:])
		require.NotContains(t, out, name, "%s: one entry per name", op)
		out[name] = pe
	}
	return out, content
}

// partOf is a part encoding stating the content type ct, empty for none, and
// the structural flags flags spells: "M" for Multi and "F" for Filename.
func partOf(ct, flags string) ir.PartEncoding {
	pe := ir.PartEncoding{Multi: strings.Contains(flags, "M"), Filename: strings.Contains(flags, "F")}
	if ct != "" {
		pe.ContentTypes = []string{ct}
	}
	return pe
}

const (
	octetStream = "application/octet-stream"
	textPlain   = "text/plain"
	jsonType    = "application/json"
)

// assertMultipartDefaults31 pins the content type a 3.1 part no Encoding
// Object configures takes by the value it applies to: JSON for an object or a
// map, text for a string, a number or a boolean, octet-stream for raw bytes, a
// contentEncoding string and an untyped value, text for format byte, which 3.1
// gives no effect, and each item's row for an array, none where two values'
// rows differ. A declared content type stands, and a style replaces the
// content type in a multipart/form-data part as in a form body. No part of a
// form body is a file.
func assertMultipartDefaults31(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	upload, _ := partsOf(t, doc, "upload")
	assert.Equal(t, map[string]ir.PartEncoding{
		"metadata": partOf(jsonType, ""), "settings": partOf(jsonType, ""), "note": partOf(textPlain, ""),
		"when": partOf(textPlain, ""), "count": partOf(textPlain, ""), "ratio": partOf(textPlain, ""),
		"enabled": partOf(textPlain, ""), "kind": partOf(textPlain, ""), "fixed": partOf(textPlain, ""),
		"file": partOf(octetStream, "F"), "token": partOf(textPlain, ""), "blob": partOf(octetStream, ""),
		"raw": partOf(octetStream, "F"), "tags": partOf(textPlain, "M"), "addresses": partOf(jsonType, "M"),
		"labels": partOf(textPlain, "M"), "nullable": partOf(textPlain, ""),
		"declared": partOf("application/xml", ""),
		"arranged": {Style: "form", Explode: new(false), Multi: true},
	}, upload)

	form, _ := partsOf(t, doc, "form")
	assert.Equal(t, map[string]ir.PartEncoding{
		"address": partOf(jsonType, ""), "id": partOf(textPlain, ""), "image": partOf(octetStream, ""),
		"ids": partOf(textPlain, "M"), "filter": {Style: "deepObject"},
	}, form)
}

// assertMultipartDefaults30 pins 3.0's rows where they differ from 3.1's: a
// string of format byte is octet-stream, which is no file, a contentEncoding
// string is text and an untyped value takes none; an array of arrays takes its
// innermost items' row; and a style, which 3.0 gives no effect in a multipart
// body, is typed nowhere there and leaves the default, kept verbatim and
// warned of, as an explode in a form body arranges the value under the
// default style.
func assertMultipartDefaults30(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	upload, content := partsOf(t, doc, "upload")
	assert.Equal(t, map[string]ir.PartEncoding{
		"file": partOf(octetStream, "F"), "token": partOf(octetStream, ""), "blob": partOf(textPlain, ""),
		"matrix": partOf(textPlain, "M"), "styled": partOf(textPlain, ""),
	}, upload, "raw takes no default, so it has no entry")
	assert.Contains(t, content.Unmodeled, "openapi:encoding/styled/style")
	at := "/paths/~1upload/post/requestBody/content/multipart~1form-data/encoding/styled/style"
	assert.Equal(t, []ir.Severity{ir.SeverityWarning}, diagsAt(diags, "openapi/validation/validation-no-effect", at))

	form, _ := partsOf(t, doc, "form")
	assert.Equal(t, map[string]ir.PartEncoding{
		"token": partOf(octetStream, ""), "tags": {Style: "form", Explode: new(false), Multi: true},
	}, form)
}

// assertMultipartDefaults32 pins 3.2's row for an array inside an array part,
// a nested encoding's key held to the properties of its part, and the items of
// a multipart sequential body taking their item's default.
func assertMultipartDefaults32(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	const media = "/paths/~1upload/post/requestBody/content/multipart~1form-data"
	upload, content := partsOf(t, doc, "upload")
	assert.Equal(t, partOf(jsonType, "M"), upload["matrix"])
	assert.Equal(t, []string{"multipart/mixed"}, upload["meta"].ContentTypes)
	assert.Contains(t, content.Unmodeled, "openapi:encoding/meta/encoding", "the nested encoding is kept verbatim")
	assert.Equal(t, []ir.Severity{ir.SeverityWarning}, diagsAt(diags, diag.UnknownEncodingKey, media+"/encoding/meta/encoding/ghost"))
	assert.Empty(t, diagsAt(diags, diag.UnknownEncodingKey, media+"/encoding/meta/encoding/a"), "a names a property of meta")

	stream, ok := opByName(doc, "stream")
	require.True(t, ok)
	item := requestContent(t, stream).ItemEncoding
	require.NotNil(t, item, "each item of a multipart stream is a part")
	assert.Equal(t, partOf(jsonType, "M"), *item)
}

// assertEncodingUnionBranches pins a part only a union's branch declares to a
// key on the variant declaring it, in each spelling: a oneOf body, an anyOf
// behind a $ref with a union nested in a branch, a union on an allOf branch
// the composition is distributed across, a multi-typed body, and a model
// narrowing a union. A name two branches declare is keyed under each, as each
// declares it. No entry is kept whole.
func assertEncodingUnionBranches(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	png := ir.PartEncoding{ContentTypes: []string{"image/png"}, Filename: true}
	for op, want := range map[string]map[string]ir.PartEncoding{
		"oneOf":      {"file": png, "url": partOf(textPlain, "")},
		"anyOf":      {"file": png, "url": partOf(textPlain, ""), "id": partOf(textPlain, "")},
		"composed":   {"targetUrl": partOf(textPlain, ""), "torrentfile": {ContentTypes: []string{"application/x-bittorrent"}, Filename: true}, "name": partOf(textPlain, "")},
		"multiTyped": {"file": {ContentTypes: []string{"image/png"}, Filename: true, Inferred: "binary-media-type"}},
		"narrowed":   {"file": png, "link": partOf(textPlain, ""), "note": partOf(textPlain, "")},
	} {
		parts, content := partsOf(t, doc, op)
		assert.Equal(t, want, parts, op)
		assert.Empty(t, content.Unmodeled, "%s: nothing is kept whole", op)
	}
	shared, ok := opByName(doc, "shared")
	require.True(t, ok)
	const schema = "p/openapi/paths/~1shared/post/requestBody/content/multipart~1form-data/schema/"
	assert.Equal(t, map[ir.PropID]ir.PartEncoding{
		schema + "oneOf/0/properties/doc": partOf(octetStream, "F"),
		schema + "oneOf/1/properties/doc": partOf(octetStream, "MF"),
	}, requestContent(t, shared).Encoding)
	for _, d := range diags {
		assert.NotEqual(t, diag.DegradedConstruct, d.Code, "no entry draws an announcement: %s", d.Message)
		assert.NotEqual(t, diag.UnknownEncodingKey, d.Code, "and no key names nothing: %s", d.Message)
	}
}

// assertEncodingOutsideForm pins each entry of an encoding under a media type
// that is neither multipart nor form, a file body's too, to being kept whole
// with one info, beside load's warning that the field has no effect there.
func assertEncodingOutsideForm(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	for op, media := range map[string]string{
		"json":  "/paths/~1json/post/requestBody/content/application~1json",
		"bytes": "/paths/~1bytes/post/requestBody/content/application~1octet-stream",
	} {
		parts, content := partsOf(t, doc, op)
		assert.Empty(t, parts, op)
		require.Contains(t, content.Unmodeled, "openapi:encoding/f", op)
		assert.Equal(t, ir.ReasonNoIRHome, content.Unmodeled["openapi:encoding/f"].Reason, op)
		assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, diag.DegradedConstruct, media+"/encoding/f"), op)
		assert.Equal(t, []ir.Severity{ir.SeverityWarning}, diagsAt(diags, "openapi/validation/validation-no-effect", media+"/encoding"), op)
	}
	assert.Empty(t, diagsAt(diags, diag.UnknownEncodingKey, "/paths/~1json/post/requestBody/content/application~1json/encoding/f"),
		"the field has no effect whatever its keys name")
}
