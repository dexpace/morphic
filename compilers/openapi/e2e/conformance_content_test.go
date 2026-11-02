// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func assertMultiContent(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getData")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 2, "both media types kept, no primary selection")
	assert.Equal(t, "application/json", op.Responses[0].Payload.Contents[0].MediaType)
	assert.Equal(t, "application/xml", op.Responses[0].Payload.Contents[1].MediaType)
}

func assertMultipartEncoding(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "upload")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	enc := op.Request.Contents[0].Encoding
	require.NotEmpty(t, enc, "multipart parts carry PartEncoding keyed by PropID")
	var sawFile, sawMulti bool
	var fileHeaders []ir.Header
	for _, pe := range enc {
		if pe.Filename {
			sawFile = true
			fileHeaders = pe.Headers
		}
		if pe.Multi {
			sawMulti = true
		}
	}
	assert.True(t, sawFile, "binary file part carries Filename")
	assert.True(t, sawMulti, "array part carries Multi")
	assertPartHeaders(t, doc, fileHeaders)
	assertFormPartStyle(t, doc)
	assertComposedPartEncoding(t, doc)
}

// assertComposedPartEncoding pins the body that composes its parts rather than
// declaring them. Reading only the schema node's own properties found none on an
// allOf wrapper, so the whole encoding block was discarded with no diagnostic and
// a Content that still looked well-formed, just smaller (GitHub #140).
//
// The keys are asserted to be the properties the IR actually holds:
// docs/ir/models.md#composition keeps a composed model's inherited parts on its
// Base, so a key derived from the composed node's own pointer would name a
// property that exists nowhere.
func assertComposedPartEncoding(t *testing.T, doc *ir.Document) {
	t.Helper()
	op, ok := opByName(doc, "uploadComposed")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)

	enc := op.Request.Contents[0].Encoding
	require.NotEmpty(t, enc, "a composed body keeps the encoding a declared one does")

	inherited := ir.PropID("p/openapi/components/schemas/UploadForm/properties/file")
	file, ok := enc[inherited]
	require.True(t, ok, "the inherited part is keyed on the Base that holds it; got keys %v", enc)
	assert.Equal(t, []string{"image/png"}, file.ContentTypes)
	assert.True(t, file.Filename, "the structural flag is read off the merged type of the inherited part")

	// Every key must name a property some type in the document declares. A part
	// keyed by a PropID nothing holds is the failure this lowering can produce
	// while still looking well-formed.
	for id := range enc {
		assert.True(t, propIDExists(doc, id), "encoding key %s names no property in the document", id)
	}
}

// propIDExists reports whether any type in doc declares a property with the
// given ID.
func propIDExists(doc *ir.Document, id ir.PropID) bool {
	for _, td := range doc.Types {
		m, ok := td.(*ir.Model)
		if !ok {
			continue
		}
		if slices.ContainsFunc(m.Properties, func(p ir.Property) bool { return p.ID == id }) {
			return true
		}
	}
	return false
}

// assertFormPartStyle pins the form-serialization half of a part's encoding: a
// urlencoded body's parts carry the same style/explode pair a query parameter
// does, on the part rather than on any binding.
func assertFormPartStyle(t *testing.T, doc *ir.Document) {
	t.Helper()
	op, ok := opByName(doc, "submitForm")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	assert.Equal(t, "application/x-www-form-urlencoded", op.Request.Contents[0].MediaType)
	enc := op.Request.Contents[0].Encoding
	require.Len(t, enc, 1)
	for _, pe := range enc {
		assert.Equal(t, "form", pe.Style)
		require.NotNil(t, pe.Explode)
		assert.True(t, *pe.Explode)
		assert.True(t, pe.Multi, "the structural flag is read off the part")
		// Two documents differing only in allowReserved used to compile to one IR
		// (GitHub #291); it lowers onto the part, as a parameter's does.
		assert.True(t, pe.AllowReserved)
	}
	assert.NotContains(t, op.Request.Contents[0].Unmodeled, "openapi:encoding/ids/allowReserved",
		"and is kept verbatim nowhere")
}

// assertFileBody pins a binary body: the payload's type degrades to bytes and the
// media types it is carried as move onto Content.File, which is what tells an
// emitter to generate a stream rather than a model. Both routes to that lowering
// are covered — a media type with no schema at all, and a string+binary schema.
func assertFileBody(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "putBlob")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	req := op.Request.Contents[0]
	require.NotNil(t, req.File, "application/octet-stream with no schema is a file body")
	assert.Equal(t, []string{"application/octet-stream"}, req.File.ContentTypes)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), req.Type.Target)

	resp := firstContent(t, op)
	require.NotNil(t, resp.File, "a string+binary schema is a file body whatever the media type")
	assert.Equal(t, []string{"image/png"}, resp.File.ContentTypes)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), resp.Type.Target)

	documented, ok := opByName(doc, "putDocumented")
	require.True(t, ok)
	require.NotNil(t, documented.Request)
	require.Len(t, documented.Request.Contents, 1)
	blob := documented.Request.Contents[0]
	require.NotNil(t, blob.File, "a binary $ref is a file body")
	assert.Equal(t, componentID("Blob"), blob.Type.Target, "and keeps the schema it names")

	stored := firstContent(t, documented)
	require.NotNil(t, stored.File, "an inline binary schema is a file body")
	node, ok := doc.Types[stored.Type.Target]
	require.True(t, ok)
	assert.Equal(t, "The stored object.", node.Common().Docs.Description, "and keeps its docs")
}

// assertMediaTypeSpelling pins that a media type is classified by its type alone
// while the IR keeps the key as written. One content per classification: a
// multipart body, a form-urlencoded body and a file body with no schema, the
// last on both sides of an operation.
func assertMediaTypeSpelling(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	upload, ok := opByName(doc, "upload")
	require.True(t, ok)
	multipart := requestContent(t, upload)
	assert.Equal(t, "Multipart/Form-Data", multipart.MediaType)
	require.Len(t, multipart.Encoding, 1, "a mixed-case multipart key keeps its part encoding")
	for _, pe := range multipart.Encoding {
		assert.Equal(t, []string{"image/png"}, pe.ContentTypes)
		assert.True(t, pe.Filename)
	}

	submit, ok := opByName(doc, "submit")
	require.True(t, ok)
	form := requestContent(t, submit)
	assert.Equal(t, "Application/X-WWW-Form-Urlencoded; charset=utf-8", form.MediaType)
	require.Len(t, form.Encoding, 1, "a parameterized form-urlencoded key keeps its part encoding")
	for _, pe := range form.Encoding {
		assert.True(t, pe.Multi)
	}

	blob, ok := opByName(doc, "putBlob")
	require.True(t, ok)
	req := requestContent(t, blob)
	require.NotNil(t, req.File, "a mixed-case octet-stream key with no schema is a file body")
	assert.Equal(t, []string{"Application/Octet-Stream"}, req.File.ContentTypes)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), req.Type.Target)

	resp := firstContent(t, blob)
	require.NotNil(t, resp.File, "a parameterized octet-stream key with no schema is a file body")
	assert.Equal(t, []string{"application/octet-stream; type=tar"}, resp.File.ContentTypes)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), resp.Type.Target)
}

// assertPartHeaders pins encoding.<part>.headers, a TypeRef position reachable
// only through a PartEncoding and, before this case, exercised by no corpus
// spec. It covers both hops a header takes — an inline schema and one behind a
// $ref through components/headers — and requires each target to be in the
// registry, which is what gives the referential-closure walker something to
// check here.
func assertPartHeaders(t *testing.T, doc *ir.Document, headers []ir.Header) {
	t.Helper()
	require.Len(t, headers, 2, "the file part declares two per-part headers")
	byWire := make(map[string]ir.Header, len(headers))
	for _, h := range headers {
		byWire[h.WireName] = h
	}

	id, ok := byWire["X-Part-Id"]
	require.True(t, ok, "inline part header lowered; got %v", byWire)
	assert.Equal(t, ir.TypeID("t/prim/uuid"), id.Type.Target)
	assert.True(t, id.Required, "a required part header keeps its presence bit")
	assert.Equal(t, "per-part correlation id", id.Docs.Description)

	sum, ok := byWire["X-Part-Checksum"]
	require.True(t, ok, "$ref'd part header lowered; got %v", byWire)
	assert.Equal(t, componentID("Digest"), sum.Type.Target, "a $ref'd header takes the referent's schema")
	assert.Equal(t, "sha-256 digest of this part", sum.Docs.Description)
	for _, h := range headers {
		assert.Contains(t, doc.Types, h.Type.Target, "part header %q must reference a live node", h.WireName)
	}
}

// assertSequentialMedia pins the 3.2 sequential-media lowering, including the
// two forms per-item encoding takes: one Encoding governing every item lands in
// Content.ItemEncoding, while a positional prefixEncoding — which a single
// every-item encoding has no ordinals for — takes itself and the tail encoding
// beside it into Unmodeled instead.
//
// It also pins what itemSchema says about the operation, which for a long while
// was nothing: the keyword states that the body is a sequence of items, so the
// operation streams, and it says so itself rather than being guessed at from
// the media type — multipart/mixed is in no streaming media-type list.
func assertSequentialMedia(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	events, ok := opByName(doc, "streamEvents")
	require.True(t, ok)
	c := firstContent(t, events)
	require.NotNil(t, c.Item, "itemSchema becomes the element type")
	require.NotNil(t, c.ItemEncoding, "an encoding governing every item lowers structurally")
	assert.Equal(t, []string{"application/json"}, c.ItemEncoding.ContentTypes)
	assert.True(t, c.ItemEncoding.Multi, "the construct describes a repeated tail")
	assert.Empty(t, c.Unmodeled, "nothing is left over once it lowers")

	assert.Equal(t, ir.StreamingServer, events.Streaming)
	require.NotNil(t, events.ResponseStream)
	require.NotNil(t, events.ResponseStream.Events)
	assert.Empty(t, cmp.Diff(*c.Item, *events.ResponseStream.Events),
		"the declared item schema is the stream element type")
	assert.Empty(t, events.Provenance.Inferred, "a declared sequence is not a heuristic")

	parts, ok := opByName(doc, "streamParts")
	require.True(t, ok)
	pc := firstContent(t, parts)
	assert.Nil(t, pc.ItemEncoding, "positional prefixes have no every-item form")
	for _, key := range []string{"openapi:prefixEncoding", "openapi:itemEncoding"} {
		entry, ok := pc.Unmodeled[key]
		require.True(t, ok, "%s kept verbatim; got %v", key, pc.Unmodeled)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	}
}

// assertComponentMediaTypes32 pins a 3.2 document using components/mediaTypes
// (GitHub #856): a content entry written as a $ref, directly or through an
// alias, lowers what the entry it names declares, its $ref'd example and its
// extension included, documented by its description and the summary beside
// the $ref; the entry nothing references is kept on the document; and nothing
// the dialect defines is called undefined.
func assertComponentMediaTypes32(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	want := map[string]ir.Docs{
		"listEvents": {Summary: "Every event.", Description: "One event."},
		"getEvent":   {Description: "One event."},
	}
	for name, docs := range want {
		op, ok := opByName(doc, name)
		require.True(t, ok)
		content := op.Responses[0].Payload.Contents[0]
		assert.Equal(t, "application/json", content.MediaType)
		assert.Equal(t, componentID("Event"), content.Type.Target, name)
		assert.Equal(t, docs, content.Docs, name)
		require.Len(t, content.Examples, 1, name)
		assert.Contains(t, content.Unmodeled, "openapi:x-note", name)
	}
	assert.Contains(t, doc.Unmodeled, "openapi:components/mediaTypes/Unused")
	for _, reached := range []string{"mediaTypes/Event", "mediaTypes/EventAlias", "examples/One"} {
		assert.NotContains(t, doc.Unmodeled, "openapi:components/"+reached)
	}
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
	assert.False(t, openapitest.HasDiag(diags, diag.DegradedConstruct), "got %v", diags)
}

// assertNestedEncoding32 pins GitHub #615's nested Encoding fields: each is kept
// verbatim under the part's own scope with one info, rather than drawing an
// unknown-object-key warning and reaching no entry at all.
func assertNestedEncoding32(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "upload")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	content := op.Request.Contents[0]
	for _, keyword := range []string{"encoding", "prefixEncoding"} {
		entry, ok := content.Unmodeled["openapi:encoding/note/"+keyword]
		require.True(t, ok, "%s is kept verbatim; got %v", keyword, slices.Sorted(maps.Keys(content.Unmodeled)))
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	}
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey),
		"3.2 defines these keys, so the census must leave them alone")
}
