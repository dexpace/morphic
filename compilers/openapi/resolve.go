package openapi

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
)

// siteKind distinguishes a position that declares a type from one that
// references another type and may carry annotations of its own.
type siteKind int

const (
	siteDeclaration siteKind = iota
	siteReference
)

// site is a position that owns an IR node. Node is the schema written at the
// position; Referent is the schema one hop away, set only for a reference site.
//
// The split is what keeps annotations attached where they were written: a
// site-only annotation (examples, constraints) reads Node alone, while a
// site-overrides-referent annotation (docs, deprecation, visibility, default)
// reads Node and falls back to Referent. Resolving this once here is what stops
// each attachment point from re-deciding it.
type site struct {
	Pointer  string
	Kind     siteKind
	Node     *oas3.Schema
	Referent *oas3.Schema
}

// siteAt builds the site for the position at pointer. A position carrying a
// $ref is a reference site and resolves its referent exactly one hop, never to
// the end of the chain: a sub-schema spelled {$ref: Other, minimum: 7} must read
// as this position, not as Other, or the bound written beside the $ref is lost
// before anything can record it.
func (l *lowerer) siteAt(js *oas3.JSONSchema[oas3.Referenceable], pointer string) site {
	st := site{Pointer: pointer, Kind: siteDeclaration, Node: siteSchema(js)}
	if js == nil || js.IsBool() {
		return st
	}
	if !js.IsReference() && (st.Node == nil || st.Node.Ref == nil) {
		return st
	}
	st.Kind = siteReference
	if decl := declaredSchema(js); decl != nil {
		st.Referent = siteSchema(decl)
	}
	return st
}
