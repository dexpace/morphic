package resolve

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// ComponentRef resolves an internal pointer addressing a top-level component
// schema to its stable named ID, but only when that component is declared. It
// returns handled=true once the pointer is classified as a component pointer,
// declared or not, so callers can stop: a declared component yields ok=true, an
// undeclared one ok=false (a dangling reference to drop). The ID is rebuilt
// from the component's canonical name, unescaped then re-escaped by ids.Ptr,
// not from the incoming pointer text, so a non-canonically escaped reference
// (`A~B` for a component named "A~B", interned under `A~0B`) still resolves to
// the interned node.
func (s Scope) ComponentRef(pointer jsontext.Pointer) (id ir.TypeID, ok, handled bool) {
	name, isComponent := ids.ComponentSchemaName(pointer)
	if !isComponent {
		return "", false, false
	}
	if s.Declares(name) {
		return ids.Document{}.NamedType(ids.Ptr("components", "schemas", name)), true, true
	}
	return "", false, true
}

// NamesReferent reports whether the $ref at js names a schema this
// compilation can point a TypeRef at, answering what resolveSchemaRef answers
// without promising anything on the way, since deciding how to lower a schema
// must not queue nodes as a side effect of asking. resolveSchemaRef reads the
// registry no more than it does, so the two answer alike in any order.
func (s Scope) NamesReferent(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	at, ok := s.Target(js)
	if !ok {
		return false
	}
	if at.Doc == bind.Source {
		if _, resolved, handled := s.ComponentRef(at.Pointer); handled {
			return resolved
		}
	}
	return Hoistable(s.Bindings.Declared(js))
}

// Hoistable reports whether decl, the schema a $ref lands on, can be hoisted
// where it is written: one with a body, or a boolean one (GitHub #861).
// The schema lowering's resolvePointer and NamesReferent both ask it, so they
// cannot part.
func Hoistable(decl *oas3.JSONSchema[oas3.Referenceable]) bool {
	return decl != nil && (decl.IsBool() || annotation.At(decl).Node != nil)
}

// IsRefSite reports whether a position is $ref-shaped: the resolver's own
// IsReference (a non-empty $ref), or a schema body that carries a Ref field of
// its own even when empty. That is deliberately broader than annotation.At's
// classification: schemaRefHomed, TargetSchema, and bodySchemaPointer
// (operation/multipart.go) all need "there is a $ref-carrying body here," not
// "there is a genuine, followable reference," so the degenerate {$ref: ""}
// shape counts for them even though it does not count as an
// annotation.Reference. s is the schema body the caller already holds; a nil s
// (a boolean schema carries none) is never $ref-shaped.
func IsRefSite(js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema) bool {
	return js.IsReference() || (s != nil && s.Ref != nil)
}

// TargetSchema returns the schema the chain of $refs from js ends on when js
// is a $ref, so use-site annotations can fall back to the referent, and nil
// otherwise or where the chain ends on none.
func (s Scope) TargetSchema(js *oas3.JSONSchema[oas3.Referenceable], ref *oas3.Schema) *oas3.Schema {
	if !IsRefSite(js, ref) {
		return nil
	}
	return s.Bindings.End(js).GetSchema()
}
