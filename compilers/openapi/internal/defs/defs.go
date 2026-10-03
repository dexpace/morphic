package defs

import (
	"encoding/json/jsontext"
	"strings"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"
)

// defsPrefix opens every pointer the resolver reads relative to the schema that
// spells it rather than to the document (oas3 resolveDefsReference, v1.25.2).
const defsPrefix = "/$defs/"

// Navigable is the parsed document a $defs pointer is navigated in: the root
// every ancestor pointer is read from.
type Navigable interface {
	GetRootNode() *yaml.Node
}

// IsPointer reports whether pointer is one the resolver reads relative to
// the schema that spells it (GitHub #557).
func IsPointer(pointer jsontext.Pointer) bool {
	return strings.HasPrefix(string(pointer), defsPrefix)
}

// Target returns the schema a same-document "#/$defs/..." pointer written at
// js lands on, and the pointer of the position it sits at, by the rule the
// resolver applies to a reference it meets on its own walk: the definitions of
// js itself when it is a schema resource of its own ($id), else those of the
// nearest ancestor holding the path (oas3 tryResolveLocalDefs, then
// tryResolveDefsUsingJSONPointerNavigation, v1.25.2).
//
// The resolver also hands the first definition found for a pointer to every
// later reference spelling it, so its answer depends on declaration order. The
// position is where the definition is written (GitHub #557).
func Target(doc Navigable, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], jsontext.Pointer, bool) {
	if doc == nil || js == nil || !IsPointer(pointer) {
		return nil, "", false
	}
	from := jsontext.Pointer(js.GetCore().GetJSONPointer(doc.GetRootNode()))
	if from == "" {
		return nil, "", false
	}
	if t, ok := localDef(js, pointer); ok {
		return t, from + pointer, true
	}
	return TargetFrom(doc, from, pointer)
}

// TargetFrom is Target for a pointer written at the position from rather than
// on a schema object: the document itself, then each ancestor of from, nearest
// first. A discriminator mapping value is read this way, from the discriminator
// that holds it; the resolver never reads one, so the reading a $ref in the
// same schema gets is the one it gets too.
func TargetFrom(doc Navigable, from, pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], jsontext.Pointer, bool) {
	if doc == nil || !IsPointer(pointer) {
		return nil, "", false
	}
	// The document itself first, as the resolver asks it before any ancestor. An
	// OpenAPI document has no $defs of its own, so this only answers for a
	// standalone schema document.
	if t, ok := defAt(doc, "", pointer); ok {
		return t, pointer, true
	}
	for anc := parentPointer(from); anc != ""; anc = parentPointer(anc) {
		if t, ok := defAt(doc, anc, pointer); ok {
			return t, anc + pointer, true
		}
	}
	return nil, "", false
}

// defAt navigates pointer from the object at anc, as the resolver does with
// that object as the document; a result that is no schema is no answer.
func defAt(doc Navigable, anc, pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], bool) {
	var obj any = doc
	if anc != "" {
		var err error
		if obj, err = jsonpointer.GetTarget(doc, jsonpointer.JSONPointer(anc), jsonpointer.WithStructTags("key")); err != nil {
			return nil, false
		}
	}
	t, err := jsonpointer.GetTarget(obj, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if err != nil {
		return nil, false
	}
	js, ok := t.(*oas3.JSONSchema[oas3.Referenceable])
	return js, ok && js != nil
}

// localDef is tryResolveLocalDefs: a schema carrying its own $id, or sitting
// in a resource whose base differs from its document's, reads "#/$defs/k" in
// its own definitions — k alone, with no path beyond it.
func localDef(js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], bool) {
	s := js.GetSchema()
	if s == nil || !ownResource(s) {
		return nil, false
	}
	key, rest, _ := strings.Cut(string(pointer)[len(defsPrefix):], "/")
	if rest != "" {
		return nil, false
	}
	key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
	defs := s.GetDefs()
	if defs == nil {
		return nil, false
	}
	d, ok := defs.Get(key)
	return d, ok && d != nil
}

func ownResource(s *oas3.Schema) bool {
	if s.GetID() != "" {
		return true
	}
	base := s.GetEffectiveBaseURI()
	reg := s.GetSchemaRegistry()
	return reg != nil && base != "" && base != reg.GetDocumentBaseURI()
}

// parentPointer is the resolver's getParentJSONPointer: everything before the
// last '/', and "" once no ancestor is left — the root itself is never searched.
func parentPointer(p jsontext.Pointer) jsontext.Pointer {
	i := strings.LastIndexByte(string(p), '/')
	if i <= 0 {
		return ""
	}
	return p[:i]
}
