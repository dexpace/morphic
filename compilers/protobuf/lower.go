package protobuf

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/dexpace/morphic/ir"
)

// maxCommentLines caps how many leading-comment lines a Docs description keeps,
// bounding work on pathologically commented sources (styleguide bounded-loops).
const maxCommentLines = 4096

// lowerer is the single mutable context of one Compile call: a local, never a
// package global. It threads the IR document under construction, the interning
// guard, accumulated diagnostics, and the resolved options through the walk.
type lowerer struct {
	srcIndex int
	file     protoreflect.FileDescriptor
	source   ir.SourceInfo
	out      *ir.Document
	opts     Options
	diags    []ir.Diagnostic
}

// newLowerer allocates a lowerer over one loaded file, with an empty IR document
// and type registry ready for lowering.
//
//nolint:unparam // srcIndex varies once Compile drives a multi-source loop
func newLowerer(srcIndex int, ld *loaded, opts Options) *lowerer {
	return &lowerer{
		srcIndex: srcIndex,
		file:     ld.File,
		source:   ld.Source,
		out:      &ir.Document{Types: ir.TypeRegistry{}},
		opts:     opts,
	}
}

// lowerTypes hoists every message and enum declared in the file, recursing into
// nested declarations. Interning by ID makes each one lower exactly once, so a
// type unreferenced by any service still lands in the registry (as component
// schemas do in the OpenAPI compiler).
func (l *lowerer) lowerTypes() {
	msgs := l.file.Messages()
	for i := range msgs.Len() {
		l.hoistMessage(msgs.Get(i))
	}
	enums := l.file.Enums()
	for i := range enums.Len() {
		l.enumRef(enums.Get(i))
	}
}

// hoistMessage interns md and recurses into its nested messages and enums. Map
// entry messages are synthetic (they model map<K,V> storage) and never hoisted;
// their fields are read directly through the map field.
func (l *lowerer) hoistMessage(md protoreflect.MessageDescriptor) {
	if md.IsMapEntry() {
		return
	}
	l.messageRef(md)
	nested := md.Messages()
	for i := range nested.Len() {
		l.hoistMessage(nested.Get(i))
	}
	enums := md.Enums()
	for i := range enums.Len() {
		l.enumRef(enums.Get(i))
	}
}

// messageRef interns the Model for md and returns a reference to it. The Model is
// registered before its fields are lowered, so a self- or cyclic reference
// reached during the walk resolves to the interned ID instead of recursing.
func (l *lowerer) messageRef(md protoreflect.MessageDescriptor) ir.TypeRef {
	id := namedTypeID(string(md.FullName()))
	if _, ok := l.out.Types[id]; !ok {
		m := &ir.Model{TypeCommon: l.namedCommon(id, md)}
		l.out.Types[id] = m
		l.fillModel(m, md)
	}
	return ir.TypeRef{Target: id}
}

// enumRef interns the Enum for ed and returns a reference to it.
func (l *lowerer) enumRef(ed protoreflect.EnumDescriptor) ir.TypeRef {
	id := namedTypeID(string(ed.FullName()))
	if _, ok := l.out.Types[id]; !ok {
		e := &ir.Enum{TypeCommon: l.namedCommon(id, ed)}
		l.out.Types[id] = e
		l.fillEnum(e, ed)
	}
	return ir.TypeRef{Target: id}
}

// primRef interns the primitive of kind k under its shared ID on first use and
// returns a reference to it. Primitives are leaves and never carry provenance
// pointers.
func (l *lowerer) primRef(k ir.PrimKind) ir.TypeRef {
	id := primTypeID(k)
	if _, ok := l.out.Types[id]; !ok {
		l.out.Types[id] = &ir.Primitive{
			TypeCommon: ir.TypeCommon{ID: id, Provenance: ir.Provenance{Source: l.srcIndex}},
			Prim:       k,
		}
	}
	return ir.TypeRef{Target: id}
}

// anyRef interns the shared schemaless Any node and returns a reference to it.
func (l *lowerer) anyRef() ir.TypeRef {
	if _, ok := l.out.Types[anyTypeID]; !ok {
		l.out.Types[anyTypeID] = &ir.Any{
			TypeCommon: ir.TypeCommon{
				ID:         anyTypeID,
				Name:       ir.Naming{Hint: "any"},
				Anonymous:  true,
				Provenance: ir.Provenance{Source: l.srcIndex},
			},
		}
	}
	return ir.TypeRef{Target: anyTypeID}
}

// namedCommon builds the TypeCommon shared by a hoisted message or enum: its
// stable ID, source/canonical name, declared package namespace, docs, and
// deprecation.
func (l *lowerer) namedCommon(id ir.TypeID, d protoreflect.Descriptor) ir.TypeCommon {
	name := string(d.Name())
	common := ir.TypeCommon{
		ID:         id,
		Name:       ir.Naming{Source: name, Canonical: canonicalWords(name)},
		Namespace:  packageWords(string(l.file.Package())),
		Docs:       l.docsFor(d),
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: string(d.FullName())},
	}
	if dep := deprecationOf(d); dep != nil {
		common.Deprecation = dep
	}
	return common
}

// anonCommon builds the TypeCommon of a hoisted anonymous type (a container or a
// oneof union), carrying only a context-derived naming hint.
func (l *lowerer) anonCommon(id ir.TypeID, pointer, hint string) ir.TypeCommon {
	return ir.TypeCommon{
		ID:         id,
		Name:       ir.Naming{Hint: hint},
		Anonymous:  true,
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: pointer},
	}
}

// docsFor builds Docs from a declaration's leading source comment.
func (l *lowerer) docsFor(d protoreflect.Descriptor) ir.Docs {
	loc := l.file.SourceLocations().ByDescriptor(d)
	desc := cleanComment(loc.LeadingComments)
	if desc == "" {
		return ir.Docs{}
	}
	return ir.Docs{Description: desc}
}

// cleanComment normalizes a proto leading comment block into a plain paragraph:
// it strips the single leading space protoc records on each line and trims
// surrounding blank lines, holding no Markdown opinion.
func cleanComment(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	if len(lines) > maxCommentLines {
		lines = lines[:maxCommentLines]
	}
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
