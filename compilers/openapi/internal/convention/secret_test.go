// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/convention"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// sensitiveKey is the default mapping's redaction key on a node, holding value.
func sensitiveKey(value string) ir.Unmodeled {
	return ir.Unmodeled{"openapi:x-sensitive": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(value)}}
}

// memberOf is a document holding one model, whose one property is p, over the
// types of reg.
func memberOf(p ir.Property, reg ir.TypeRegistry) *ir.Document {
	types := registry(reg)
	types["t/model"] = &ir.Model{ID: "t/model", Properties: []ir.Property{p}}
	return &ir.Document{Types: types}
}

// scalars is a chain of n scalars, the first t/c/0, over the text primitive.
func scalars(n int) ir.TypeRegistry {
	reg := ir.TypeRegistry{}
	for i := range n {
		base := ir.TypeID(fmt.Sprintf("t/c/%d", i+1))
		if i == n-1 {
			base = ir.PrimTypeID(ir.PrimString)
		}
		id := ir.TypeID(fmt.Sprintf("t/c/%d", i))
		reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: base}}
	}
	return reg
}

// propertyOf returns the one property memberOf built doc around.
func propertyOf(t *testing.T, doc *ir.Document) ir.Property {
	t.Helper()
	m, ok := doc.Types["t/model"].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	return m.Properties[0]
}

// TestRedact_ReadsTheNamesTail pins which names say a value is a credential:
// those whose canonical words end in a credential's, whatever their spelling,
// and not one ending in another word, a credential's identifier or a page's
// token among them, nor `token` alone.
func TestRedact_ReadsTheNamesTail(t *testing.T) {
	t.Parallel()
	for name, secret := range map[string]bool{
		"password": true, "Password": true, "user_password": true, "passwd": true, "passphrase": true,
		"secret": true, "client_secret": true, "signingSecret": true, "apikey": true, "APIKEY": true,
		"api_key": true, "X-Api-Key": true, "apiKey": true, "access_key": true, "R2-Secret-Access-Key": true,
		"secret_key": true, "privateKey": true, "access_token": true, "refreshToken": true, "X-Auth-Token": true,
		"bearer_token": true, "session_token": true, "api_token": true,
		"secret_id": false, "secret_name": false, "R2-Access-Key-Id": false, "api_key_id": false, "key": false,
		"token": false, "page_token": false, "next_token": false, "token_type": false, "password_policy": false,
		"client_secret_version": false, "keys": false, "passwords": false, "": false,
		"_": false, "public_key": false, "id_token_hint": false,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := memberOf(ir.Property{ID: "p/a", Name: ir.NamingFor(name), Type: prim(ir.PrimString)}, nil)
			assert.Empty(t, convention.Apply(under(lowering.Policy{}), doc))
			p := propertyOf(t, doc)
			assert.Equal(t, secret, p.Secret)
			assert.Equal(t, secret, p.Provenance.Inferred == lowering.SecretInferenceHeuristic, "marked where set")
		})
	}
}

// TestRedact_ReadsTheValuesType pins which values a credential's name redacts:
// text and bytes, read through the scalars a schema or a $ref hoists, and no
// other value, nor one whose chain ends at no primitive, loops, or reaches its
// primitive past the bound: 63 scalars and the primitive are 64 types read.
func TestRedact_ReadsTheValuesType(t *testing.T) {
	t.Parallel()
	over := func(id ir.TypeID, base ir.TypeID) *ir.Scalar {
		return &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: base}}
	}
	for _, tc := range []struct {
		name   string
		typ    ir.TypeID
		reg    ir.TypeRegistry
		secret bool
	}{
		{"text", ir.PrimTypeID(ir.PrimString), nil, true},
		{"bytes", ir.PrimTypeID(ir.PrimBytes), nil, true},
		{"a scalar over text", "t/s", ir.TypeRegistry{"t/s": over("t/s", ir.PrimTypeID(ir.PrimString))}, true},
		{"an alias over a scalar over bytes", "t/a", ir.TypeRegistry{
			"t/a": over("t/a", "t/s"), "t/s": over("t/s", ir.PrimTypeID(ir.PrimBytes)),
		}, true},
		{"a boolean", ir.PrimTypeID(ir.PrimBool), nil, false},
		{"a date", ir.PrimTypeID(ir.PrimDate), nil, false},
		{"a model", "t/m", ir.TypeRegistry{"t/m": &ir.Model{ID: "t/m"}}, false},
		{"an alias over a model", "t/a", ir.TypeRegistry{"t/a": over("t/a", "t/m"), "t/m": &ir.Model{ID: "t/m"}}, false},
		{"an opaque scalar", "t/o", ir.TypeRegistry{"t/o": &ir.Scalar{ID: "t/o"}}, false},
		{"a type the registry lacks", "t/missing", nil, false},
		{"a cycle of scalars", "t/x", ir.TypeRegistry{"t/x": over("t/x", "t/y"), "t/y": over("t/y", "t/x")}, false},
		{"a chain of scalars the bound reads to its primitive", "t/c/0", scalars(63), true},
		{"a chain past the bound", "t/c/0", scalars(64), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := memberOf(ir.Property{ID: "p/a", Name: ir.NamingFor("password"), Type: ir.TypeRef{Target: tc.typ}}, tc.reg)
			assert.Empty(t, convention.Apply(under(lowering.Policy{}), doc))
			assert.Equal(t, tc.secret, propertyOf(t, doc).Secret)
		})
	}
}

// TestRedact_LeavesWhatTheDocumentSays pins the precedence: a use the document
// already redacts keeps its own mark and takes none, as does one whose type is
// sensitive, and a redaction key the document writes, at the use or along its
// type, decides whatever it says.
func TestRedact_LeavesWhatTheDocumentSays(t *testing.T) {
	t.Parallel()
	scalar := func(common ir.TypeCommon) ir.TypeRegistry {
		common.ID = "t/s"
		return ir.TypeRegistry{"t/s": &ir.Scalar{TypeCommon: common, Base: &ir.TypeRef{Target: ir.PrimTypeID(ir.PrimString)}}}
	}
	for _, tc := range []struct {
		name   string
		p      ir.Property
		reg    ir.TypeRegistry
		secret bool
		marker ir.Inferred
	}{
		{"already redacted", ir.Property{Secret: true, Type: prim(ir.PrimString),
			Provenance: ir.Provenance{Inferred: "extension-promotion"}}, nil, true, "extension-promotion"},
		{"a sensitive type", ir.Property{Type: ir.TypeRef{Target: "t/s"}}, scalar(ir.TypeCommon{Sensitive: true}), false, ""},
		{"a key saying false at the use", ir.Property{Type: prim(ir.PrimString), Unmodeled: sensitiveKey(`false`)}, nil,
			false, ""},
		{"a key saying false along the type", ir.Property{Type: ir.TypeRef{Target: "t/s"}},
			scalar(ir.TypeCommon{Unmodeled: sensitiveKey(`false`)}), false, ""},
		{"a key under another target", ir.Property{Type: prim(ir.PrimString), Unmodeled: ir.Unmodeled{
			"openapi:x-stability": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`"beta"`)},
		}}, nil, true, lowering.SecretInferenceHeuristic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.p.ID, tc.p.Name = "p/a", ir.NamingFor("password")
			doc := memberOf(tc.p, tc.reg)
			assert.Empty(t, convention.Apply(under(lowering.Policy{}), doc))
			p := propertyOf(t, doc)
			assert.Equal(t, tc.secret, p.Secret)
			assert.Equal(t, tc.marker, p.Provenance.Inferred)
		})
	}
}

// TestRedact_ReachesEveryUse pins where a credential's name is read: an
// operation's parameters, the headers of its responses, error cases and
// default, those its bodies' encodings give a part or a stream's items, and
// each model's members, and that with the policy off none is.
func TestRedact_ReachesEveryUse(t *testing.T) {
	t.Parallel()
	pw := func(id ir.PropID) ir.Header {
		return ir.Header{ID: id, Name: ir.NamingFor("X-Api-Key"), WireName: "X-Api-Key", Type: prim(ir.PrimString)}
	}
	parts := func(at ir.PropID) *ir.Payload {
		return &ir.Payload{Contents: []ir.Content{{
			ItemEncoding: &ir.PartEncoding{Headers: []ir.Header{pw(at + "/item")}},
			Encoding:     map[ir.PropID]ir.PartEncoding{"p/part": {Headers: []ir.Header{pw(at + "/part")}}},
		}}}
	}
	build := func() *ir.Document {
		op := ir.Operation{
			ID:        "op/a",
			Params:    []ir.Parameter{{ID: "param/a", Name: ir.NamingFor("password"), Type: prim(ir.PrimString)}},
			Request:   parts("p/request"),
			Responses: []ir.Response{{Headers: []ir.Header{pw("p/response")}, Payload: parts("p/response-body")}},
			Errors:    []ir.ErrorCase{{Headers: []ir.Header{pw("p/error")}, Payload: parts("p/error-body")}},
			Default:   &ir.Response{Headers: []ir.Header{pw("p/default")}, Payload: parts("p/default-body")},
		}
		doc := oneOperation(op, registry(nil))
		doc.Types["t/model"] = &ir.Model{ID: "t/model", Properties: []ir.Property{
			{ID: "p/member", Name: ir.NamingFor("client_secret"), Type: prim(ir.PrimString)},
		}}
		return doc
	}

	on := build()
	assert.Empty(t, convention.Apply(under(lowering.Policy{}), on))
	got := redacted(on)
	assert.ElementsMatch(t, []string{"param/a", "p/request/item", "p/request/part", "p/response",
		"p/response-body/item", "p/response-body/part", "p/error", "p/error-body/item", "p/error-body/part",
		"p/default", "p/default-body/item", "p/default-body/part", "p/member"}, got)

	off := build()
	assert.Empty(t, convention.Apply(under(lowering.Policy{SecretInference: lowering.SecretInference{Disabled: true}}), off))
	assert.Empty(t, redacted(off))
}

// redacted returns the ID of every parameter, header and member of doc that is
// Secret and marked by the secret policy.
func redacted(doc *ir.Document) []string {
	var ids []string
	mark := func(id string, secret bool, prov ir.Provenance) {
		if secret && prov.Inferred == lowering.SecretInferenceHeuristic {
			ids = append(ids, id)
		}
	}
	headers := func(hs []ir.Header) {
		for _, h := range hs {
			mark(string(h.ID), h.Secret, h.Provenance)
		}
	}
	payload := func(p *ir.Payload) {
		for _, c := range p.Contents {
			headers(c.ItemEncoding.Headers)
			for _, e := range c.Encoding {
				headers(e.Headers)
			}
		}
	}
	op := operationOf(doc)
	for _, p := range op.Params {
		mark(string(p.ID), p.Secret, p.Provenance)
	}
	payload(op.Request)
	headers(op.Responses[0].Headers)
	payload(op.Responses[0].Payload)
	headers(op.Errors[0].Headers)
	payload(op.Errors[0].Payload)
	headers(op.Default.Headers)
	payload(op.Default.Payload)
	if m, ok := doc.Types["t/model"].(*ir.Model); ok {
		for _, p := range m.Properties {
			mark(string(p.ID), p.Secret, p.Provenance)
		}
	}
	return ids
}
