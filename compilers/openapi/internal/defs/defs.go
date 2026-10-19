package defs

import (
	"encoding/json/jsontext"
	"reflect"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
)

type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// defsPrefix opens every pointer the resolver reads relative to the schema that
// spells it rather than to the document (oas3 resolveDefsReference).
// defsRoot is the position it opens.
const (
	defsRoot   = "/$defs"
	defsPrefix = defsRoot + "/"
)

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

// PointerOf returns the "#/$defs/..." pointer ref spells, decoded as
// fragment.Pointer decodes it, and whether the resolver reads it relative to
// the schema that spells it. It does not when ref names a document, even this
// one: the resolver reads such a pointer for itself, which is what load leaves
// to it.
func PointerOf(ref references.Reference) (jsontext.Pointer, bool) {
	pointer := fragment.Pointer(string(ref))
	if ref.GetURI() != "" || !IsPointer(pointer) {
		return "", false
	}
	return pointer, true
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
	// positions is where the containers of doc's tree sit, indexed on first use.
	positions *positions
	// roots is what each pointer read from the document's root found.
	roots map[jsontext.Pointer]rootRead
	// raw reads a pointer on in raw YAML where the root's read leaves the
	// model; nil asks the library.
	raw RawRead
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

// rootRead is what a pointer read from the document's root found, and whether
// it found anything.
type rootRead struct {
	def   Definition
	found bool
}

// Definition is what a "#/$defs/..." pointer names: the position the definition
// is written at, and the YAML node written there.
type Definition struct {
	At   jsontext.Pointer
	Node *yaml.Node
}

// RawRead returns the node the library's read of tokens finds in raw, the raw
// YAML a read of the model went on in (jsonpointer.GetTarget), and false where
// it finds none.
type RawRead func(raw *yaml.Node, tokens []string) (*yaml.Node, bool)

// NewReader returns a Reader over doc, which may be nil: it then finds nothing.
// It reads raw YAML by asking the library, which scans a mapping for each read.
func NewReader(doc Navigable) *Reader {
	return NewIndexedReader(doc, nil)
}

// NewIndexedReader is NewReader reading raw YAML through raw, which answers as
// the library's read does from an index of each mapping, so a wide one costs
// its width once however many pointers are read through it (GitHub #778). A
// nil raw asks the library.
func NewIndexedReader(doc Navigable, raw RawRead) *Reader {
	return &Reader{doc: doc, places: map[jsontext.Pointer]place{}, roots: map[jsontext.Pointer]rootRead{}, raw: raw}
}

// Indexed reports whether r reads raw YAML through the RawRead it was given,
// an index, rather than by asking the library, which scans a mapping on every
// read (GitHub #778).
func (r *Reader) Indexed() bool {
	return r != nil && r.raw != nil
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

// Target returns the definition a same-document "#/$defs/..." pointer written
// at js names, by the resolver's rule for a reference it meets on its own walk:
// js's own definitions when it is a resource of its own (localDef), else the
// position the pointer names from the document's root, else the definitions
// of the nearest ancestor holding the path (oas3 tryResolveLocalDefs, then
// references.Resolve, then tryResolveDefsUsingJSONPointerNavigation). The
// position is where the definition is written (GitHub #557).
//
// Unlike the resolver's, the answer does not depend on declaration order
// (docs/parser-library.md#defs-order). The root answers wherever the pointer
// lands, raw YAML included, unless js shadows it (Shadowed)
// (docs/parser-library.md#root-defs).
func (r *Reader) Target(js *schemaRef, pointer jsontext.Pointer) (Definition, bool) {
	def, found, shadowed := r.answer(js, pointer)
	if shadowed {
		return Definition{}, false
	}
	return def, found
}

// Shadowed reports whether js, though no schema resource of its own, holds a
// definition under the one key pointer names while the document's root holds
// another, which Target would name. The resolver reads js's own instead once a
// reference chain has reached js, so which one it names turns on what it
// resolved first, and Target names none (docs/parser-library.md#root-defs).
func (r *Reader) Shadowed(js *schemaRef, pointer jsontext.Pointer) bool {
	_, _, shadowed := r.answer(js, pointer)
	return shadowed
}

// answer is Target's reading of pointer at js before Shadowed's applies, and
// whether it does.
func (r *Reader) answer(js *schemaRef, pointer jsontext.Pointer) (Definition, bool, bool) {
	if js == nil || !IsPointer(pointer) {
		return Definition{}, false, false
	}
	from := r.positionOf(js.GetCore())
	if from == "" {
		return Definition{}, false, false
	}
	if t, ok := localDef(js, pointer); ok {
		return definition(t, from+pointer), true, false
	}
	def, found := r.targetFrom(from, pointer)
	if !found || def.At != pointer {
		return def, found, false
	}
	own, ok := ownDef(js.GetSchema(), pointer)
	return def, true, ok && resolveAlias(own.GetCore().GetRootNode()) != resolveAlias(def.Node)
}

// MappingTarget is Target for a "#/$defs/..." value of the mapping of d, read
// from the discriminator's own position. The resolver never reads a mapping
// value (docs/parser-library.md#mapping-unresolved), so it is read as a $ref
// written in the same schema would be.
func (r *Reader) MappingTarget(d *oas3.Discriminator, pointer jsontext.Pointer) (Definition, bool) {
	if d == nil || !IsPointer(pointer) {
		return Definition{}, false
	}
	from := r.positionOf(d.GetCore())
	if from == "" {
		return Definition{}, false
	}
	return r.targetFrom(from, pointer)
}

// definition is the Definition of the schema js, written at at.
func definition(js *schemaRef, at jsontext.Pointer) Definition {
	return Definition{At: at, Node: js.GetCore().GetRootNode()}
}

// positioned is a parsed object that knows the node it was built from.
type positioned interface {
	GetRootNode() *yaml.Node
}

// positionOf returns the pointer to where obj sits in r's document, as the
// library's GetJSONPointer finds it, or "" when r has no document or obj is not
// in it. The document itself is "/", and no $defs pointer is read from there.
func (r *Reader) positionOf(obj positioned) jsontext.Pointer {
	if r.Doc() == nil {
		return ""
	}
	if r.positions == nil {
		r.positions = indexPositions(r.doc.GetRootNode())
	}
	return r.positions.pointerOf(obj.GetRootNode())
}

// targetFrom reads pointer as written at the position from, which is in the
// document: from the document's root, then each ancestor of from, nearest
// first.
func (r *Reader) targetFrom(from, pointer jsontext.Pointer) (Definition, bool) {
	if read := r.rootDef(pointer); read.found {
		return read.def, true
	}
	// Only an object with a $defs of its own can answer, and each holder is
	// closer to the root than the last, which bounds the walk.
	for anc := from.Parent(); anc != ""; {
		here := r.placeAt(anc)
		if here.hasDefs {
			r.reads++
			if t, ok := defAt(here.value, pointer); ok {
				return definition(t, anc+pointer), true
			}
		}
		anc = here.holder
	}
	return Definition{}, false
}

// rootDef reads pointer from the document's root, as the resolver reads a
// pointer from the document before any ancestor, and remembers the answer. It
// lands wherever the pointer does: on a schema of the model, or in raw YAML
// the model leaves the read to, which the reference then reads as a schema, as
// it does any other position a root pointer names (GitHub #910). A position
// the model holds something other than a schema at is no answer.
func (r *Reader) rootDef(pointer jsontext.Pointer) rootRead {
	if read, ok := r.roots[pointer]; ok {
		return read
	}
	r.reads++
	read := rootRead{def: Definition{At: pointer}}
	read.def.Node, read.found = r.rootNode(pointer)
	r.roots[pointer] = read
	return read
}

// rootNode returns the node pointer names from the document's root, and
// whether it names a schema of the model or raw YAML. Past where it leaves the
// model, the rest is read as the library reads raw YAML, which is how the
// resolver goes on there (see RawRead).
func (r *Reader) rootNode(pointer jsontext.Pointer) (*yaml.Node, bool) {
	tokens, ok := navigation.Tokens(pointer)
	if !ok {
		return nil, false
	}
	t, rest, err := navigation.Walk(r.doc, tokens)
	if err != nil {
		return nil, false
	}
	if raw, leaves := t.(*yaml.Node); leaves && len(rest) > 0 {
		return r.rawNode(raw, rest)
	}
	js, ok := t.(*schemaRef)
	if !ok || js == nil {
		return nil, false
	}
	return js.GetCore().GetRootNode(), true
}

// rawNode is rootNode's read of the tokens left in raw, the raw YAML the walk
// from the root left the model for.
func (r *Reader) rawNode(raw *yaml.Node, rest []string) (*yaml.Node, bool) {
	read := r.raw
	if read == nil {
		read = libraryRead
	}
	n, found := read(raw, rest)
	if n = resolveAlias(n); !found || n == nil {
		return nil, false
	}
	return n, true
}

// libraryRead is the RawRead the library's own read answers.
func libraryRead(raw *yaml.Node, tokens []string) (*yaml.Node, bool) {
	t, err := jsonpointer.GetTarget(raw, jsonpointer.PartsToJSONPointer(tokens), jsonpointer.WithStructTags("key"))
	n, ok := t.(*yaml.Node)
	return n, err == nil && ok
}

// placeAt returns what the document holds at pos. Each position is read from
// the one at its parent and remembered, so the walk down to a position starts
// at the closest one already read. Every step shortens the position.
// Pointer.Parent is the resolver's getParentJSONPointer: everything before the
// last '/', and "" once none is left, so the root itself is never searched.
func (r *Reader) placeAt(pos jsontext.Pointer) place {
	var unread []jsontext.Pointer
	here := place{value: r.doc, held: true}
	for at := pos; at != ""; at = at.Parent() {
		if read, ok := r.places[at]; ok {
			here = read
			break
		}
		unread = append(unread, at)
	}
	for _, u := range slices.Backward(unread) {
		here = r.read(here, u)
		r.places[u] = here
	}
	return here
}

// read returns the place at, one token below parent, as the resolver's walk
// reads it on (navigation.Step). A position the document lacks has no object
// below it either, as the resolver reading a path from the root finds, and nor
// does one in raw YAML, which holds no schema to read a $defs pointer in.
func (r *Reader) read(parent place, at jsontext.Pointer) place {
	above := at.Parent()
	next := place{holder: parent.holder}
	if parent.hasDefs { // never the root, whose place is not read for one
		next.holder = above
	}
	if !parent.held {
		return next
	}
	r.reads++
	tokens, ok := navigation.Tokens(at[len(above):])
	if !ok || len(tokens) != 1 {
		return next
	}
	if _, raw := navigation.Leaves(parent.value, tokens[0]); raw {
		return next
	}
	value, ok := navigation.Step(parent.value, tokens[0])
	if !ok {
		return next
	}
	r.reads++
	next.value, next.held, next.hasDefs = value, true, definesDefs(value)
	return next
}

// definesDefs reports whether obj has a $defs of its own. An object that does
// not answers a pointer for no reference, so the search never probes it, and
// nor does one whose "$defs" is raw YAML, which holds no schema. The library
// reports an absent $defs as an empty value rather than an error.
func definesDefs(obj any) bool {
	if _, raw := navigation.Leaves(obj, "$defs"); raw {
		return false
	}
	t, ok := navigation.Step(obj, "$defs")
	if !ok || t == nil {
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
// document; a result that is no schema is no answer. The walk stops where the
// pointer leaves the model, since raw YAML holds no schema, so it scans none
// of the mappings the library would (GitHub #778).
func defAt(obj any, pointer jsontext.Pointer) (*schemaRef, bool) {
	tokens, ok := navigation.Tokens(pointer)
	if !ok {
		return nil, false
	}
	t, rest, err := navigation.Walk(obj, tokens)
	if err != nil || len(rest) > 0 {
		return nil, false
	}
	js, ok := t.(*schemaRef)
	return js, ok && js != nil
}

// localDef is tryResolveLocalDefs: a schema carrying its own $id, or sitting
// in a resource whose base differs from its document's, reads "#/$defs/k" in
// its own definitions (ownDef).
func localDef(js *schemaRef, pointer jsontext.Pointer) (*schemaRef, bool) {
	s := js.GetSchema()
	if s == nil || !ownResource(s) {
		return nil, false
	}
	return ownDef(s, pointer)
}

// ownDef returns the definition s, which may be nil, holds in its own $defs
// under k, for a pointer "/$defs/k" with no path beyond k.
//
// The key is decoded as jsontext decodes a token, without the validation
// navigation.Tokens applies, since tryResolveLocalDefs applies none. A trailing
// empty token is read as no token, which the library does not do (GitHub #843).
func ownDef(s *oas3.Schema, pointer jsontext.Pointer) (*schemaRef, bool) {
	tokens := slices.Collect(pointer[len(defsRoot):].Tokens())
	if len(tokens) == 2 && tokens[1] == "" {
		tokens = tokens[:1]
	}
	if len(tokens) != 1 {
		return nil, false
	}
	defs := s.GetDefs()
	if defs == nil {
		return nil, false
	}
	d, ok := defs.Get(tokens[0])
	return d, ok && d != nil
}

// ownResource reports whether s is a schema resource of its own: it carries an
// $id, or sits in a resource whose base differs from its document's.
func ownResource(s *oas3.Schema) bool {
	if s.GetID() != "" {
		return true
	}
	base := s.GetEffectiveBaseURI()
	reg := s.GetSchemaRegistry()
	return reg != nil && base != "" && base != reg.GetDocumentBaseURI()
}
