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

// Reader reads "#/$defs/..." pointers in one parsed document by the rule the
// resolver applies to a reference it meets on its own walk, and remembers what
// it has read. References that sit under the same ancestors share the walk down
// to them and the answer each ancestor gives, so a document nested d deep with
// a reference at every level costs O(d) steps rather than O(d²). A Reader is for
// one document and is not safe for concurrent use.
type Reader struct {
	doc Navigable
	// objects is the object at each position read from the root so far.
	objects map[jsontext.Pointer]object
	// nearest is the answer to each question asked of a position so far.
	nearest map[probe]answer
	// reads counts the navigations made of doc.
	reads int
}

// object is what the document holds at a position: the object, if it holds one.
type object struct {
	value any
	held  bool
}

// probe asks which of a position and its ancestors answers a pointer.
type probe struct{ at, pointer jsontext.Pointer }

// answer is the definition an ancestor holds for a pointer and where that
// ancestor sits; def is nil when none does.
type answer struct {
	def *oas3.JSONSchema[oas3.Referenceable]
	at  jsontext.Pointer
}

// NewReader returns a Reader over doc, which may be nil: it then finds nothing.
func NewReader(doc Navigable) *Reader {
	return &Reader{doc: doc, objects: map[jsontext.Pointer]object{}, nearest: map[probe]answer{}}
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
	found := r.above(parentPointer(from), pointer)
	if found.def == nil {
		return nil, "", false
	}
	return found.def, found.at + pointer, true
}

// above returns the closest of anc and its ancestors, the root excepted, whose
// object answers pointer, and remembers the answer for each position it passed
// on the way. Every step shortens the position, which bounds it.
func (r *Reader) above(anc, pointer jsontext.Pointer) answer {
	var passed []jsontext.Pointer
	var found answer
	for at := anc; at != ""; at = parentPointer(at) {
		if remembered, ok := r.nearest[probe{at, pointer}]; ok {
			found = remembered
			break
		}
		if obj, held := r.objectAt(at); held {
			r.reads++
			if def, ok := defAt(obj, pointer); ok {
				found = answer{def: def, at: at}
				r.nearest[probe{at, pointer}] = found
				break
			}
		}
		passed = append(passed, at)
	}
	for _, at := range passed {
		r.nearest[probe{at, pointer}] = found
	}
	return found
}

// objectAt returns the object at pos and whether the document holds one there.
// Each is read from the one at its parent, one token at a time, and remembered,
// so the walk down to a position starts at the closest one already read. A
// position the document lacks has no object below it either, as the resolver
// reading a path from the root finds. Every step shortens the position.
func (r *Reader) objectAt(pos jsontext.Pointer) (any, bool) {
	var unread []jsontext.Pointer
	var obj any = r.doc
	held := true
	for at := pos; at != ""; at = parentPointer(at) {
		if read, ok := r.objects[at]; ok {
			obj, held = read.value, read.held
			break
		}
		unread = append(unread, at)
	}
	for i := len(unread) - 1; i >= 0; i-- {
		at := unread[i]
		if held {
			var err error
			r.reads++
			obj, err = jsonpointer.GetTarget(obj, jsonpointer.JSONPointer(at[len(parentPointer(at)):]), jsonpointer.WithStructTags("key"))
			held = err == nil
		}
		r.objects[at] = object{value: obj, held: held}
	}
	return obj, held
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
