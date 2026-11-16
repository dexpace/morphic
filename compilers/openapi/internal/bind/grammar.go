// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// state is where the OpenAPI grammar stands after reading a pointer's tokens.
type state uint8

const (
	sRaw state = iota
	sValue
	sDocument
	sComponents
	sSchemaMap
	sSchema
	sSchemaList
	sPaths
	sPathItemMap
	sPathItem
	sOperationMap
	sOperation
	sParameterList
	sParameterMap
	sParameter
	sHeaderMap
	sHeader
	sRequestBodyMap
	sRequestBody
	sResponses
	sResponseMap
	sResponse
	sMediaTypeMap
	sMediaType
	sEncodingMap
	sEncodingList
	sEncoding
	sCallbackMap
	sCallback
	sExampleMap
	sExample
	sLinkMap
	sLink
	sSecuritySchemeMap
	sSecurityScheme
	// sSecurityList is a `security` list, and sSecurityRequirement one of its
	// Security Requirement Objects, whose keys name schemes (requirementKeys).
	sSecurityList
	sSecurityRequirement
	sObject
	sRawEntries
	// sLoose is a position of a document that is no OpenAPI document, which no
	// grammar places: read as a schema for what it declares (docs.identityState).
	sLoose
)

// fields maps the fields each object state defines to the state each leads
// to: every field the rule table gives the object, at any minor
// (objectFields), and the Schema Object's keywords. A field an object leaves
// out is raw YAML, as is every x- key. A schema's keyword grammar is its
// dialect's, not the object grammar's, so it stays hand-written
// (schemaFields, dialectStep).
var fields = withSchema(objectFields(oas.Table()))

// withSchema returns objects with the Schema Object's keywords beside them.
func withSchema(objects map[state]map[string]state) map[state]map[string]state {
	objects[sSchema] = schemaFields()
	return objects
}

// unheld holds the fields the grammar places whose content no model holds,
// the parser's or the load phase's: the Encoding Objects OpenAPI 3.2 nests
// under another's encoding, prefixEncoding and itemEncoding. The load phase
// models none, as it models components/mediaTypes, and the IR has no home for
// one, so the lowering keeps each verbatim. place.below is its one reader.
var unheld = map[state]map[string]bool{sEncoding: {"encoding": true, "prefixEncoding": true, "itemEncoding": true}}

// place is where a read stands in the grammar: the state it reads a position
// at, and whether a token it read since it began, or since the last reference
// it passed, named a field whose content no model holds (unheld). Beneath one a
// schema is raw YAML to the model, as one under an extension is
// (docs/compilers/openapi.md#references). A read past a reference starts
// afresh, as under an extension: the object its chain ends on is built where
// no model holds one.
type place struct {
	state   state
	beneath bool
}

// below is the place one token below p. Every read asking what the model holds
// steps by it. A walk placing what the grammar reads, as the finder's for
// references, the identifiers' and Kinds' do, steps by step instead, since the
// grammar places each such field
// (docs/compilers/openapi.md#unmodeled-dialect-fields).
func (p place) below(token string) place {
	return place{state: step(p.state, token), beneath: p.beneath || unheld[p.state][token]}
}

// along is the place tokens reach from p.
func (p place) along(tokens []string) place {
	for _, t := range tokens {
		p = p.below(t)
	}
	return p
}

// role is what a reference landing at p reads there (Role): a schema beneath a
// field no model holds is raw YAML, and any other position is what its state
// reads.
func (p place) role() Role {
	if r := roleOf(p.state); r != Schema || !p.beneath {
		return r
	}
	return Raw
}

// held reports whether the model holds p as an object or a list of its own:
// one the grammar models (modelled), beneath no field no model holds.
func (p place) held() bool { return modelled(p.state) && !p.beneath }

// schemaFields is what a Schema Object holds under each keyword the model
// reads: a schema, a map or list of them, a literal kept raw, a list of
// literals, or a value.
func schemaFields() map[string]state {
	m := map[string]state{}
	for _, k := range []string{"not", "if", "then", "else", "additionalProperties", "items", "contains",
		"propertyNames", "unevaluatedItems", "unevaluatedProperties", "contentSchema"} {
		m[k] = sSchema
	}
	for _, k := range []string{"properties", "patternProperties", "dependentSchemas", "$defs"} {
		m[k] = sSchemaMap
	}
	for _, k := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		m[k] = sSchemaList
	}
	for _, k := range []string{"example", "default", "const"} {
		m[k] = sRaw
	}
	for _, k := range []string{"enum", "examples"} {
		m[k] = sRawEntries
	}
	for _, k := range []string{"$ref", "$id", "$schema", "$anchor", "type", "title", "description", "format",
		"multipleOf", "maximum", "minimum", "exclusiveMaximum", "exclusiveMinimum", "maxLength", "minLength",
		"pattern", "maxItems", "minItems", "uniqueItems", "maxProperties", "minProperties", "required",
		"minContains", "maxContains", "contentEncoding", "contentMediaType", "nullable", "readOnly", "writeOnly",
		"deprecated", "discriminator", "xml", "externalDocs"} {
		m[k] = sValue
	}
	return m
}

// entries maps a container state to the state of each of its entries.
var entries = map[state]state{
	sSchemaMap:         sSchema,
	sSchemaList:        sSchema,
	sPaths:             sPathItem,
	sPathItemMap:       sPathItem,
	sOperationMap:      sOperation,
	sParameterList:     sParameter,
	sParameterMap:      sParameter,
	sHeaderMap:         sHeader,
	sRequestBodyMap:    sRequestBody,
	sResponses:         sResponse,
	sResponseMap:       sResponse,
	sMediaTypeMap:      sMediaType,
	sEncodingMap:       sEncoding,
	sEncodingList:      sEncoding,
	sCallbackMap:       sCallback,
	sCallback:          sPathItem,
	sExampleMap:        sExample,
	sLinkMap:           sLink,
	sSecuritySchemeMap: sSecurityScheme,
	sSecurityList:      sSecurityRequirement,
	sRawEntries:        sRaw,
}

// extensible holds the containers whose x- keys are extensions rather than
// entries: those the specification gives patterned fields of their own.
var extensible = map[state]bool{sPaths: true, sResponses: true, sCallback: true}

// refKinds holds the states of the objects modelled as a Reference Object when
// they write a $ref, each with the kind of object the reference stands for: the
// parser's own kinds, and a Media Type Object, which the load phase models
// (GitHub #856). A pointer read through one reads on in the object its chain
// ends on (binder.through).
var refKinds = map[state]RefKind{
	sPathItem: RefPathItem, sParameter: RefParameter, sHeader: RefHeader, sRequestBody: RefRequestBody,
	sResponse: RefResponse, sCallback: RefCallback, sExample: RefExample, sLink: RefLink,
	sSecurityScheme: RefSecurityScheme, sMediaType: RefMediaType,
}

// refStates is the state each RefKind reads its object at, refKinds turned
// round, by the kind less one.
var refStates = []state{sPathItem, sParameter, sHeader, sRequestBody, sResponse, sCallback, sExample, sLink,
	sSecurityScheme, sMediaType}

// state is the state an object of kind k is read at.
func (k RefKind) state() state { return refStates[k-1] }

// step is the state one token below s.
func step(s state, token string) state {
	switch s {
	case sRaw:
		return sRaw
	case sValue, sObject:
		return sValue
	case sLoose:
		return looseStep(token)
	}
	if e, ok := entries[s]; ok {
		if strings.HasPrefix(token, "x-") && extensible[s] {
			return sRaw
		}
		return e
	}
	if next, ok := fields[s][token]; ok {
		return next
	}
	return sRaw
}

// looseStep is the state one token below a position of a document no grammar
// places: a keyword of a schema reads as it does in one, so a literal or a value
// holds nothing and a schema map holds schemas; an extension is raw, as a
// schema's is; and any other key stays loose, since a document of schemas keeps
// them under keys of its own.
func looseStep(token string) state {
	if strings.HasPrefix(token, "x-") {
		return sRaw
	}
	if next, ok := fields[sSchema][token]; ok {
		return next
	}
	return sLoose
}

// stateOf is the state tokens reach from s.
func stateOf(s state, tokens []string) state {
	for _, t := range tokens {
		s = step(s, t)
	}
	return s
}

// roleOf is the role a position read at state s has.
func roleOf(s state) Role {
	switch s {
	case sSchema:
		return Schema
	case sRaw:
		return Raw
	default:
		return Object
	}
}

// modelled reports whether the model holds a position read at s as an object
// or a list of its own, rather than as raw YAML or a scalar value.
func modelled(s state) bool {
	return s != sRaw && s != sValue
}
