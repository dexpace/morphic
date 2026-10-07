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
	"encoding/json/jsontext"
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

// RawFromNode converts a YAML node to the canonical JSON an Unmodeled entry
// holds. An absent node yields (nil, nil): no construct was there. One that
// cannot be represented yields an error, so no caller announces a preservation
// that never happened (GitHub #144).
//
// Conversion fails on a non-string or repeated mapping key, .nan or .inf, a
// scalar whose text does not satisfy its tag, invalid UTF-8, and an alias that
// cycles or expands past the node budget. A tag yaml.v3 leaves untyped is kept.
//
// It walks the node tree, not a decode into `any`, which rounds numeric
// literals through float64 (GitHub #32).
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
// (ir-design §5.2): readOnly is present in every response lifecycle and absent
// only from requests; writeOnly is create+update. The bool reports both flags
// in force, which no lifecycle satisfies. It is not a diagnostic because this
// reader has no provenance of its own.
//
// Each flag resolves on its own, use-site over referent (the uniform §14
// merge).
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
// #116). The referent half is ir-design §14: ref-target annotations merge onto
// the referencing Property/Parameter with use-site precedence, uniformly,
// constraints excepted, while the referent keeps its own copy on its node.
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
func ExtensionsFrom(ext *extensions.Extensions, locate Locator, owner jsontext.Pointer) (ir.Unmodeled, []ir.Diagnostic) {
	return ExtensionsUnder(ext, locate, owner, "")
}

// ExtensionsUnder is ExtensionsFrom with every entry keyed beneath scope, for
// objects whose extensions have no Unmodeled map of their own.
//
// Those objects ride on the nearest node that has one, where "openapi:x-id"
// from two objects would be one key, so scope names the writer: the source path
// from the carrier down to it, or its own keyword where it is not beneath the
// carrier.
//
// Keys cannot collide: a scope segment never begins with "x-" and an extension
// name always does, so the first "x-" segment ends the scope. The same gap
// separates them from non-extension keys under these scopes.
func ExtensionsUnder(ext *extensions.Extensions, locate Locator, owner jsontext.Pointer, scope string) (ir.Unmodeled, []ir.Diagnostic) {
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
				locate(owner), "extension %q could not be serialized", name))
			continue
		}
		out[prefix+name] = ir.UnmodeledEntry{
			Reason:     ir.ReasonVendorExtension,
			Value:      raw,
			Provenance: locate(owner + ids.Ptr(name)),
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
	Owner jsontext.Pointer
	Ext   *extensions.Extensions
}

// ExtensionsAt folds every site into one Unmodeled map, for the carriers that
// hold more than one object's extensions. Sites are applied in the order given,
// which is source order at every caller; distinct scopes cannot collide, so the
// order decides nothing but is fixed anyway.
func ExtensionsAt(locate Locator, sites ...ExtensionSite) (ir.Unmodeled, []ir.Diagnostic) {
	var out ir.Unmodeled
	var diags []ir.Diagnostic
	for _, site := range sites {
		ext, extDiags := ExtensionsUnder(site.Ext, locate, site.Owner, site.Scope)
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
// object, and names the arms it combines.
func IfThenElseRaw(s *oas3.Schema) (ir.RawValue, []string, error) {
	return combine(s, "if", "then", "else")
}

// ContainsRaw combines contains/minContains/maxContains into one raw JSON
// object, and names the keywords it combines.
func ContainsRaw(s *oas3.Schema) (ir.RawValue, []string, error) {
	if s.GetContains() == nil && s.GetMinContains() == nil && s.GetMaxContains() == nil {
		return nil, nil, nil
	}
	return combine(s, "contains", "minContains", "maxContains")
}

// UnevaluatedRaw combines a non-false unevaluatedProperties and any
// unevaluatedItems into one raw JSON object (a false unevaluatedProperties is a
// structural mode, handled in fillAdditional), and names the keywords it
// combines.
func UnevaluatedRaw(s *oas3.Schema) (ir.RawValue, []string, error) {
	var want []string
	if up := s.GetUnevaluatedProperties(); up != nil && !IsFalseSchema(up) {
		want = append(want, "unevaluatedProperties")
	}
	if s.GetUnevaluatedItems() != nil {
		want = append(want, "unevaluatedItems")
	}
	return combine(s, want...)
}

// combine renders the keys s writes, of those given, as one raw JSON object,
// and names the keys written; nothing written yields no object rather than an
// empty one. The entry built from it has no position of its own, so those keys
// are what its provenance is asked about (GitHub #534). They are named beside a
// conversion error as well, because the report that stands in for an entry that
// could not be kept is attributed as the entry would have been.
func combine(s *oas3.Schema, keys ...string) (ir.RawValue, []string, error) {
	members, written, err := presentMembers(s, keys...)
	if err != nil || len(members) == 0 {
		return nil, written, err
	}
	raw, err := jsonObject(members)
	return raw, written, err
}

// presentMembers collects the given keywords that are present on s as raw JSON
// members, preserving the requested order, and names the keywords present.
//
// A member that cannot be converted fails the whole combination rather than
// being skipped. The entry these build is one object presented as the verbatim
// source, so dropping a member from it would restate GitHub #144 in miniature:
// an object labelled verbatim that silently omits one of its keywords. The
// error names the first such member, and the keywords present are returned in
// full even then, since the report of the failure is attributed by all of them.
func presentMembers(s *oas3.Schema, keys ...string) ([]rawMember, []string, error) {
	var members []rawMember
	var present []string
	var failed error
	for _, k := range keys {
		node := RawPropertyNode(s, k)
		if node == nil {
			continue
		}
		present = append(present, k)
		raw, err := RawFromNode(node)
		switch {
		case err == nil:
			members = append(members, rawMember{key: k, val: raw})
		case failed == nil:
			failed = fmt.Errorf("%s: %w", k, err)
		}
	}
	if failed != nil {
		return nil, present, failed
	}
	return members, present, nil
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

// jsonObject renders ordered raw members into a JSON object; combine is what
// keeps an empty set from reaching it. Unlike rawConv.mapping, member order here
// is the caller's — a fixed handful of JSON Schema keywords in keyword order —
// and is preserved rather than sorted.
func jsonObject(members []rawMember) (ir.RawValue, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := jsontext.AppendQuote(nil, m.key)
		if err != nil {
			return nil, fmt.Errorf("member %q: %w", m.key, err)
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return ir.RawValue(b.String()), nil
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
// and Referent — set only for a reference site — is the schema exactly one
// hop away, never the end of a $ref chain.
//
// Production reads only Node from a Site At builds: a declaration's annotations
// bind the position they are written at (attachDeclaredAnnotations), and a
// component that aliases another keeps the target's annotations reachable
// through its Base. A reader that inherits, FillPropertyDetail (schema.go) or
// fillParamSchema, builds its own Site with Referent from resolve.TargetSchema,
// which follows the chain to its end; the two differ on a ref-to-ref chain.
type Site struct {
	Kind Kind
	Node *oas3.Schema
	// Referent is nil when Kind is Reference but the $ref does not
	// resolve; refTypeRef is what diagnoses that, not this.
	Referent *oas3.Schema
}

// At builds the site for js. A $ref position resolves Referent exactly one hop
// through DeclaredSchema, never the full chain (see Site).
//
// A schema whose $ref is present but empty ({$ref: ""}) is a declaration, not a
// reference site: an empty ref resolves nowhere, so IsReference is false. That
// keeps Referent's nil guarantee: a reference site's $ref was genuinely
// attempted, so an unresolved target is the only reason Referent is nil.
//
// At trusts its caller that js is the schema at the position being modeled.
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
// on-wire name, unwrapping a document node first; nil when absent. It reads
// exact literals the high-level model does not preserve (links, servers,
// content maps).
//
// The last pair spelling the key wins, as for the parser, which skips every
// earlier occurrence. An explicit pair and an aliased one are one key to the
// parser and two nodes here, so the first would describe a mapping by a value
// nothing else in the compiler uses.
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
	var found *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if keyName(root.Content[i]) == key {
			found = root.Content[i+1]
		}
	}
	return found
}

// RawMappingKeys returns the on-wire names a raw mapping writes, each once, in
// first-written order, unwrapping a document node first; nil for a node that is
// not a mapping or writes no key. Each name resolves through RawChildNode.
//
// It exists for a Path Item Object, whose unmarshaller skips a key with an
// anchored value (GitHub #412). Anchors are cleared first (GitHub #459, #501,
// #538), so it is needed only past #538's hop bound.
//
// A `<<` merge key is left out and its merged pairs are not read (GitHub #395).
// A repeated key is returned once, so a census does not report a collision the
// document lacks.
func RawMappingKeys(root *yaml.Node) []string {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var keys []string
	seen := make(map[string]bool, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if isMergeKey(k) {
			continue
		}
		name := keyName(k)
		if seen[name] {
			continue
		}
		seen[name] = true
		keys = append(keys, name)
	}
	return keys
}

// keyName is the on-wire name a mapping key node spells, following an alias to
// the scalar it stands for.
//
// yaml.v3 leaves an alias node's own Value as the anchor name, so a key written
// as an alias matches nothing when read raw — while the parser reads that key
// under the name it resolves to, which is the name every caller here looks up.
// Comparing the two spellings is what let a key the model reported as
// undeclared reach no Unmodeled entry at all (GitHub #297).
func keyName(n *yaml.Node) string {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias.Value
	}
	return n.Value
}

// RawChildNodes is RawChildNode for every key of a mapping at once: each name
// the mapping writes, to the node RawChildNode returns for it. A caller looking
// up many keys of one large mapping reads its pairs once rather than once per
// key. It is nil for a missing node and for one that is not a mapping.
func RawChildNodes(root *yaml.Node) map[string]*yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	children := make(map[string]*yaml.Node, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		children[keyName(root.Content[i])] = root.Content[i+1]
	}
	return children
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

// Read reads every site-local annotation at st. reads32 reports that the
// document speaks OpenAPI 3.2, whose XML object adds a `nodeType` the parser's
// model has no field for, read here off the raw node (GitHub #615). It is a
// parameter because this package may not import the lowering or the loader.
//
// This is the one place the site-versus-referent choice is made, rather than
// at each position that attaches annotations: three attachment points used to
// make it separately and disagreed.
func Read(st Site, pointer jsontext.Pointer, locate Locator, reads32 bool) (Set, []ir.Diagnostic) {
	var out Set

	referent := st.Referent
	if st.Kind == Declaration {
		referent = nil // a declaration has no target to fall back to
	}

	FillCarrierDocs(&out.Docs, st.Node, referent)
	out.Deprecated = EffectiveDeprecated(st.Node, referent)
	out.XML = XMLHints(st.Node.GetXML())
	if reads32 {
		applyNodeType(out.XML, st.Node)
	}

	examples, exDiags := schemaExamplesAt(st.Node, pointer, locate)
	out.Examples = examples

	ext, extDiags := ExtensionsFrom(st.Node.GetExtensions(), locate, pointer)
	sub, subDiags := subObjectKeys(st.Node, pointer, locate, reads32)
	kept, keptDiags := unmodeledAt(st.Node, pointer, locate)

	diags := make([]ir.Diagnostic, 0, len(exDiags)+len(extDiags)+len(subDiags)+len(keptDiags))
	diags = append(diags, exDiags...)
	diags = append(diags, extDiags...)
	diags = append(diags, subDiags...)
	diags = append(diags, keptDiags...)

	out.Unmodeled = MergeUnmodeled(MergeUnmodeled(ext, sub), kept)
	return out, diags
}

// applyNodeType fills the 3.2 nodeType an XML object declares, read off the raw
// node because the parser's XML model has no field for it (GitHub #615).
// ir.XMLHints.NodeType already exists and its GoDoc already names the version.
//
// A schema declaring no xml object has no XMLHints to fill — nodeType is written
// inside the xml object, so there is no position it could have been declared at.
// A declared nodeType wins over the attribute flag: 3.2 replaces `attribute:
// true` with `nodeType: attribute`, so a document writing both has stated the
// newer field, and the reader that took it last makes that the value.
func applyNodeType(h *ir.XMLHints, s *oas3.Schema) {
	if h == nil {
		return
	}
	if node := RawChildNode(RawPropertyNode(s, "xml"), "nodeType"); node != nil {
		h.NodeType = node.Value
	}
}

// subObjectKeys collects what a schema's xml, discriminator and externalDocs
// declare that reaches no IR field: the x-* they carry and the keys the
// specification defines for none of them. None of those IR types holds an
// Unmodeled map, so the entries ride on the schema's node, keyed apart by the
// keyword each was written under.
//
// The census is graded as an OpenAPI object's, not a schema keyword's. reads32
// names the key a 3.2 document defines that Read already takes raw, the XML
// object's `nodeType`, so the census skips it there and still warns below 3.2.
func subObjectKeys(s *oas3.Schema, pointer jsontext.Pointer, locate Locator, reads32 bool) (ir.Unmodeled, []ir.Diagnostic) {
	subs := []struct {
		keyword string
		obj     any
		ext     *extensions.Extensions
		decided []string
	}{
		{"xml", s.GetXML(), s.GetXML().GetExtensions(), nodeTypeKey(reads32)},
		{"discriminator", s.GetDiscriminator(), s.GetDiscriminator().GetExtensions(), nil},
		{"externalDocs", s.GetExternalDocs(), s.GetExternalDocs().GetExtensions(), nil},
	}
	var out ir.Unmodeled
	var diags []ir.Diagnostic
	for _, sub := range subs {
		owner := pointer + ids.Ptr(sub.keyword)
		ext, extDiags := ExtensionsUnder(sub.ext, locate, owner, sub.keyword)
		out = MergeUnmodeled(out, ext)
		diags = append(diags, extDiags...)
		diags = append(diags, UnknownKeysDecided(&out, sub.obj, locate, owner, sub.keyword, sub.decided)...)
	}
	return out, diags
}

// nodeTypeKey names the XML object's 3.2 `nodeType` for the census, and nothing
// below 3.2: the key is a misspelling there rather than a field the dialect
// added, and the warning is what says so.
func nodeTypeKey(reads32 bool) []string {
	if !reads32 {
		return nil
	}
	return []string{"nodeType"}
}

// unmodeledAt collects every keyword a site declares that the IR keeps verbatim
// instead of modelling, each under the reason that says which of those it is
// (§12): validation logic the IR draws a boundary against (§4.7), and JSON
// Schema resource/dialect metadata the IR excludes on purpose.
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
func dialectAt(s *oas3.Schema, pointer jsontext.Pointer, locate Locator) (ir.Unmodeled, []ir.Diagnostic) {
	var p ir.Unmodeled
	var diags []ir.Diagnostic
	for _, keyword := range DialectKeywords {
		prov := locate(pointer + ids.Ptr(keyword))
		kept, keptDiags := PreserveNodeInto(&p, "openapi:"+keyword, RawPropertyNode(s, keyword),
			ir.ReasonOutOfScope, prov)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, prov,
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
func schemaExamplesAt(s *oas3.Schema, pointer jsontext.Pointer, locate Locator) ([]ir.Example, []ir.Diagnostic) {
	var out []ir.Example
	var diags []ir.Diagnostic
	if node := s.GetExample(); node != nil {
		out, diags = appendExampleAt(out, diags, node, locate, pointer, "example")
	}
	for i, node := range s.GetExamples() {
		out, diags = appendExampleAt(out, diags, node, locate, pointer, "examples", strconv.Itoa(i))
	}
	return out, diags
}

// appendExampleAt converts one example node, reporting an unconvertible value
// rather than dropping it.
func appendExampleAt(out []ir.Example, diags []ir.Diagnostic, node *yaml.Node,
	locate Locator, base jsontext.Pointer, seg ...string,
) ([]ir.Example, []ir.Diagnostic) {
	v, err := value.FromNode(node)
	if err != nil {
		return out, append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct,
			locate(base+ids.Ptr(seg...)), "example: %s", err.Error()))
	}
	return append(out, ir.Example{Value: &v}), diags
}

// validationOnlyAt collects the §4.7 keywords a schema declares that the IR does
// not model, keeping each verbatim and announcing it.
//
// Site-only: these constrain the value at the position that wrote them.
func validationOnlyAt(s *oas3.Schema, pointer jsontext.Pointer, locate Locator) (ir.Unmodeled, []ir.Diagnostic) {
	var p ir.Unmodeled
	var diags []ir.Diagnostic

	// keep takes the conversion's error alongside its payload so a keyword that
	// could not be converted is reported rather than passed on as an absent one —
	// the two were indistinguishable here before GitHub #144.
	//
	// combined names the keywords an entry folds together, when it folds several.
	// Such an entry is located at the schema and attributed by those keywords,
	// and so is the report that stands in for it when it cannot be kept.
	keep := func(key string, raw ir.RawValue, err error, entryPtr jsontext.Pointer, label string, combined ...string) {
		from := make([]jsontext.Pointer, 0, len(combined))
		for _, keyword := range combined {
			from = append(from, pointer+ids.Ptr(keyword))
		}
		entry := locate(entryPtr, from...)
		if err != nil {
			diags = append(diags, UnpreservableDiag(key, entry, err))
			return
		}
		diags = append(diags, PreserveKeywordInto(&p, key, raw, entry, locate(pointer), label)...)
	}
	// A keyword whose entry is its own node needs no label of its own: the
	// keyword names it. The §4.7 entries combining several keywords into one
	// object — if/then/else, contains, unevaluated — reach keep directly, because
	// no single keyword names those.
	keepKeyword := func(keyword string) {
		raw, err := RawFromNode(RawPropertyNode(s, keyword))
		keep("openapi:"+keyword, raw, err, pointer+ids.Ptr(keyword), keyword)
	}

	if s.GetNot() != nil {
		keepKeyword("not")
	}
	ite, iteKeys, iteErr := IfThenElseRaw(s)
	if ite != nil || iteErr != nil {
		keep("openapi:if-then-else", ite, iteErr, pointer, "if/then/else", iteKeys...)
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
	craw, cKeys, cErr := ContainsRaw(s)
	if craw != nil || cErr != nil {
		keep("openapi:contains", craw, cErr, pointer, "contains", cKeys...)
	}
	u, uKeys, uErr := UnevaluatedRaw(s)
	if u != nil || uErr != nil {
		keep("openapi:unevaluated", u, uErr, pointer, "unevaluated", uKeys...)
	}
	return p, diags
}

// PreserveInto records raw under key in p, located at prov, or records nothing
// when there are no bytes to record.
//
// len rather than a nil comparison: nil and a zero-length slice are distinct
// states, and an empty payload is the worse of the two. It preserves no
// construct, and it fails the encoding of the whole document that carries it.
func PreserveInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	reason ir.UnmodeledReason, prov ir.Provenance,
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
		Provenance: prov,
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
	reason ir.UnmodeledReason, prov ir.Provenance,
) (bool, []ir.Diagnostic) {
	raw, err := RawFromNode(node)
	if err != nil {
		return false, []ir.Diagnostic{UnpreservableDiag(key, prov, err)}
	}
	if len(raw) == 0 {
		return false, nil
	}
	PreserveInto(p, key, raw, reason, prov)
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
func UnpreservableDiag(key string, prov ir.Provenance, err error) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.UnpreservableConstruct, prov,
		"%s could not be kept verbatim under Unmodeled and is represented in the IR "+
			"in no form at all: %s", key, err.Error())
}

// PreserveKeywordInto records a validation-only keyword at entry and returns the
// one diagnostic announcing it at note, the schema that wrote it. An absent
// payload records nothing and announces nothing; an unconvertible one never
// reaches here, because its caller reports it through UnpreservableDiag first.
func PreserveKeywordInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	entry, note ir.Provenance, label string,
) []ir.Diagnostic {
	if len(raw) == 0 {
		return nil
	}
	PreserveInto(p, key, raw, ir.ReasonValidationOnly, entry)
	return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.ValidationOnlyKeyword, note,
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
