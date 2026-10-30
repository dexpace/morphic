// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"fmt"
	"strconv"

	"github.com/speakeasy-api/openapi/extensions"
	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// Locator returns the provenance to record for the position at a pointer: the
// pointer, and the index of the input document that supplied what sits there.
//
// The index belongs to the position, not to the compile: an overlay supplies
// some positions of the document it patches (GitHub #522). The compiler passes
// lowering.Ctx.ProvenanceAt, which knows what the overlay introduced; this
// package sits below that context and cannot ask it directly.
//
// from is for a record no single position addresses, and names the positions it
// was assembled from (see lowering.Ctx.ProvenanceAt).
type Locator func(pointer jsontext.Pointer, from ...jsontext.Pointer) ir.Provenance

// EffectiveDeprecated reports the deprecated flag, use-site over referent.
func EffectiveDeprecated(ref, tgt *oas3.Schema) bool {
	return pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.Deprecated })
}

// EffectiveVisibility maps readOnly/writeOnly to a lifecycle visibility set
// (docs/ir/properties.md#visibility): readOnly is present in every response
// lifecycle and absent only from requests; writeOnly is create+update. The bool
// reports both flags in force, which no lifecycle satisfies. It is not a
// diagnostic because this reader has no provenance of its own.
//
// Each flag resolves on its own, use-site over referent (the uniform merge of
// docs/compilers/openapi.md#ref-use-site).
//
// Both in force is contradictory but legal: JSON Schema forbids neither beside
// the other. Read as sets they leave nothing, which Visibility{None: true}
// states, as merge.mergeVisibility answers for the pair spread over two allOf
// branches (GitHub #276).
func EffectiveVisibility(ref, tgt *oas3.Schema) (ir.Visibility, bool) {
	readOnly := pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.ReadOnly })
	writeOnly := pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.WriteOnly })

	switch {
	case readOnly && writeOnly:
		return ir.Visibility{None: true}, true
	case readOnly:
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}, false
	case writeOnly:
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, false
	default:
		return ir.Visibility{}, false
	}
}

// EffectiveDefault returns the default ref, the schema at pointer, writes, or
// else the one its referent tgt writes, for the property or parameter ref
// describes to keep. A schema writing none yields nothing; an unconvertible
// node yields a warning at pointer and no value, rather than a warning at the
// default's own position as docs/compilers/openapi.md#values asks (GitHub
// #1101).
func EffectiveDefault(ref, tgt *oas3.Schema, pointer jsontext.Pointer, locate Locator) (*ir.Value, []ir.Diagnostic) {
	node := ref.GetDefault()
	if node == nil && tgt != nil {
		node = tgt.GetDefault()
	}
	if node == nil {
		return nil, nil
	}
	v, err := value.FromNode(node)
	if err != nil {
		return nil, []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.DegradedConstruct, locate(pointer),
			"default: %s", err.Error())}
	}
	return &v, nil
}

// pickFlag returns the bool field extracted by accessor from ref when present,
// else from tgt, else false. It is the single nil-safe "use-site overrides
// referent" primitive for boolean schema flags (readOnly, writeOnly, deprecated).
func pickFlag(ref, tgt *oas3.Schema, accessor func(*oas3.Schema) *bool) bool {
	if ref != nil {
		if v := accessor(ref); v != nil {
			return *v
		}
	}
	if tgt != nil {
		if v := accessor(tgt); v != nil {
			return *v
		}
	}
	return false
}

// FillTypeDocs maps a schema's title, description, and externalDocs onto Docs.
// Every field is assigned, never accumulated: a schema declares at most one
// externalDocs, and a pointer read twice (a sub-schema lowered both in place
// and through a $ref that hoists it) must not end up with two copies of it.
func FillTypeDocs(d *ir.Docs, s *oas3.Schema) {
	if t := s.GetTitle(); t != "" {
		d.Summary = t
	}
	if desc := s.GetDescription(); desc != "" {
		d.Description = desc
	}
	if ed := s.GetExternalDocs(); ed != nil {
		d.ExternalDocs = []ir.Link{{URL: ed.GetURL(), Description: ed.GetDescription()}}
	}
}

// FillCarrierDocs fills the ir.Property or ir.Parameter carrying a position
// with the documentation effective there: the $ref referent's title,
// description and externalDocs first, then the use-site's over them, field by
// field. A bare `$ref` therefore reads all three from the referent.
//
// Both halves are deliberate. The use-site half is the only home a keyword
// written at the position has once the body reduced to a shared node (GitHub
// #116). The referent half is the rule docs/compilers/openapi.md#ref-use-site:
// ref-target annotations merge onto the referencing Property/Parameter with
// use-site precedence, uniformly, constraints excepted, while the referent
// keeps its own copy on its node.
func FillCarrierDocs(d *ir.Docs, ref, tgt *oas3.Schema) {
	if tgt != nil {
		FillTypeDocs(d, tgt)
	}
	if ref != nil {
		FillTypeDocs(d, ref)
	}
}

// XMLHints maps an OpenAPI XML object onto ir.XMLHints; an attribute flag
// becomes the "attribute" node type. Fields are read directly rather than via
// the library getters, which dereference unset (nil) field pointers.
func XMLHints(x *oas3.XML) *ir.XMLHints {
	if x == nil {
		return nil
	}
	h := &ir.XMLHints{}
	if x.Name != nil {
		h.Name = *x.Name
	}
	if x.Namespace != nil {
		h.Namespace = *x.Namespace
	}
	if x.Prefix != nil {
		h.Prefix = *x.Prefix
	}
	if x.Wrapped != nil {
		h.Wrapped = *x.Wrapped
	}
	if x.Attribute != nil && *x.Attribute {
		h.NodeType = "attribute"
	}
	return h
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

// Site is what a schema position declares: Node is the schema written there,
// and Referent — set only for a reference site — is the schema the $ref's
// chain ends on, the referent a use site's annotations fall back to.
//
// A Site At builds carries no Referent: a declaration's annotations bind the
// position they are written at (attachDeclaredAnnotations), and a component
// that aliases another keeps the target's annotations reachable through its
// Base. A reader that inherits, FillPropertyDetail (schema/property.go) or
// fillParamSchema, builds its own Site with Referent from
// resolve.Scope.TargetSchema.
type Site struct {
	Kind Kind
	Node *oas3.Schema
	// Referent is nil when Kind is Reference but the $ref's chain ends on no
	// schema; refTypeRef is what diagnoses that, not this.
	Referent *oas3.Schema
}

// At builds the site for js, a reference site's without its Referent (see
// Site).
//
// A schema whose $ref is present but empty ({$ref: ""}) is a declaration, not a
// reference site: an empty ref resolves nowhere, so IsReference is false.
//
// At trusts its caller that js is the schema at the position being modeled.
func At(js *oas3.JSONSchema[oas3.Referenceable]) Site {
	s := Site{Kind: Declaration, Node: SchemaOf(js)}
	if js != nil && !js.IsBool() && js.IsReference() {
		s.Kind = Reference
	}
	return s
}

// SchemaOf returns the schema body written at this position, including one
// that also carries a $ref — an example or bound written beside a $ref binds
// the position, not the referent, the same rule fillPropertyAnnotations and
// fillPropertyConstraints apply at a property. It returns nil only where no
// body is written: a nil either, or a boolean schema, which admits no
// annotations.
//
// Every position modeled as a site reads it through At: a named component
// (lowerComponentSchema) and a $ref'd internal sub-schema (hoistSubSchema, fed
// one hop at a time by the schema bindings).
func SchemaOf(js *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if js == nil || js.IsBool() {
		return nil
	}
	return js.GetSchema()
}

// IsFalseSchema reports whether js is the boolean `false` schema, which a
// `null` written in its place is not, though the parser decodes one as `false`
// (docs/parser-library.md#null-schema-is-false).
func IsFalseSchema(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return js != nil && js.IsBool() && js.GetBool() != nil && !*js.GetBool() && !IsNullLiteral(js)
}

// IsNullLiteral reports whether js is a `null` written where a schema belongs.
// The parser reads one as the boolean `false` schema
// (docs/parser-library.md#null-schema-is-false), so only its node tells the
// two apart.
func IsNullLiteral(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return js != nil && js.IsBool() && IsNullNode(js.GetCore().GetRootNode())
}

// Written reports whether js is a schema written at its position: present,
// and no `null`, which states nothing about the values there (IsNullLiteral).
// The load phase reports the null; every reader takes it as left out
// (docs/compilers/openapi.md#null-schema).
func Written(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return js != nil && !IsNullLiteral(js)
}

// Home names where the annotations a schema position declares are
// kept. It is what decides whether a position that lowered to a shared node
// hoists one of its own, so the two homes can never both hold the same
// declaration (GitHub #116).
type Home int

// The readers below consume a Site the caller supplies rather than resolving
// one themselves, since obtaining a referent is reference resolution — walk
// work. Site.Referent is the end of the $ref's chain, so a schema between it
// and the use site lends nothing (GitHub #1029).

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
	// (DeclaresRedaction). A type node holds it as Sensitive and a property as
	// Secret; a parameter has no field for it (GitHub #578).
	Sensitive bool
	XML       *ir.XMLHints
	Examples  []ir.Example
	Unmodeled ir.Unmodeled
}

// redactionFormat is the one format OpenAPI defines that names no shape: its
// format table gives it as a hint to obscure the value.
const redactionFormat = "password"

// DeclaresRedaction reports whether s asks for its value to be redacted, by
// writing `format: password`. It is honoured whatever type s declares: the
// table lists it for strings, but hiding a value no one needed hidden is the
// safe reading. It is an annotation rather than a (type, format) pairing, so
// the shape a position lowers to is read without it.
func DeclaresRedaction(s *oas3.Schema) bool {
	return s.GetFormat() == redactionFormat
}

// Read reads every site-local annotation at st.
//
// This is the one place the site-versus-referent choice is made, rather than
// at each position that attaches annotations: three attachment points used to
// make it separately and disagreed.
func Read(st Site, pointer jsontext.Pointer, locate Locator) (Set, []ir.Diagnostic) {
	var out Set

	referent := st.Referent
	if st.Kind == Declaration {
		referent = nil // a declaration has no target to fall back to
	}

	FillCarrierDocs(&out.Docs, st.Node, referent)
	out.Deprecated = EffectiveDeprecated(st.Node, referent)
	out.Sensitive = DeclaresRedaction(st.Node) || DeclaresRedaction(referent)
	out.XML = XMLHints(st.Node.GetXML())

	examples, exDiags := schemaExamplesAt(st.Node, pointer, locate)
	out.Examples = examples

	ext, extDiags := ExtensionsFrom(st.Node.GetExtensions(), locate, pointer)
	sub, subDiags := subObjectKeys(st.Node, out.XML, pointer, locate)
	kept, keptDiags := unmodeledAt(st.Node, pointer, locate)

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
// (applyNodeType).
func subObjectKeys(s *oas3.Schema, hints *ir.XMLHints, pointer jsontext.Pointer, locate Locator) (ir.Unmodeled, []ir.Diagnostic) {
	var out ir.Unmodeled
	xmlTaken, diags := applyNodeType(&out, hints, s.GetXML(), locate, pointer+ids.Ptr("xml"))
	subs := []struct {
		keyword string
		obj     any
		ext     *extensions.Extensions
		taken   []string
	}{
		{"xml", s.GetXML(), s.GetXML().GetExtensions(), xmlTaken},
		{"discriminator", s.GetDiscriminator(), s.GetDiscriminator().GetExtensions(), nil},
		{"externalDocs", s.GetExternalDocs(), s.GetExternalDocs().GetExtensions(), nil},
	}
	for _, sub := range subs {
		owner := pointer + ids.Ptr(sub.keyword)
		ext, extDiags := ExtensionsUnder(sub.ext, locate, owner, sub.keyword)
		out = MergeUnmodeled(out, ext)
		diags = append(diags, extDiags...)
		diags = append(diags, UnknownKeysUnder(&out, sub.obj, locate, owner, sub.keyword, sub.taken...)...)
	}
	return out, diags
}

// applyNodeType places the nodeType an XML object writes on hints, which the
// parser's model has no field for (GitHub #615), and returns the keys it read
// for the census to leave alone. It reads at every version, as load reports the
// field below 3.2 (diag.LaterVersionConstruct). A non-empty value outside the
// five the specification defines is kept verbatim on p instead, as nothing else
// may fill XMLHints.NodeType. An attribute flag the nodeType contradicts, which
// 3.2 forbids writing beside it, is kept verbatim on p rather than overwritten
// in silence.
func applyNodeType(p *ir.Unmodeled, hints *ir.XMLHints, x *oas3.XML, locate Locator, owner jsontext.Pointer) ([]string, []ir.Diagnostic) {
	if x == nil {
		return nil, nil
	}
	nodeType, read, diags := TakeString(p, x.GetRootNode(), "nodeType", definedNodeType, locate, owner, "xml")
	if !read {
		return nil, diags
	}
	if nodeType == "" {
		return []string{"nodeType"}, diags
	}
	if x.Attribute != nil && *x.Attribute != (nodeType == "attribute") {
		at := locate(owner + ids.Ptr("attribute"))
		PreserveInto(p, "openapi:xml/attribute", ir.RawValue(strconv.FormatBool(*x.Attribute)),
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
// instead of modelling, each under the reason that says which of those it is
// (docs/ir/unmodeled.md#reasons): validation logic the IR draws a boundary
// against (docs/ir/boundaries.md#validation-only-constructs), and JSON Schema
// resource/dialect metadata the IR excludes on purpose.
//
// The content vocabulary is not read here even though it is data with an IR
// home: whether contentEncoding, contentMediaType and contentSchema reached
// ir.Encoding depends on what the position lowered to, which only the schema
// package can answer — schema.recordUnplacedContent asks the node that was
// built rather than the keyword that was written.
func unmodeledAt(s *oas3.Schema, pointer jsontext.Pointer, locate Locator) (ir.Unmodeled, []ir.Diagnostic) {
	vOnly, vDiags := validationOnlyAt(s, pointer, locate)
	dialect, dDiags := dialectAt(s, pointer, locate)

	diags := make([]ir.Diagnostic, 0, len(vDiags)+len(dDiags))
	diags = append(diags, vDiags...)
	diags = append(diags, dDiags...)

	return MergeUnmodeled(vOnly, dialect), diags
}

// DialectKeywords are the JSON Schema resource and dialect keywords the IR
// excludes on purpose. It identifies every type by a synthetic ID derived from
// its source pointer rather than by `$id` (docs/ir/identity.md), and describes
// one API surface rather than a JSON Schema resource graph, so it has no
// dialect axis for `$schema`/`$vocabulary` to land on and none is coming —
// ReasonOutOfScope rather than ReasonNoIRHome (docs/ir/unmodeled.md#reasons).
//
// `$id` is kept and read besides, where docs/compilers/openapi.md#references
// reads one, as the identifier and base URI a $ref resolves against.
var DialectKeywords = []string{"$id", "$schema", "$vocabulary"}

// dialectRead is what keyword, one of DialectKeywords, is read for beyond
// being kept: $id for reference resolution alone, any other for nothing.
func dialectRead(keyword string) string {
	if keyword == "$id" {
		return "is read only for reference resolution"
	}
	return "is not honoured for reference resolution"
}

// dialectAt keeps each dialect keyword s declares verbatim and announces it, so
// the exclusion is visible in the output rather than only in this comment.
func dialectAt(s *oas3.Schema, pointer jsontext.Pointer, locate Locator) (ir.Unmodeled, []ir.Diagnostic) {
	var p ir.Unmodeled
	var diags []ir.Diagnostic
	for _, keyword := range DialectKeywords {
		kept, keptDiags := PreserveSchemaKeyword(&p, s, keyword, ir.ReasonOutOfScope, pointer, locate)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		at := locate(pointer + ids.Ptr(keyword))
		diags = append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
			"%s identifies or configures a JSON Schema resource rather than describing data; "+
				"the IR models no such axis, so it is kept verbatim under Unmodeled and %s", keyword,
			dialectRead(keyword)))
	}
	return p, diags
}

// schemaExamplesAt reads a schema's example and examples keywords.
//
// Site-only: an example written beside a $ref describes the position, never the
// referent, which is the class of annotation the $ref-sibling defect broke.
func schemaExamplesAt(s *oas3.Schema, pointer jsontext.Pointer, locate Locator) ([]ir.Example, []ir.Diagnostic) {
	var out []ir.Example
	var diags []ir.Diagnostic
	add := func(node *yaml.Node, seg ...string) {
		var exDiags []ir.Diagnostic
		out, exDiags = AppendExample(out, ir.Example{}, node, locate, pointer, seg...)
		diags = append(diags, exDiags...)
	}
	if node := s.GetExample(); node != nil {
		add(node, "example")
	}
	for i, node := range s.GetExamples() {
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
	locate Locator, base jsontext.Pointer, seg ...string,
) ([]ir.Example, []ir.Diagnostic) {
	v, err := value.FromNode(node)
	if err != nil {
		return out, []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.DegradedConstruct,
			locate(base+ids.Ptr(seg...)), "example: %s", err.Error())}
	}
	proto.Value = &v
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
