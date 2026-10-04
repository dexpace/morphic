package defs

import (
	"encoding/json/jsontext"
	"reflect"
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

// Reader reads "#/$defs/..." pointers by the rule the resolver applies to a
// reference it meets on its own walk, and remembers what it has read of one
// document. Only an object with a $defs of its own can answer a pointer, so each
// position keeps the closest ancestor that has one: a reference visits those,
// however deep it sits or whatever it spells, and a document nested d deep with
// a reference at every level costs O(d) steps and memory, not O(d²). It is not
// safe for concurrent use.
type Reader struct {
	doc Navigable
	// places is what the document holds at each position read from the root so
	// far.
	places map[jsontext.Pointer]place
	// reads counts the navigations made of doc.
	reads int
}

// place is a position the reader has read: the object the document holds there,
// if it holds one; whether that object has a $defs of its own; and the closest
// proper ancestor that does, the root excepted, or "" for none.
type place struct {
	value   any
	held    bool
	hasDefs bool
	holder  jsontext.Pointer
}

// NewReader returns a Reader over doc, which may be nil: it then finds nothing.
func NewReader(doc Navigable) *Reader {
	return &Reader{doc: doc, places: map[jsontext.Pointer]place{}}
}

// Reads reports how many times r has navigated its document, which is the work
// it has done. Because it remembers, that stays proportional to the positions
// it has read however many references ask about them; a test holds it to that.
func (r *Reader) Reads() int {
	if r == nil {
		return 0
	}
	return r.reads
}

// Doc returns the document r reads, or nil for a nil Reader.
func (r *Reader) Doc() Navigable {
	if r == nil {
		return nil
	}
	return r.doc
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
func (r *Reader) Target(js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], jsontext.Pointer, bool) {
	if r.Doc() == nil || js == nil || !IsPointer(pointer) {
		return nil, "", false
	}
	from := jsontext.Pointer(js.GetCore().GetJSONPointer(r.doc.GetRootNode()))
	if from == "" {
		return nil, "", false
	}
	if t, ok := localDef(js, pointer); ok {
		return t, from + pointer, true
	}
	return r.TargetFrom(from, pointer)
}

// TargetFrom is Target for a pointer written at the position from rather than
// on a schema object: the document itself, then each ancestor of from, nearest
// first. A discriminator mapping value is read this way, from the discriminator
// that holds it; the resolver never reads one, so the reading a $ref in the
// same schema gets is the one it gets too.
func (r *Reader) TargetFrom(from, pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], jsontext.Pointer, bool) {
	if r.Doc() == nil || !IsPointer(pointer) {
		return nil, "", false
	}
	// The document itself first, as the resolver asks it before any ancestor. An
	// OpenAPI document has no $defs of its own, so this only answers for a
	// standalone schema document.
	r.reads++
	if t, ok := defAt(r.doc, pointer); ok {
		return t, pointer, true
	}
	anc := parentPointer(from)
	if anc == "" {
		return nil, "", false
	}
	here := r.placeAt(anc)
	if !here.hasDefs {
		anc = here.holder
	}
	// Each holder is closer to the root than the last, which bounds the walk.
	for anc != "" {
		here = r.placeAt(anc)
		r.reads++
		if t, ok := defAt(here.value, pointer); ok {
			return t, anc + pointer, true
		}
		anc = here.holder
	}
	return nil, "", false
}

// placeAt returns what the document holds at pos. Each position is read from
// the one at its parent, one token at a time, and remembered, so the walk down
// to a position starts at the closest one already read. A position the document
// lacks has no object below it either, as the resolver reading a path from the
// root finds. Every step shortens the position.
func (r *Reader) placeAt(pos jsontext.Pointer) place {
	var unread []jsontext.Pointer
	here := place{value: r.doc, held: true}
	for at := pos; at != ""; at = parentPointer(at) {
		if read, ok := r.places[at]; ok {
			here = read
			break
		}
		unread = append(unread, at)
	}
	for i := len(unread) - 1; i >= 0; i-- {
		at := unread[i]
		parent := parentPointer(at)
		next := place{}
		if here.held {
			r.reads++
			value, err := jsonpointer.GetTarget(here.value, jsonpointer.JSONPointer(at[len(parent):]), jsonpointer.WithStructTags("key"))
			next.value, next.held = value, err == nil
		}
		if next.held {
			r.reads++
			next.hasDefs = definesDefs(next.value)
		}
		next.holder = here.holder
		if here.hasDefs { // never the root, whose place is not read for one
			next.holder = parent
		}
		r.places[at] = next
		here = next
	}
	return here
}

// definesDefs reports whether obj has a $defs of its own. An object that does
// not answers a pointer for no reference, so the search never probes it. The
// library reports an absent $defs as an empty value rather than an error.
func definesDefs(obj any) bool {
	t, err := jsonpointer.GetTarget(obj, "/$defs", jsonpointer.WithStructTags("key"))
	if err != nil || t == nil {
		return false
	}
	switch v := reflect.ValueOf(t); v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return !v.IsNil()
	default:
		return true
	}
}

// defAt navigates pointer from obj, as the resolver does with obj as the
// document; a result that is no schema is no answer.
func defAt(obj any, pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], bool) {
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
