// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// EffectiveDeprecated reports whether a schema of chain
// (SchemaPositionAnnotationSources.Chain) is deprecated. JSON Schema reads a
// location deprecated where any occurrence of the keyword applying to it is
// true, and an occurrence false as one omitted, so no schema of a chain
// withdraws another's.
func EffectiveDeprecated(chain []*statement.Schema) bool {
	return anyFlag(chain, func(notes *statement.Notes) *bool { return notes.Deprecated })
}

// EffectiveVisibility maps readOnly/writeOnly to a lifecycle visibility set
// (docs/ir/properties.md#visibility): readOnly is present in every response
// lifecycle and absent only from requests; writeOnly is create+update. The bool
// reports both flags in force, which no lifecycle satisfies. It is not a
// diagnostic because this reader has no provenance of its own.
//
// Each flag is in force where any schema of chain sets it, as JSON Schema
// reads both (docs/compilers/openapi.md#ref-use-site).
//
// Both in force is contradictory but legal: JSON Schema forbids neither. Read
// as sets they leave nothing, which Visibility{None: true} states, as
// merge.mergeVisibility answers for the pair spread over two allOf branches
// (GitHub #276).
func EffectiveVisibility(chain []*statement.Schema) (ir.Visibility, bool) {
	readOnly := anyFlag(chain, func(notes *statement.Notes) *bool { return notes.ReadOnly })
	writeOnly := anyFlag(chain, func(notes *statement.Notes) *bool { return notes.WriteOnly })

	switch {
	case readOnly && writeOnly:
		return ir.Visibility{InvisibleInEveryLifecycle: true}, true
	case readOnly:
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}, false
	case writeOnly:
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, false
	default:
		return ir.Visibility{}, false
	}
}

// anyFlag reports whether a schema of chain sets the bool field accessor
// extracts. It is the one reading of the boolean annotations JSON Schema
// defines (deprecated, readOnly, writeOnly): each in force where any
// occurrence is true, and false the same as absent.
func anyFlag(chain []*statement.Schema, accessor func(*statement.Notes) *bool) bool {
	return slices.ContainsFunc(chain, func(schema *statement.Schema) bool {
		v := (*bool)(nil)
		if notes := notesIn(schema); notes != nil {
			v = accessor(notes)
		}
		return v != nil && *v
	})
}

// FillTypeDocs maps a schema's title, description, and externalDocs onto Docs.
// Every field is assigned, never accumulated: a schema declares at most one
// externalDocs, and a pointer read twice (a sub-schema lowered both in place
// and through a $ref that hoists it) must not end up with two copies of it.
func FillTypeDocs(docs *ir.Docs, schema *statement.Schema) {
	notes := notesIn(schema)
	if notes == nil {
		return
	}
	if notes.Title != "" {
		docs.Summary = notes.Title
	}
	if notes.Description != "" {
		docs.Description = notes.Description
	}
	if ed := notes.ExternalDocs; ed != nil {
		docs.ExternalDocs = []ir.Link{{URL: ed.URL, Description: ed.Description}}
	}
}

// FillCarrierDocs fills the ir.Property or ir.Parameter carrying a position
// with the documentation effective there: the title, description and
// externalDocs of chain's farthest schema first, then each nearer one's over
// them, field by field. A bare `$ref` therefore reads all three from the
// referent.
//
// Both ends are deliberate. The use site is the only home a keyword written at
// the position has once the body reduced to a shared node (GitHub #116). The
// referent is the rule docs/compilers/openapi.md#ref-use-site: ref-target
// annotations merge onto the referencing Property/Parameter with use-site
// precedence, uniformly, constraints excepted, while the referent keeps its own
// copy on its node.
func FillCarrierDocs(docs *ir.Docs, chain []*statement.Schema) {
	for _, schema := range slices.Backward(chain) {
		FillTypeDocs(docs, schema)
	}
}

// XMLHints maps an OpenAPI XML object onto ir.XMLHints; an attribute flag
// becomes the "attribute" node type. A field xml leaves unset stays unset, so a
// declared false can override a type's true field by field.
func XMLHints(xml *statement.XML) *ir.XMLHints {
	if xml == nil {
		return nil
	}
	xmlHints := &ir.XMLHints{}
	if xml.Name != nil {
		xmlHints.Name = *xml.Name
	}
	if xml.Namespace != nil {
		xmlHints.Namespace = *xml.Namespace
	}
	if xml.Prefix != nil {
		xmlHints.Prefix = *xml.Prefix
	}
	if xml.Wrapped != nil {
		xmlHints.Wrapped = new(*xml.Wrapped)
	}
	if xml.Attribute != nil {
		xmlHints.Attribute = new(*xml.Attribute)
	}
	return xmlHints
}

// The two site kinds. Declaration is a position that writes a schema of its
// own; Reference is one that points at another and may still annotate the
// pointer.
const (
	Declaration Kind = iota
	Reference
)

const (
	// HomeOwnNode marks a position with no home but a type node: items,
	// additionalProperties, prefixItems, patternProperties, a union branch, a
	// media-type schema, a component, a $ref-hoisted sub-schema. A declaration
	// there hoists an alias rather than lose what it wrote.
	HomeOwnNode Home = iota
	// HomeCarrier marks a position whose caller carries an ir.Property or
	// ir.Parameter that holds the declaration's annotations itself
	// (FillPropertyDetail, fillParamSchema): a model property, a response or
	// part header, an operation parameter. Hoisting there would give one
	// declaration two homes.
	HomeCarrier
)

// Kind distinguishes a position that declares a type from one that
// references another type and may carry annotations of its own.
type Kind int

// SchemaPositionAnnotationSources is what a schema position declares: Node is
// the schema written there, Folded branches the inline allOf branches read as
// written beside it, and Referents, honoured only for a reference site, what a
// use site's annotations fall back to, nearest first.
//
// A Site At builds carries none of either: a declaration's annotations bind the
// position they are written at (attachDeclaredAnnotations), and a component
// that aliases another keeps the target's annotations reachable through its
// Base. A reader that inherits, FillPropertyDetail (schema/property.go) or
// fillParamSchema, builds its own Site (schema.UseOf).
type SchemaPositionAnnotationSources struct {
	Kind Kind
	Node *statement.Schema
	// FoldedAllOfBranches are the branches read as written at Node
	// (docs/compilers/openapi.md#allof-inline-residue).
	FoldedAllOfBranches []FoldedAllOfBranch
	// Referents is empty when Kind is Reference but the $ref's chain ends on no
	// schema; refTypeRef is what diagnoses that, not this.
	Referents []*statement.Schema
	// Dialect is what the dialect Node is read in spells otherwise than 2020-12.
	Dialect Dialect
	// Closure says the node Node lowered to holds its `unevaluatedProperties:
	// false` as its openness, so no entry keeps it. The zero value keeps it.
	Closure bool
	// Leaves names the keywords Node writes that Read keeps no entry of: an
	// inline allOf branch the position keeps whole writes them alike, and that
	// entry holds the one writing (docs/compilers/openapi.md#allof-refinement).
	Leaves []string
}

// Dialect is what a schema's dialect spells otherwise than JSON Schema
// 2020-12, as the readers here need it: the Identifier keyword
// (specrules.SchemaRules.Identifier), "" for `$id`, whether the identifier is
// IdentifierIgnored, as the drafts before 2019-09 ignore every keyword beside
// a `$ref` (specrules.SchemaRules.IgnoresRefFields), whether it defines
// Dependencies, the drafts' validation-only keyword 2019-09 split in two, and
// whether it reads a ListedTuple, an `items` list whose rest is the
// `additionalItems` beside it, as the drafts before 2020-12 spell a tuple.
type Dialect struct {
	Identifier        string
	IdentifierIgnored bool
	Dependencies      bool
	ListedTuple       bool
}

// FoldedAllOfBranch is an inline allOf branch and where it is written, so what
// it declares is read, and reported, at its own pointer, in its own dialect.
type FoldedAllOfBranch struct {
	Node *statement.Schema
	At   jsontext.Pointer
	// Dependencies says the dialect Node is written in defines `dependencies`
	// (Dialect.Dependencies), which a branch naming its own may, whatever the
	// position's.
	Dependencies bool
	// KeptWhole says the position keeps the branch whole, an entry holding what
	// it writes, so it writes no entry Read keeps beside it but one no other
	// folded branch writes (docs/compilers/openapi.md#allof-inline-residue).
	KeptWhole bool
	// Beside says Node is the position's own schema, read beside the inline
	// allOf branch its node is built from: what it writes stands before that
	// branch's, as the keywords beside an allOf do before an entry's.
	Beside bool
}

// Chain returns what speaks for the site, nearest first: its own schema, its
// folded branches, then the referents a reference site falls back to.
func (st SchemaPositionAnnotationSources) Chain() []*statement.Schema {
	chain := make([]*statement.Schema, 0, 1+len(st.FoldedAllOfBranches)+len(st.Referents))
	chain = append(chain, st.Node)
	for _, foldedAllOfBranch := range st.FoldedAllOfBranches {
		chain = append(chain, foldedAllOfBranch.Node)
	}
	if st.Kind == Declaration {
		return chain // a declaration has no target to fall back to
	}
	return append(chain, st.Referents...)
}

// At builds the site for js, a reference site's without its Referents (see
// Site).
//
// A schema whose $ref is present but empty ({$ref: ""}) is a declaration, not a
// reference site: an empty ref resolves nowhere, so its Text is empty.
//
// At trusts its caller that js is the schema at the position being modeled.
func At(js *statement.Statement) SchemaPositionAnnotationSources {
	schemaPositionAnnotationSources := SchemaPositionAnnotationSources{Kind: Declaration}
	if js == nil || js.Truth != statement.Stated {
		return schemaPositionAnnotationSources
	}
	schemaPositionAnnotationSources.Node = js.Schema
	if js.Ref != nil && js.Ref.Text != "" {
		schemaPositionAnnotationSources.Kind = Reference
	}
	return schemaPositionAnnotationSources
}

// Home names where the annotations a schema position declares are
// kept. It is what decides whether a position that lowered to a shared node
// hoists one of its own, so the two homes can never both hold the same
// declaration (GitHub #116).
type Home int

// The readers below consume a Site the caller supplies rather than resolving
// one themselves, since obtaining a referent is reference resolution — walk
// work. SchemaPositionAnnotationSources.Referents holds each hop of the $ref's
// chain, nearest first.

// Set is everything a site's annotations yield, before any of it is
// attached to a carrier.
//
// The readers produce a value; the caller decides what its carrier can hold. A
// Parameter has no XML field and a TypeCommon has no Required, so the shape of
// the write differs per carrier while the reading does not.
type Set struct {
	Docs       ir.Docs
	Deprecated bool
	// Sensitive is a redaction the site or its referent asks for
	// (DeclaresRedaction). A type node holds it as Sensitive, and a property or
	// a parameter as Secret.
	Sensitive bool
	XML       *ir.XMLHints
	Examples  []ir.Example
	Unmodeled ir.Unmodeled
}

// redactionFormat is the one format OpenAPI defines that names no shape: its
// format table gives it as a hint to obscure the value.
const redactionFormat = "password"

// DeclaresRedaction reports whether schema asks for its value to be redacted,
// by writing `format: password`. It is honoured whatever type schema declares:
// the table lists it for strings, but hiding a value no one needed hidden is
// the safe reading. It is an annotation rather than a (type, format) pairing,
// so the shape a position lowers to is read without it.
func DeclaresRedaction(schema *statement.Schema) bool {
	return schema != nil && schema.Format != nil && schema.Format.Name == redactionFormat
}

// Read reads every site-local annotation at st: what its schema writes at
// pointer, then what each foldedBranch writes at its own. A foldedBranch agrees
// with what spoke before it wherever both write a keyword
// (schema.foldingAllOfBranches), so an entry is kept once: the site's own,
// else, where several folded branches kept apart write it, at pointer, no one
// writer's (underlay). A folded branch's unevaluated keywords fold nowhere
// (schema.foldEntry): its branch, kept whole, is their one record.
//
// This is the one place the site-versus-referent choice is made, so no
// position attaching annotations makes it apart.
func Read(st SchemaPositionAnnotationSources, pointer jsontext.Pointer, doc provenance.Doc) (Set, []ir.Diagnostic) {
	out, diags := readLocal(st.Node, pointer, doc, true, st.Dialect, st.Closure)
	for _, key := range leftEntries(st) {
		if _, kept := out.Unmodeled[key]; kept {
			delete(out.Unmodeled, key)
			diags = lessNote(diags, doc.At(pointer), key)
		}
	}
	var underlayValue underlay
	if len(st.FoldedAllOfBranches) > 0 {
		underlayValue.own = make(map[string]bool, len(out.Unmodeled))
		for key := range out.Unmodeled {
			underlayValue.own[key] = true
		}
	}
	for _, foldedAllOfBranch := range st.FoldedAllOfBranches {
		foldedBranch, foldedBranchDiags := readLocal(foldedAllOfBranch.Node, foldedAllOfBranch.At, doc, false, Dialect{Dependencies: foldedAllOfBranch.Dependencies}, false)
		for key := range foldedBranch.Unmodeled {
			if underlayValue.own[key] {
				// The site keeps the one writing, and its report says so.
				foldedBranchDiags = lessNote(foldedBranchDiags, doc.At(foldedAllOfBranch.At), key)
			}
		}
		diags = append(diags, foldedBranchDiags...)
		if out.XML == nil {
			out.XML = foldedBranch.XML
		}
		if len(out.Examples) == 0 {
			out.Examples = foldedBranch.Examples
		}
		out.Unmodeled = underlayValue.add(out.Unmodeled, foldedBranch.Unmodeled, foldedAllOfBranch.KeptWhole, doc.At(pointer))
	}
	chain := st.Chain()
	FillCarrierDocs(&out.Docs, chain)
	out.Deprecated = EffectiveDeprecated(chain)
	out.Sensitive = slices.ContainsFunc(chain, DeclaresRedaction)
	return out, diags
}

// leftEntries returns the entries Read keeps none of for st.Leaves: the one
// each keyword is kept under, and the one several keywords fold into, if, then
// and else or contains and its counts, where the site leaves every one of
// them it writes.
func leftEntries(st SchemaPositionAnnotationSources) []string {
	if len(st.Leaves) == 0 {
		return nil
	}
	out := make([]string, 0, len(st.Leaves)+2)
	for _, keyword := range st.Leaves {
		out = append(out, EntryKey("", keyword))
	}
	left := func(keyword string) bool { return slices.Contains(st.Leaves, keyword) }
	if _, written, _ := IfThenElseRaw(st.Node); len(written) > 0 && !slices.ContainsFunc(written, func(k string) bool { return !left(k) }) {
		out = append(out, "openapi:if-then-else")
	}
	if _, written, _ := ContainsRaw(st.Node); len(written) > 0 && !slices.ContainsFunc(written, func(k string) bool { return !left(k) }) {
		out = append(out, "openapi:contains")
	}
	return out
}

// lessNote returns diags less the report validationOnlyAt drew at at for the
// validation-only entry under key, which its caller keeps no entry of: another
// writer keeps the one writing, and reports it. An entry of another reason
// drew none.
func lessNote(diags []ir.Diagnostic, at ir.Provenance, key string) []ir.Diagnostic {
	label := strings.TrimPrefix(key, EntryKey("", ""))
	switch label {
	case "if-then-else":
		label = "if/then/else"
	default:
	}
	message := validationOnlyMessage(label)
	return slices.DeleteFunc(diags, func(diagnostic ir.Diagnostic) bool {
		return diagnostic.Code == diag.ValidationOnlyKeyword && diagnostic.Provenance.Pointer == at.Pointer && diagnostic.Message == message
	})
}

// readLocal reads what schema, written at pointer, says of the position alone:
// the annotations no use inherits. own says schema is the schema written at the
// position, rather than a folded branch, whose dialect keywords, discriminator
// and unevaluated keywords no fold reads and whose residue keeps them, dialect
// is what its dialect spells otherwise
// (SchemaPositionAnnotationSources.Dialect), and closure is
// SchemaPositionAnnotationSources.Closure.
func readLocal(schema *statement.Schema, pointer jsontext.Pointer, doc provenance.Doc, own bool, dialect Dialect, closure bool) (Set, []ir.Diagnostic) {
	var out Set
	out.XML = XMLHints(schema.XML())

	examples, exDiags := schemaExamplesAt(schema, pointer, doc)
	out.Examples = examples

	ext, extDiags := SchemaExtensions(extensionsIn(schema), doc, pointer, "")
	sub, subDiags := subObjectKeys(schema, out.XML, pointer, doc, own)
	kept, keptDiags := unmodeledAt(schema, pointer, doc, own, dialect, closure)

	diags := make([]ir.Diagnostic, 0, len(exDiags)+len(extDiags)+len(subDiags)+len(keptDiags))
	diags = append(diags, exDiags...)
	diags = append(diags, extDiags...)
	diags = append(diags, subDiags...)
	diags = append(diags, keptDiags...)

	out.Unmodeled = MergeUnmodeled(MergeUnmodeled(ext, sub), kept)
	return out, diags
}

// subObjectKeys collects what a schema's xml, discriminator and externalDocs
// declare that reaches no IR field: the x-* they carry and the keys the
// specification defines for none of them. None of those IR types holds an
// Unmodeled map, so the entries ride on the schema's node, keyed apart by the
// keyword each was written under. The census is graded as an OpenAPI object's,
// not a schema keyword's. It first reads the XML object's nodeType into hints
// (applyNodeType). discriminator says whether schema's discriminator is read.
// An object schema does not write is not asked, so a schema writing none costs
// nothing.
func subObjectKeys(schema *statement.Schema, hints *ir.XMLHints, pointer jsontext.Pointer, doc provenance.Doc, discriminator bool) (ir.Unmodeled, []ir.Diagnostic) {
	var out ir.Unmodeled
	var diags []ir.Diagnostic
	keep := func(keyword string, unknown []string, node *yaml.Node, ext []statement.Extension, taken []string) {
		owner := pointer + ids.Ptr(keyword)
		kept, extDiags := SchemaExtensions(ext, doc, owner, keyword)
		out = MergeUnmodeled(out, kept)
		diags = append(diags, extDiags...)
		diags = append(diags, UnknownKeysNamed(&out, unknown, node, doc, owner, keyword, taken...)...)
	}
	if xml := schema.XML(); xml != nil {
		taken, xmlDiags := applyNodeType(&out, hints, xml, doc, pointer+ids.Ptr("xml"))
		diags = append(diags, xmlDiags...)
		keep("xml", xml.Unknown, xml.Node, xml.Extensions, taken)
	}
	if discriminator {
		kept, keptDiags := DiscriminatorKeys(schema, pointer, doc)
		out = MergeUnmodeled(out, kept)
		diags = append(diags, keptDiags...)
	}
	if externalDocs := schema.ExternalDocs(); externalDocs != nil {
		keep("externalDocs", externalDocs.Unknown, externalDocs.Node, externalDocs.Extensions, nil)
	}
	return out, diags
}

// DiscriminatorKeys returns what the discriminator schema writes, at pointer,
// declares that reaches no IR field, as Read keeps a site's own schema's: the
// x-* it carries and the keys the specification does not define for it. A node
// holding the discriminator of a schema written elsewhere, an allOf entry whose
// union stands at the entry's position, keeps them so.
func DiscriminatorKeys(schema *statement.Schema, pointer jsontext.Pointer, doc provenance.Doc) (ir.Unmodeled, []ir.Diagnostic) {
	discriminator := schema.Discriminator()
	if discriminator == nil {
		return nil, nil
	}
	owner := pointer + ids.Ptr("discriminator")
	out, diags := SchemaExtensions(discriminator.Extensions, doc, owner, "discriminator")
	return out, append(diags, UnknownKeysNamed(&out, discriminator.Unknown, discriminator.Node, doc, owner, "discriminator")...)
}

// applyNodeType places the nodeType the XML object xml writes on hints, which
// the parser's model has no field for (GitHub #615), and returns the keys it
// read for the census to leave alone. It reads at every version, as load
// reports the field below 3.2 (diag.LaterVersionConstruct). A non-empty value
// outside the five the specification defines is kept verbatim on unmodeled
// instead, as nothing else may fill XMLHints.NodeType. An attribute flag the
// nodeType contradicts, which 3.2 forbids writing beside it, is kept verbatim
// on unmodeled rather than placed, so the hints never name two kinds of node.
func applyNodeType(unmodeled *ir.Unmodeled, hints *ir.XMLHints, xml *statement.XML, doc provenance.Doc, owner jsontext.Pointer) ([]string, []ir.Diagnostic) {
	nodeType, read, diags := TakeString(unmodeled, xml.Node, "nodeType", definedNodeType, doc, owner, "xml")
	if !read {
		return nil, diags
	}
	if nodeType == "" {
		return []string{"nodeType"}, diags
	}
	if xml.Attribute != nil && *xml.Attribute != (nodeType == "attribute") {
		hints.Attribute = nil
		at := doc.At(owner + ids.Ptr("attribute"))
		PreserveInto(unmodeled, "openapi:xml/attribute", ir.RawValue(strconv.FormatBool(*xml.Attribute)),
			ir.ReasonDegradedLowering, at)
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct, at,
			"attribute contradicts nodeType %q, which replaces it; nodeType is placed and attribute "+
				"kept verbatim under Unmodeled", nodeType))
	}
	hints.NodeType = nodeType
	return []string{"nodeType"}, diags
}

// definedNodeType reports whether v is a node type OpenAPI 3.2 defines, the
// domain ir.XMLHints.NodeType holds.
func definedNodeType(v string) bool {
	switch v {
	case "element", "attribute", "text", "cdata", "none":
		return true
	}
	return false
}

// unmodeledAt collects every keyword a site declares that the IR keeps verbatim
// instead of modelling, each under the reason saying which it is
// (docs/ir/unmodeled.md#reasons): validation logic the IR draws a boundary
// against (docs/ir/boundaries.md#validation-only-constructs), and JSON Schema
// resource/dialect metadata the IR excludes on purpose. own and closure are
// readLocal's.
//
// The content vocabulary is not read here even though it is data with an IR
// home: whether contentEncoding, contentMediaType and contentSchema reached
// ir.Encoding depends on what the position lowered to, which only the schema
// package can answer — schema.recordUnplacedContent asks the node that was
// built rather than the keyword that was written.
func unmodeledAt(schema *statement.Schema, pointer jsontext.Pointer, doc provenance.Doc, own bool, dialect Dialect,
	closure bool,
) (ir.Unmodeled, []ir.Diagnostic) {
	vOnly, diags := validationOnlyAt(schema, pointer, doc, dialect, own, closure)
	if !own {
		return vOnly, diags
	}
	kept, dDiags := dialectAt(schema, pointer, doc, dialect)
	return MergeUnmodeled(vOnly, kept), append(diags, dDiags...)
}

// dialectKeywords are the JSON Schema resource and dialect keywords the IR
// excludes on purpose. It identifies every type by a synthetic ID derived from
// its source pointer rather than by `$id` (docs/ir/identity.md), and describes
// one API surface rather than a JSON Schema resource graph, so it has no
// dialect axis for `$schema`/`$vocabulary` to land on and none is coming —
// ReasonOutOfScope rather than ReasonNoIRHome (docs/ir/unmodeled.md#reasons).
//
// `$id` is kept and read besides, where docs/compilers/openapi.md#references
// reads one, as the identifier and base URI a $ref resolves against, and
// `$schema` as the dialect a schema it roots is read in.
var dialectKeywords = [...]string{"$id", "$schema", "$vocabulary"}

// DialectKeywords yields the JSON Schema resource and dialect keywords the IR
// keeps out of scope (dialectKeywords). Each reader ranges over its own
// sequence, so none can change what another reads.
func DialectKeywords() iter.Seq[string] { return slices.Values(dialectKeywords[:]) }

// DialectKeyword reports whether keyword is one of DialectKeywords.
func DialectKeyword(keyword string) bool { return slices.Contains(dialectKeywords[:], keyword) }

// dialectRead is what keyword, one of DialectKeywords or draft 4's `id`, is
// read for beyond being kept, its schema's dialect reading its identifier as
// dialect says: an identifier for reference resolution alone, and for nothing
// where the dialect ignores it, or spells it `id` and keyword is `$id`;
// $schema for the dialect of a schema resource it roots
// (docs/compilers/openapi.md#dialect-keywords); and $vocabulary for nothing.
// The words hold at every site, as the reader cannot tell a root: a $schema
// rooting no resource, or in a 3.0 document, chooses nothing.
func dialectRead(keyword string, dialect Dialect) string {
	switch {
	case keyword == "$id" && dialect.Identifier == "id":
		return "is read for nothing, as this schema's dialect spells an identifier id"
	case (keyword == "$id" || keyword == "id") && dialect.IdentifierIgnored:
		return "is read for nothing, as this schema's dialect ignores the keywords beside a $ref"
	case keyword == "$id", keyword == "id":
		return "is read only for reference resolution"
	case keyword == "$schema":
		return "is read only where it roots a schema resource of a 3.1 or 3.2 document, to choose " +
			"that resource's dialect, never for reference resolution"
	default:
		return "is not honoured for reference resolution"
	}
}

// dialectAt keeps each dialect keyword schema declares verbatim and announces
// it, so the exclusion is visible in the output rather than only in this
// comment: those DialectKeywords names, and draft 4's `id` where dialect says
// schema is read in a dialect spelling its identifier so.
func dialectAt(schema *statement.Schema, pointer jsontext.Pointer, doc provenance.Doc, dialect Dialect) (ir.Unmodeled, []ir.Diagnostic) {
	var unmodeled ir.Unmodeled
	var diags []ir.Diagnostic
	if dialect.Identifier == "id" {
		diags = append(diags, KeepDialectKeyword(&unmodeled, schema, "id", pointer, doc, dialect)...)
	}
	for _, keyword := range dialectKeywords {
		diags = append(diags, KeepDialectKeyword(&unmodeled, schema, keyword, pointer, doc, dialect)...)
	}
	return unmodeled, diags
}

// KeepDialectKeyword keeps keyword, one of DialectKeywords or draft 4's `id`,
// verbatim on unmodeled where schema, the schema at pointer, writes it, out of
// scope, and announces it at the keyword, as dialectAt does for each one a site
// writes.
func KeepDialectKeyword(unmodeled *ir.Unmodeled, schema *statement.Schema, keyword string, pointer jsontext.Pointer,
	doc provenance.Doc, dialect Dialect,
) []ir.Diagnostic {
	kept, diags := PreserveSchemaKeyword(unmodeled, schema, keyword, ir.ReasonOutOfScope, pointer, doc)
	if !kept {
		return diags
	}
	return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, doc.At(pointer+ids.Ptr(keyword)),
		"%s identifies or configures a JSON Schema resource rather than describing data; "+
			"the IR models no such axis, so it is kept verbatim under Unmodeled and %s", keyword,
		dialectRead(keyword, dialect)))
}

// schemaExamplesAt reads a schema's example and examples keywords.
//
// Site-only: an example written beside a $ref describes the position, never the
// referent, which is the class of annotation the $ref-sibling defect broke.
func schemaExamplesAt(schema *statement.Schema, pointer jsontext.Pointer, doc provenance.Doc) ([]ir.Example, []ir.Diagnostic) {
	var out []ir.Example
	var diags []ir.Diagnostic
	add := func(node *yaml.Node, seg ...string) {
		var exDiags []ir.Diagnostic
		out, exDiags = AppendExample(out, ir.Example{}, node, doc, pointer, seg...)
		diags = append(diags, exDiags...)
	}
	notes := notesIn(schema)
	if notes == nil {
		return out, diags
	}
	if notes.Example != nil {
		add(notes.Example, "example")
	}
	for i, node := range notes.Examples {
		add(node, "examples", strconv.Itoa(i))
	}
	return out, diags
}

// AppendExample converts node into proto's value and appends the result to out.
// An unconvertible node is skipped and yields a warning diagnostic for the
// caller to record: an example is an annotation, not a structural hole, so
// losing it is fine as long as it is not silent. proto carries the annotations
// around the value (name, summary, description); base and seg locate the node,
// joined into a pointer only on the failure path.
func AppendExample(out []ir.Example, proto ir.Example, node *yaml.Node,
	doc provenance.Doc, base jsontext.Pointer, seg ...string,
) ([]ir.Example, []ir.Diagnostic) {
	irValue, err := value.FromNode(node)
	if err != nil {
		return out, []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.DegradedConstruct,
			doc.At(base+ids.Ptr(seg...)), "example: %s", err.Error())}
	}
	proto.Value = &irValue
	return append(out, proto), nil
}

// String renders k by name; an assertion failure or test diff over a bare
// Kind would otherwise print the underlying int (0 or 1).
func (k Kind) String() string {
	switch k {
	case Declaration:
		return "Declaration"
	case Reference:
		return "Reference"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// notesIn returns what schema says of itself, nil for none.
func notesIn(schema *statement.Schema) *statement.Notes {
	if schema == nil {
		return nil
	}
	return schema.Notes
}

// extensionsIn returns the extensions schema writes, nil for none.
func extensionsIn(schema *statement.Schema) []statement.Extension {
	if schema == nil {
		return nil
	}
	return schema.Extensions
}
