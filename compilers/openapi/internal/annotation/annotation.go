// Package annotation reads the documentation-adjacent facts a schema or a
// carrier declares — descriptions, deprecation, visibility, XML hints,
// extensions — and the validation-only JSON Schema keywords the IR keeps
// verbatim rather than models.
//
// It exists because those two jobs share one question: given a $ref, does the
// fact belong to the referenced declaration or to the site that references it?
// Site and Home answer it once, and every reader here routes through them, so a
// new annotation cannot quietly pick the other answer.
package annotation

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/extensions"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// RawFromNode converts a YAML node to the canonical JSON an Unmodeled entry
// holds.
//
// Its three outcomes are deliberately distinct, because collapsing the first two
// into one nil return is what let a diagnostic announce a preservation that never
// happened (GitHub #144): an absent node yields (nil, nil) — there was no
// construct here — while a node that cannot be represented yields an error.
//
// A node fails to convert when it names something JSON cannot: a mapping key
// that is not a string, a key written twice, .nan or .inf, or a scalar whose tag
// promises a type its text does not hold. The walk's own bounds refuse two
// shapes more — an alias that cycles, and one that expands past its node budget.
// A tag yaml.v3 assigns no type to is not a failure: its scalar keeps its text.
//
// The conversion walks the node tree rather than decoding it into `any` and
// re-marshalling, because that decode rounds every numeric literal through
// float64: it silently rewrote a 23-digit extension value and flattened
// 1.000000000000000000001 to 1, in the one channel whose whole promise is
// verbatim preservation (GitHub #32).
func RawFromNode(node *yaml.Node) (ir.RawValue, error) {
	if node == nil {
		return nil, nil
	}
	var conv rawConv
	data, err := conv.node(node, 0)
	if err != nil {
		return nil, fmt.Errorf("render yaml node as json: %w", err)
	}
	return data, nil
}

// EffectiveDeprecated reports the deprecated flag, use-site over referent.
func EffectiveDeprecated(ref, tgt *oas3.Schema) bool {
	return pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.Deprecated })
}

// EffectiveVisibility maps readOnly/writeOnly to a lifecycle visibility set
// (ir-design §5.2): readOnly is present in every response lifecycle
// (read/delete/query) and absent only from requests; writeOnly is create+update.
//
// A single schema declaring both flags keeps readOnly's set, since the switch
// below takes the first matching case. Spread across two allOf branches the
// same pairing instead intersects to Visibility{None: true}, the two sets
// being disjoint — so one schema and two branches answer differently for what
// a reader would call the same input. That divergence is tracked in #276 and
// deliberately not settled here.
func EffectiveVisibility(ref, tgt *oas3.Schema) ir.Visibility {
	switch {
	case pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.ReadOnly }):
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}
	case pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.WriteOnly }):
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}
	default:
		return ir.Visibility{}
	}
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
// field. A carrier therefore ends up with documentation the position itself
// need not have written — a bare `$ref` reads all three from the referent,
// which keeps its own copy on its node.
//
// Both halves are deliberate. The use-site half is the only home a keyword
// written *at* the position has once the body reduced to a shared node
// (GitHub #116): dropping it there loses it outright. The referent half is
// ir-design §14 — ref-target annotations merge onto the referencing
// Property/Parameter with use-site precedence, applied uniformly — and it is how
// every other field this compiler reads through a $ref already behaves
// (fillPropertyDefault, EffectiveVisibility, EffectiveDeprecated).
//
// Uniform is the load-bearing word: description alone inheriting, while title
// and externalDocs stop at the position, is the ad-hoc per-keyword patching §14
// names as the counterexample to avoid.
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

// ExtensionsFrom lowers an x-* extension map into namespaced ir.Unmodeled, keys
// prefixed "openapi:" and values serialized to raw JSON. owner is the pointer of
// the object the extensions were written on; each entry is located at its own
// key beneath it and marked ReasonVendorExtension, since the format assigns an
// x-* key no semantics at all.
func ExtensionsFrom(ext *extensions.Extensions, srcIndex int, owner string) (ir.Unmodeled, []ir.Diagnostic) {
	return ExtensionsUnder(ext, srcIndex, owner, "")
}

// ExtensionsUnder is ExtensionsFrom with every entry keyed beneath scope, for
// the objects whose extensions have no Unmodeled map of their own to land on.
//
// Most OpenAPI objects lower to an IR node that carries one, and those pass an
// empty scope. The rest ride on the nearest node that does — an info object's on
// the document, an encoding's on the content, a path item's on each of its
// operations — and several of them can reach the same map, where "openapi:x-id"
// from two objects is one key and the surviving entry would depend on which
// lowering ran last. scope names which object wrote them: the source path from
// the carrier down to it, or the object's own keyword where it is not beneath
// the carrier at all.
//
// A scoped key cannot collide with an unscoped one on the same map, since only
// an x-* key reaches here and no scope begins with "x-".
func ExtensionsUnder(ext *extensions.Extensions, srcIndex int, owner, scope string) (ir.Unmodeled, []ir.Diagnostic) {
	if ext == nil || ext.Len() == 0 {
		return nil, nil
	}
	prefix := "openapi:"
	if scope != "" {
		prefix += scope + "/"
	}
	out := ir.Unmodeled{}
	var diags []ir.Diagnostic
	for name, node := range ext.All() {
		raw, err := RawFromNode(node)
		if err != nil || raw == nil {
			diags = append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct,
				ir.Provenance{Source: srcIndex, Pointer: owner},
				"extension %q could not be serialized", name))
			continue
		}
		out[prefix+name] = ir.UnmodeledEntry{
			Reason:     ir.ReasonVendorExtension,
			Value:      raw,
			Provenance: ir.Provenance{Source: srcIndex, Pointer: owner + ids.Ptr(name)},
		}
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// ExtensionSite is one object's x-* map paired with where it was written: Owner
// is the object's own source pointer, and Scope is what its entries key under on
// the carrier that ends up holding them (see ExtensionsUnder).
type ExtensionSite struct {
	Scope string
	Owner string
	Ext   *extensions.Extensions
}

// ExtensionsAt folds every site into one Unmodeled map, for the carriers that
// hold more than one object's extensions. Sites are applied in the order given,
// which is source order at every caller; distinct scopes cannot collide, so the
// order decides nothing but is fixed anyway.
func ExtensionsAt(srcIndex int, sites ...ExtensionSite) (ir.Unmodeled, []ir.Diagnostic) {
	var out ir.Unmodeled
	var diags []ir.Diagnostic
	for _, site := range sites {
		ext, extDiags := ExtensionsUnder(site.Ext, srcIndex, site.Owner, site.Scope)
		out = MergeUnmodeled(out, ext)
		diags = append(diags, extDiags...)
	}
	return out, diags
}

// MergeUnmodeled overlays src onto dst, allocating dst on first write.
func MergeUnmodeled(dst, src ir.Unmodeled) ir.Unmodeled {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = ir.Unmodeled{}
	}
	maps.Copy(dst, src)
	return dst
}

// IsFalseSchema reports whether js is the boolean `false` schema.
func IsFalseSchema(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return js != nil && js.IsBool() && js.GetBool() != nil && !*js.GetBool()
}

// IfThenElseRaw combines the present if/then/else arms into one raw JSON
// object.
func IfThenElseRaw(s *oas3.Schema) (ir.RawValue, error) {
	members, err := presentMembers(s, "if", "then", "else")
	if err != nil {
		return nil, err
	}
	return jsonObject(members), nil
}

// ContainsRaw combines contains/minContains/maxContains into one raw JSON
// object.
func ContainsRaw(s *oas3.Schema) (ir.RawValue, error) {
	if s.GetContains() == nil && s.GetMinContains() == nil && s.GetMaxContains() == nil {
		return nil, nil
	}
	members, err := presentMembers(s, "contains", "minContains", "maxContains")
	if err != nil {
		return nil, err
	}
	return jsonObject(members), nil
}

// UnevaluatedRaw combines a non-false unevaluatedProperties and any
// unevaluatedItems into one raw JSON object (a false unevaluatedProperties is a
// structural mode, handled in fillAdditional).
func UnevaluatedRaw(s *oas3.Schema) (ir.RawValue, error) {
	var want []string
	if up := s.GetUnevaluatedProperties(); up != nil && !IsFalseSchema(up) {
		want = append(want, "unevaluatedProperties")
	}
	if s.GetUnevaluatedItems() != nil {
		want = append(want, "unevaluatedItems")
	}
	members, err := presentMembers(s, want...)
	if err != nil {
		return nil, err
	}
	return jsonObject(members), nil
}

// presentMembers collects the given keywords that are present on s as raw JSON
// members, preserving the requested order.
// A member that cannot be converted fails the whole combination rather than
// being skipped. The entry these build is one object presented as the verbatim
// source, so dropping a member from it would restate GitHub #144 in miniature:
// an object labelled verbatim that silently omits one of its keywords.
func presentMembers(s *oas3.Schema, keys ...string) ([]rawMember, error) {
	var members []rawMember
	for _, k := range keys {
		raw, err := RawFromNode(RawPropertyNode(s, k))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		if raw == nil {
			continue
		}
		members = append(members, rawMember{key: k, val: raw})
	}
	return members, nil
}

// RawPropertyNode returns the raw YAML value node of a top-level schema
// keyword, or nil when absent. The library's GetPropertyNode resolves Go core
// field names and returns key nodes; this scans the schema's root mapping for
// the on-wire keyword and returns its value node, which is where exact literals
// live.
func RawPropertyNode(s *oas3.Schema, key string) *yaml.Node {
	if s == nil {
		return nil
	}
	return RawChildNode(s.GetRootNode(), key)
}

// rawMember is one key/raw-JSON pair of a combined validation-only object.
type rawMember struct {
	key string
	val ir.RawValue
}

// jsonObject renders ordered raw members into a JSON object, or nil when empty.
func jsonObject(members []rawMember) ir.RawValue {
	if len(members) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(m.key)
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return ir.RawValue(b.String())
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
	// (fillPropertyDetail, fillParamSchema): a model property, a response or
	// part header, an operation parameter. Hoisting there would give one
	// declaration two homes.
	HomeCarrier
)

// Kind distinguishes a position that declares a type from one that
// references another type and may carry annotations of its own.
type Kind int

// Site is what a schema position declares: Node is the schema written there,
// and Referent — set only for a reference site — is the schema exactly one
// hop away, never the end of a $ref chain.
//
// The split lets an annotation be read from where it was written rather than
// wherever the $ref resolves to, with a fallback to Referent for annotations
// meant to inherit from the target. Only Node has a production reader: a
// declaration's annotations bind the position they are written at
// (attachDeclaredAnnotations), and a component that aliases another keeps the
// target's own annotations reachable through its Base rather than copying
// them. Kind and Referent are for a reader that does want to inherit.
// fillPropertyDetail (schema.go) instead falls back via refTargetSchema,
// which follows a $ref chain to its end (GetResolvedSchema) rather than one
// hop (GetReferenceResolutionInfo, what Referent uses). The two are not
// interchangeable: swapping one for the other would silently change
// property-default and description semantics on a ref-to-ref chain.
type Site struct {
	Kind Kind
	Node *oas3.Schema
	// Referent is nil when Kind is Reference but the $ref does not
	// resolve; refTypeRef is what diagnoses that, not this.
	Referent *oas3.Schema
}

// At builds the site for js. A $ref position resolves Referent exactly
// one hop through DeclaredSchema, never the full chain — see Site and
// DeclaredSchema for why that distinction matters.
//
// A schema whose $ref pointer is present but empty ({$ref: ""}) is not a
// reference site: an empty ref resolves nowhere, so IsReference is false and
// it is classified as a declaration like any other schema body. That is what
// keeps Referent's nil guarantee true: a reference site's $ref was genuinely
// attempted, so an unresolved target is the only reason Referent is nil.
//
// At trusts its caller that js is genuinely the schema at the position
// being modeled; nothing here can cross-check that from js alone.
func At(js *oas3.JSONSchema[oas3.Referenceable]) Site {
	s := Site{Kind: Declaration, Node: SchemaOf(js)}
	if js == nil || js.IsBool() {
		return s
	}
	if !js.IsReference() {
		return s
	}
	s.Kind = Reference
	if decl := DeclaredSchema(js); decl != nil {
		s.Referent = SchemaOf(decl)
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
// one hop at a time by DeclaredSchema).
func SchemaOf(js *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if js == nil || js.IsBool() {
		return nil
	}
	return js.GetSchema()
}

// Home names where the annotations a schema position declares are
// kept. It is what decides whether a position that lowered to a shared node
// hoists one of its own, so the two homes can never both hold the same
// declaration (GitHub #116).
type Home int

// DeclaredSchema returns the schema written at the position js references — one
// hop, not the end of the chain. GetResolvedSchema follows a reference to a
// reference all the way through, which is the wrong node to hoist at that
// position: a sub-schema spelled {$ref: Other, minimum: 7} would be read as
// Other, and the bound written beside the $ref would be gone before anything
// could record it.
func DeclaredSchema(js *oas3.JSONSchema[oas3.Referenceable]) *oas3.JSONSchema[oas3.Referenceable] {
	info := js.GetReferenceResolutionInfo()
	if info == nil {
		return nil
	}
	return info.Object
}

// RawChildNode returns the raw YAML value node of a mapping child keyed by the
// on-wire name, unwrapping a document node first; nil when absent. It reads exact
// literals the high-level model does not preserve (links, servers, content maps).
func RawChildNode(root *yaml.Node, key string) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}

// The readers below consume a Site the caller supplies rather than resolving
// one themselves. Obtaining a referent is reference resolution — walk work —
// and the two available resolutions are not interchangeable: Site.Referent is
// exactly one hop, while the property path follows a $ref chain to its end.
// Passing the wrong one silently changes default and description semantics on
// a ref-to-ref chain.

// Set is everything a site's annotations yield, before any of it is
// attached to a carrier.
//
// The readers produce a value; the caller decides what its carrier can hold. A
// Parameter has no XML field and a TypeCommon has no Required, so the shape of
// the write differs per carrier while the reading does not.
type Set struct {
	Docs       ir.Docs
	Deprecated bool
	XML        *ir.XMLHints
	Examples   []ir.Example
	Unmodeled  ir.Unmodeled
}

// Read reads every site-local annotation at st.
//
// This is the single call site the decomposition exists for. Not because it
// merges duplicate readers — the docs readers are genuinely distinct and stay
// distinct — but because the site-versus-referent choice is made here once
// instead of at each position that attaches annotations. Three attachment points
// previously made it separately and disagreed: one passed a referent, one passed
// nil because a declaration has none, and one passed nil because it never
// resolved the referent it had.
func Read(st Site, pointer string, srcIndex int) (Set, []ir.Diagnostic) {
	var out Set

	referent := st.Referent
	if st.Kind == Declaration {
		referent = nil // a declaration has no target to fall back to
	}

	FillCarrierDocs(&out.Docs, st.Node, referent)
	out.Deprecated = EffectiveDeprecated(st.Node, referent)
	out.XML = XMLHints(st.Node.GetXML())

	examples, exDiags := schemaExamplesAt(st.Node, pointer, srcIndex)
	out.Examples = examples

	ext, extDiags := ExtensionsFrom(st.Node.GetExtensions(), srcIndex, pointer)
	sub, subDiags := subObjectKeys(st.Node, pointer, srcIndex)
	kept, keptDiags := unmodeledAt(st.Node, pointer, srcIndex)

	diags := make([]ir.Diagnostic, 0, len(exDiags)+len(extDiags)+len(subDiags)+len(keptDiags))
	diags = append(diags, exDiags...)
	diags = append(diags, extDiags...)
	diags = append(diags, subDiags...)
	diags = append(diags, keptDiags...)

	out.Unmodeled = MergeUnmodeled(MergeUnmodeled(ext, sub), kept)
	return out, diags
}

// subObjectKeys collects what the sub-objects of a schema declare that reaches
// no IR field — the x-* they carry and the keys the specification defines for
// none of them — over its xml, its discriminator and its externalDocs.
//
// Each is an OpenAPI object with its own closed key set, and none of
// ir.XMLHints, ir.Discriminator or ir.Link holds an Unmodeled map, so the
// entries ride on the node the schema itself lowers to; the keyword each was
// written under is what keeps three objects' entries apart on that one map.
//
// The census is graded as an OpenAPI object's rather than as a schema keyword's,
// even though these hang off a schema: the JSON Schema rule that an unrecognized
// keyword is legal governs the schema itself, and these three are OpenAPI
// objects that the schema vocabulary says nothing about.
func subObjectKeys(s *oas3.Schema, pointer string, srcIndex int) (ir.Unmodeled, []ir.Diagnostic) {
	subs := []struct {
		keyword string
		obj     any
		ext     *extensions.Extensions
	}{
		{"xml", s.GetXML(), s.GetXML().GetExtensions()},
		{"discriminator", s.GetDiscriminator(), s.GetDiscriminator().GetExtensions()},
		{"externalDocs", s.GetExternalDocs(), s.GetExternalDocs().GetExtensions()},
	}
	var out ir.Unmodeled
	var diags []ir.Diagnostic
	for _, sub := range subs {
		owner := pointer + ids.Ptr(sub.keyword)
		ext, extDiags := ExtensionsUnder(sub.ext, srcIndex, owner, sub.keyword)
		out = MergeUnmodeled(out, ext)
		diags = append(diags, extDiags...)
		diags = append(diags, UnknownKeysUnder(&out, sub.obj, srcIndex, owner, sub.keyword)...)
	}
	return out, diags
}

// unmodeledAt collects every keyword a site declares that the IR keeps verbatim
// instead of modelling, each under the reason that says which of those it is
// (§12): validation logic the IR draws a boundary against (§4.7), data with no IR
// field yet, and JSON Schema resource/dialect metadata the IR excludes on
// purpose.
func unmodeledAt(s *oas3.Schema, pointer string, srcIndex int) (ir.Unmodeled, []ir.Diagnostic) {
	vOnly, vDiags := validationOnlyAt(s, pointer, srcIndex)
	noHome, nhDiags := noIRHomeAt(s, pointer, srcIndex)
	dialect, dDiags := dialectAt(s, pointer, srcIndex)

	diags := make([]ir.Diagnostic, 0, len(vDiags)+len(nhDiags)+len(dDiags))
	diags = append(diags, vDiags...)
	diags = append(diags, nhDiags...)
	diags = append(diags, dDiags...)

	return MergeUnmodeled(MergeUnmodeled(vOnly, noHome), dialect), diags
}

// noIRHomeAt collects the keywords a schema declares that describe real data yet
// have no field at any IR position. Unlike the §4.7 family these are gaps
// expected to close rather than a boundary the IR draws, which is what
// ReasonNoIRHome says and ReasonValidationOnly would not (§12).
//
// Site-only: contentSchema describes the value at the position that wrote it.
func noIRHomeAt(s *oas3.Schema, pointer string, srcIndex int) (ir.Unmodeled, []ir.Diagnostic) {
	if s.GetContentSchema() == nil {
		return nil, nil
	}
	at := pointer + ids.Ptr("contentSchema")
	var p ir.Unmodeled
	kept, diags := PreserveNodeInto(&p, "openapi:contentSchema", RawPropertyNode(s, "contentSchema"),
		ir.ReasonNoIRHome, at, srcIndex)
	if !kept {
		return nil, diags
	}
	return p, []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.DegradedConstruct,
		ir.Provenance{Source: srcIndex, Pointer: at},
		"contentSchema is the shape of the decoded content and no IR position has a field "+
			"for it; kept verbatim under Unmodeled")}
}

// DialectKeywords are the JSON Schema resource and dialect keywords the IR
// excludes on purpose. It identifies every type by a synthetic ID derived from
// its source pointer rather than by `$id` (ir-design §3), and describes one API
// surface rather than a JSON Schema resource graph, so it has no dialect axis for
// `$schema`/`$vocabulary` to land on and none is coming — ReasonOutOfScope rather
// than ReasonNoIRHome (§12).
//
// `$id` is kept, not honoured: reference resolution addresses same-document JSON
// pointers, never an `$id` base URI.
var DialectKeywords = []string{"$id", "$schema", "$vocabulary"}

// dialectAt keeps each dialect keyword s declares verbatim and announces it, so
// the exclusion is visible in the output rather than only in this comment.
func dialectAt(s *oas3.Schema, pointer string, srcIndex int) (ir.Unmodeled, []ir.Diagnostic) {
	var p ir.Unmodeled
	var diags []ir.Diagnostic
	for _, keyword := range DialectKeywords {
		at := pointer + ids.Ptr(keyword)
		kept, keptDiags := PreserveNodeInto(&p, "openapi:"+keyword, RawPropertyNode(s, keyword),
			ir.ReasonOutOfScope, at, srcIndex)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct,
			ir.Provenance{Source: srcIndex, Pointer: at},
			"%s identifies or configures a JSON Schema resource rather than describing data; "+
				"the IR models no such axis, so it is kept verbatim under Unmodeled and is not "+
				"honoured for reference resolution", keyword))
	}
	return p, diags
}

// schemaExamplesAt reads a schema's example and examples keywords.
//
// Site-only: an example written beside a $ref describes the position, never the
// referent, which is the class of annotation the $ref-sibling defect broke.
func schemaExamplesAt(s *oas3.Schema, pointer string, srcIndex int) ([]ir.Example, []ir.Diagnostic) {
	var out []ir.Example
	var diags []ir.Diagnostic
	if node := s.GetExample(); node != nil {
		out, diags = appendExampleAt(out, diags, node, srcIndex, pointer, "example")
	}
	for i, node := range s.GetExamples() {
		out, diags = appendExampleAt(out, diags, node, srcIndex, pointer, "examples", strconv.Itoa(i))
	}
	return out, diags
}

// appendExampleAt converts one example node, reporting an unconvertible value
// rather than dropping it.
func appendExampleAt(out []ir.Example, diags []ir.Diagnostic, node *yaml.Node,
	srcIndex int, base string, seg ...string,
) ([]ir.Example, []ir.Diagnostic) {
	at := base + ids.Ptr(seg...)
	v, err := value.FromNode(node)
	if err != nil {
		return out, append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct,
			ir.Provenance{Source: srcIndex, Pointer: at}, "example: %s", err.Error()))
	}
	return append(out, ir.Example{Value: &v}), diags
}

// validationOnlyAt collects the §4.7 keywords a schema declares that the IR does
// not model, keeping each verbatim and announcing it.
//
// Site-only: these constrain the value at the position that wrote them.
func validationOnlyAt(s *oas3.Schema, pointer string, srcIndex int) (ir.Unmodeled, []ir.Diagnostic) {
	var p ir.Unmodeled
	var diags []ir.Diagnostic

	// keep takes the conversion's error alongside its payload so a keyword that
	// could not be converted is reported rather than passed on as an absent one —
	// the two were indistinguishable here before GitHub #144.
	keep := func(key string, raw ir.RawValue, err error, entryPtr, label string) {
		if err != nil {
			diags = append(diags, UnpreservableDiag(key, entryPtr, srcIndex, err))
			return
		}
		diags = append(diags, PreserveKeywordInto(&p, key, raw, pointer, entryPtr, label, srcIndex)...)
	}
	// A keyword whose entry is its own node needs no label of its own: the
	// keyword names it. Only the §4.7 entries combining several keywords into one
	// object — if/then/else, contains, unevaluated — reach keep directly, because
	// no single keyword names those.
	keepKeyword := func(keyword string) {
		raw, err := RawFromNode(RawPropertyNode(s, keyword))
		keep("openapi:"+keyword, raw, err, pointer+ids.Ptr(keyword), keyword)
	}

	if s.GetNot() != nil {
		keepKeyword("not")
	}
	ite, iteErr := IfThenElseRaw(s)
	if ite != nil || iteErr != nil {
		keep("openapi:if-then-else", ite, iteErr, pointer, "if/then/else")
	}
	if ds := s.GetDependentSchemas(); ds != nil && ds.Len() > 0 {
		keepKeyword("dependentSchemas")
	}
	// dependentRequired is read off the raw node because oas3.Schema has no field
	// for it at v1.24.0 — the only reason it was silently dropped where its
	// sibling dependentSchemas was kept.
	dr, drErr := RawFromNode(RawPropertyNode(s, "dependentRequired"))
	if dr != nil || drErr != nil {
		keep("openapi:dependentRequired", dr, drErr, pointer+ids.Ptr("dependentRequired"), "dependentRequired")
	}
	if s.GetPropertyNames() != nil {
		keepKeyword("propertyNames")
	}
	craw, cErr := ContainsRaw(s)
	if craw != nil || cErr != nil {
		keep("openapi:contains", craw, cErr, pointer, "contains")
	}
	u, uErr := UnevaluatedRaw(s)
	if u != nil || uErr != nil {
		keep("openapi:unevaluated", u, uErr, pointer, "unevaluated")
	}
	return p, diags
}

// PreserveInto records raw under key in p, or records nothing when there are no
// bytes to record.
//
// len rather than a nil comparison: nil and a zero-length slice are distinct
// states, and an empty payload is the worse of the two. It preserves no
// construct, and json.Marshal rejects it for the whole document while naming
// json.RawMessage rather than the entry that carried it.
func PreserveInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	reason ir.UnmodeledReason, pointer string, srcIndex int,
) {
	if len(raw) == 0 {
		return
	}
	if *p == nil {
		*p = ir.Unmodeled{}
	}
	(*p)[key] = ir.UnmodeledEntry{
		Reason:     reason,
		Value:      raw,
		Provenance: ir.Provenance{Source: srcIndex, Pointer: pointer},
	}
}

// PreserveNodeInto converts node and records it under key, reporting a
// construct that reached the IR in no form at all rather than leaving its
// caller to announce a preservation that did not happen (GitHub #144).
//
// It reports whether an entry was written, which is what a caller with an
// announcement to make gates on. The three outcomes it distinguishes are the
// point: an absent node writes nothing and says nothing, because there was no
// construct; a converted one writes the entry; an unconvertible one writes
// nothing and yields the diagnostic that says so.
func PreserveNodeInto(p *ir.Unmodeled, key string, node *yaml.Node,
	reason ir.UnmodeledReason, pointer string, srcIndex int,
) (bool, []ir.Diagnostic) {
	raw, err := RawFromNode(node)
	if err != nil {
		return false, []ir.Diagnostic{UnpreservableDiag(key, pointer, srcIndex, err)}
	}
	if len(raw) == 0 {
		return false, nil
	}
	PreserveInto(p, key, raw, reason, pointer, srcIndex)
	return true, nil
}

// UnpreservableDiag reports a construct the compiler could neither model nor
// keep verbatim, so nothing of it reached the IR.
//
// Error rather than the degradation severity its callers otherwise use: a
// degraded lowering still describes the source in a weaker shape, while this one
// leaves no trace at all, which is a losslessness failure rather than a
// compromise (GitHub #144).
//
// The vendor-extension reader (ExtensionsFrom) reports the same conversion
// failure as a warning and is deliberately left alone: it already branches on the
// error and never claimed to have kept anything, so it is not the defect this
// code exists for.
func UnpreservableDiag(key, pointer string, srcIndex int, err error) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.UnpreservableConstruct,
		ir.Provenance{Source: srcIndex, Pointer: pointer},
		"%s could not be kept verbatim under Unmodeled and is represented in the IR "+
			"in no form at all: %s", key, err.Error())
}

// PreserveKeywordInto records a validation-only keyword and returns the one
// diagnostic announcing it. An absent payload records nothing and announces
// nothing; an unconvertible one never reaches here, because its caller reports it
// through UnpreservableDiag first.
func PreserveKeywordInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	declPtr, entryPtr, label string, srcIndex int,
) []ir.Diagnostic {
	if len(raw) == 0 {
		return nil
	}
	PreserveInto(p, key, raw, ir.ReasonValidationOnly, entryPtr, srcIndex)
	return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.ValidationOnlyKeyword,
		ir.Provenance{Source: srcIndex, Pointer: declPtr},
		"validation-only keyword %q kept verbatim under Unmodeled", label)}
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

// DeclaresAny reports whether s writes any of keywords. It reads the raw nodes
// rather than the model fields for the reason declaresValueConstraints does: the
// recorders these gate (recordResidue, dialectAt) read them there, and a
// predicate consulting a different source could disagree with them in either
// direction.
func DeclaresAny(s *oas3.Schema, keywords []string) bool {
	for _, keyword := range keywords {
		if RawPropertyNode(s, keyword) != nil {
			return true
		}
	}
	return false
}
