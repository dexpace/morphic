// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/convention"
	"github.com/dexpace/morphic/ir"
)

// sensitiveKey is the default mapping's redaction key on a node, holding value.
func sensitiveKey(value string) ir.Unmodeled {
	return ir.Unmodeled{"openapi:x-sensitive": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(value)}}
}

// memberOf is a document holding one model, whose one property is property, over the
// types of reg.
func memberOf(property ir.Property, reg ir.TypeRegistry) *ir.Document {
	types := registry(reg)
	types["type/model"] = &ir.Model{ID: "type/model", Properties: []ir.Property{property}}
	return &ir.Document{Types: types}
}

// scalars is a chain of n scalars, the first t/c/0, over the text primitive.
func scalars(n int) ir.TypeRegistry {
	reg := ir.TypeRegistry{}
	for i := range n {
		base := ir.TypeID(fmt.Sprintf("type/c/%d", i+1))
		if i == n-1 {
			base = ir.PrimitiveTypeID(ir.PrimitiveString)
		}
		id := ir.TypeID(fmt.Sprintf("type/c/%d", i))
		reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: base}}
	}
	return reg
}

// propertyOf returns the one property memberOf built doc around.
func propertyOf(t *testing.T, doc *ir.Document) ir.Property {
	t.Helper()
	model, ok := doc.Types["type/model"].(*ir.Model)
	require.True(t, ok)
	require.Len(t, model.Properties, 1)
	return model.Properties[0]
}

// TestRedact_ReadsTheNamesTail pins which names say a value is a credential:
// those whose canonical words end in a credential's, whatever their spelling
// and numbers after it, and not one ending in another word, a credential's
// identifier or a page's token among them, nor `token` alone, nor an
// authorization that is not the whole name.
func TestRedact_ReadsTheNamesTail(t *testing.T) {
	t.Parallel()
	for name, secret := range map[string]bool{
		"password": true, "Password": true, "user_password": true, "passwd": true, "passphrase": true,
		"secret": true, "client_secret": true, "signingSecret": true, "apikey": true, "APIKEY": true,
		"api_key": true, "X-Api-Key": true, "apiKey": true, "access_key": true, "R2-Secret-Access-Key": true,
		"secret_key": true, "privateKey": true, "access_token": true, "refreshToken": true, "X-Auth-Token": true,
		"bearer_token": true, "session_token": true, "api_token": true, "password2": true, "apiKey2": true,
		"Password_1_2": true, "passPhrase": true, "pass_phrase": true, "authorization": true, "Authorization": true,
		"Proxy-Authorization": true, "2": false, "secret2FA": false, "api_key_v2": false, "bypass_phrase": false,
		"phrase": false, "payment_authorization": false, "X-Authorization": false, "secret_id": false,
		"secret_name": false, "R2-Access-Key-Id": false, "api_key_id": false, "key": false,
		"token": false, "page_token": false, "next_token": false, "token_type": false, "password_policy": false,
		"client_secret_version": false, "keys": false, "passwords": false, "": false,
		"_": false, "public_key": false, "id_token_hint": false,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := memberOf(ir.Property{ID: "property/a", Name: ir.NamingFor(name), Type: prim(ir.PrimitiveString)}, nil)
			assert.Empty(t, convention.Apply(convention.Policy{}, defaults(), compileddoc.New(doc)))
			property := propertyOf(t, doc)
			assert.Equal(t, secret, property.Secret)
			assert.Equal(t, secret, property.Provenance.Inferred == convention.SecretInferenceHeuristic, "marked where set")
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
		{"text", ir.PrimitiveTypeID(ir.PrimitiveString), nil, true},
		{"bytes", ir.PrimitiveTypeID(ir.PrimitiveBytes), nil, true},
		{"a scalar over text", "type/s", ir.TypeRegistry{"type/s": over("type/s", ir.PrimitiveTypeID(ir.PrimitiveString))}, true},
		{"an alias over a scalar over bytes", "type/a", ir.TypeRegistry{
			"type/a": over("type/a", "type/s"), "type/s": over("type/s", ir.PrimitiveTypeID(ir.PrimitiveBytes)),
		}, true},
		{"a boolean", ir.PrimitiveTypeID(ir.PrimitiveBool), nil, false},
		{"a date", ir.PrimitiveTypeID(ir.PrimitiveDate), nil, false},
		{"a model", "type/m", ir.TypeRegistry{"type/m": &ir.Model{ID: "type/m"}}, false},
		{"an alias over a model", "type/a", ir.TypeRegistry{"type/a": over("type/a", "type/m"), "type/m": &ir.Model{ID: "type/m"}}, false},
		{"an opaque scalar", "type/o", ir.TypeRegistry{"type/o": &ir.Scalar{ID: "type/o"}}, false},
		{"a type the registry lacks", "type/missing", nil, false},
		{"a cycle of scalars", "type/x", ir.TypeRegistry{"type/x": over("type/x", "type/y"), "type/y": over("type/y", "type/x")}, false},
		{"a chain of scalars the bound reads to its primitive", "type/c/0", scalars(63), true},
		{"a chain past the bound", "type/c/0", scalars(64), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := memberOf(ir.Property{ID: "property/a", Name: ir.NamingFor("password"), Type: ir.TypeRef{Target: tc.typ}}, tc.reg)
			assert.Empty(t, convention.Apply(convention.Policy{}, defaults(), compileddoc.New(doc)))
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
		common.ID = "type/s"
		return ir.TypeRegistry{"type/s": &ir.Scalar{TypeCommon: common, Base: &ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveString)}}}
	}
	for _, tc := range []struct {
		name   string
		p      ir.Property
		reg    ir.TypeRegistry
		secret bool
		marker ir.Inferred
	}{
		{"already redacted", ir.Property{Secret: true, Type: prim(ir.PrimitiveString),
			Provenance: ir.Provenance{Inferred: "extension-promotion"}}, nil, true, "extension-promotion"},
		{"a sensitive type", ir.Property{Type: ir.TypeRef{Target: "type/s"}}, scalar(ir.TypeCommon{Sensitive: true}), false, ""},
		{"a key saying false at the use", ir.Property{Type: prim(ir.PrimitiveString), Unmodeled: sensitiveKey(`false`)}, nil,
			false, ""},
		{"a key saying false along the type", ir.Property{Type: ir.TypeRef{Target: "type/s"}},
			scalar(ir.TypeCommon{Unmodeled: sensitiveKey(`false`)}), false, ""},
		{"a key under another target", ir.Property{Type: prim(ir.PrimitiveString), Unmodeled: ir.Unmodeled{
			"openapi:x-stability": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`"beta"`)},
		}}, nil, true, convention.SecretInferenceHeuristic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.p.ID, tc.p.Name = "property/a", ir.NamingFor("password")
			doc := memberOf(tc.p, tc.reg)
			assert.Empty(t, convention.Apply(convention.Policy{}, defaults(), compileddoc.New(doc)))
			property := propertyOf(t, doc)
			assert.Equal(t, tc.secret, property.Secret)
			assert.Equal(t, tc.marker, property.Provenance.Inferred)
		})
	}
}

// TestRedact_ReachesEveryUse pins where a credential's name is read: an
// operation's parameters, the headers of its responses, error cases and
// default, those its bodies' encodings give a part or a stream's items, and
// each model's members, and that with the policy off none is.
func TestRedact_ReachesEveryUse(t *testing.T) {
	t.Parallel()
	pw := func(id ir.PropertyID) ir.Header {
		return ir.Header{ID: id, Name: ir.NamingFor("X-Api-Key"), WireName: "X-Api-Key", Type: prim(ir.PrimitiveString)}
	}
	parts := func(at ir.PropertyID) *ir.Payload {
		return &ir.Payload{Contents: []ir.Content{{
			ItemEncoding: &ir.PartEncoding{Headers: []ir.Header{pw(at + "/item")}},
			Encoding:     map[ir.PropertyID]ir.PartEncoding{"property/part": {Headers: []ir.Header{pw(at + "/part")}}},
		}}}
	}
	build := func() *ir.Document {
		op := ir.Operation{
			ID:        "operation/a",
			Params:    []ir.Parameter{{ID: "parameter/a", Name: ir.NamingFor("password"), Type: prim(ir.PrimitiveString)}},
			Request:   parts("property/request"),
			Responses: []ir.Response{{Headers: []ir.Header{pw("property/response")}, Payload: parts("property/response-body")}},
			Errors:    []ir.ErrorCase{{Headers: []ir.Header{pw("property/error")}, Payload: parts("property/error-body")}},
			Default:   &ir.Response{Headers: []ir.Header{pw("property/default")}, Payload: parts("property/default-body")},
		}
		doc := oneOperation(op, registry(nil))
		doc.Types["type/model"] = &ir.Model{ID: "type/model", Properties: []ir.Property{
			{ID: "property/member", Name: ir.NamingFor("client_secret"), Type: prim(ir.PrimitiveString)},
		}}
		return doc
	}

	on := build()
	assert.Empty(t, convention.Apply(convention.Policy{}, defaults(), compileddoc.New(on)))
	got := redacted(on)
	assert.ElementsMatch(t, []string{"parameter/a", "property/request/item", "property/request/part", "property/response",
		"property/response-body/item", "property/response-body/part", "property/error", "property/error-body/item", "property/error-body/part",
		"property/default", "property/default-body/item", "property/default-body/part", "property/member"}, got)

	off := build()
	assert.Empty(t, convention.Apply(convention.Policy{Secret: convention.SecretInference{Disabled: true}}, defaults(),
		compileddoc.New(off)))
	assert.Empty(t, redacted(off))
}

// redacted returns the ID of every parameter, header and member of doc that is
// Secret and marked by the secret policy.
func redacted(doc *ir.Document) []string {
	var ids []string
	mark := func(id string, secret bool, prov ir.Provenance) {
		if secret && prov.Inferred == convention.SecretInferenceHeuristic {
			ids = append(ids, id)
		}
	}
	headers := func(hs []ir.Header) {
		for _, header := range hs {
			mark(string(header.ID), header.Secret, header.Provenance)
		}
	}
	payload := func(payload *ir.Payload) {
		for _, content := range payload.Contents {
			headers(content.ItemEncoding.Headers)
			for _, partEncoding := range content.Encoding {
				headers(partEncoding.Headers)
			}
		}
	}
	op := operationOf(doc)
	for _, parameter := range op.Params {
		mark(string(parameter.ID), parameter.Secret, parameter.Provenance)
	}
	payload(op.Request)
	headers(op.Responses[0].Headers)
	payload(op.Responses[0].Payload)
	headers(op.Errors[0].Headers)
	payload(op.Errors[0].Payload)
	headers(op.Default.Headers)
	payload(op.Default.Payload)
	if model, ok := doc.Types["type/model"].(*ir.Model); ok {
		for _, property := range model.Properties {
			mark(string(property.ID), property.Secret, property.Provenance)
		}
	}
	return ids
}
