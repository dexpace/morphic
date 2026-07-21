package graphql

import (
	"strings"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/ir"
)

// mergedDef is a type definition assembled from its base occurrence and every
// `extend` occurrence of the same name (ir-design §8.4). Merging appends
// extension members after base members; each member keeps its own Position, so
// per-member provenance falls out of the source without extra bookkeeping.
type mergedDef struct {
	// def is the merged definition: base members followed by extension members.
	def *ast.Definition
	// extensions holds the `extend` occurrences, in source order, for the
	// graphql:extends provenance record.
	extensions []*ast.Definition
	// baseless reports that no base definition existed (a federation subgraph
	// extending a type owned by another subgraph); def is synthesized from the
	// first extension.
	baseless bool
}

// rootNames holds the three root operation type names, resolved from the schema
// block(s) or defaulted to the conventional Query/Mutation/Subscription.
type rootNames struct {
	query        string
	mutation     string
	subscription string
}

// isRoot reports whether name is one of the resolved root operation type names.
func (r rootNames) isRoot(name string) bool {
	return name != "" && (name == r.query || name == r.mutation || name == r.subscription)
}

// buildDefs assembles the by-name definition map, merging every `extend`
// occurrence into its base and synthesizing a base for extension-only types. A
// duplicate base definition keeps the first and reports the rest.
func buildDefs(doc *ast.SchemaDocument, index map[*ast.Source]int) (map[string]*mergedDef, []ir.Diagnostic) {
	defs := make(map[string]*mergedDef, len(doc.Definitions))
	var diags []ir.Diagnostic
	for _, d := range doc.Definitions {
		if existing, ok := defs[d.Name]; ok {
			diags = append(diags, duplicateDiag(d, existing, index))
			continue
		}
		defs[d.Name] = &mergedDef{def: cloneDefinition(d)}
	}
	for _, ext := range doc.Extensions {
		mergeExtension(defs, ext)
	}
	return defs, diags
}

// duplicateDiag reports a second definition of an already-defined type name.
func duplicateDiag(dup *ast.Definition, first *mergedDef, index map[*ast.Source]int) ir.Diagnostic {
	return diagf(ir.SeverityWarning, codeDuplicateType, positionProvenance(dup.Position, index),
		"type %q redefined; keeping the first definition", first.def.Name)
}

// mergeExtension folds one `extend` occurrence into its base, creating a
// synthesized baseless entry when the base is absent.
func mergeExtension(defs map[string]*mergedDef, ext *ast.Definition) {
	md, ok := defs[ext.Name]
	if !ok {
		md = &mergedDef{def: cloneDefinition(ext), baseless: true}
		defs[ext.Name] = md
		return
	}
	md.def.Interfaces = append(md.def.Interfaces, ext.Interfaces...)
	md.def.Directives = append(md.def.Directives, ext.Directives...)
	md.def.Fields = append(md.def.Fields, ext.Fields...)
	md.def.Types = append(md.def.Types, ext.Types...)
	md.def.EnumValues = append(md.def.EnumValues, ext.EnumValues...)
	md.extensions = append(md.extensions, ext)
}

// cloneDefinition copies the mutable slices of a definition so merging never
// mutates the parser's AST (which callers may still inspect).
func cloneDefinition(d *ast.Definition) *ast.Definition {
	clone := *d
	clone.Interfaces = append([]string(nil), d.Interfaces...)
	clone.Directives = append(ast.DirectiveList(nil), d.Directives...)
	clone.Fields = append(ast.FieldList(nil), d.Fields...)
	clone.Types = append([]string(nil), d.Types...)
	clone.EnumValues = append(ast.EnumValueList(nil), d.EnumValues...)
	return &clone
}

// resolveRootNames determines the three root operation type names from the
// schema definition and extension blocks, falling back to the conventional
// names when a matching type exists.
func resolveRootNames(doc *ast.SchemaDocument, defs map[string]*mergedDef) rootNames {
	r := rootNames{}
	for _, block := range append(append(ast.SchemaDefinitionList(nil), doc.Schema...), doc.SchemaExtension...) {
		for _, ot := range block.OperationTypes {
			applyOperationType(&r, ot)
		}
	}
	r.query = defaultRoot(r.query, "Query", defs)
	r.mutation = defaultRoot(r.mutation, "Mutation", defs)
	r.subscription = defaultRoot(r.subscription, "Subscription", defs)
	return r
}

// applyOperationType records one schema-block operation-type mapping.
func applyOperationType(r *rootNames, ot *ast.OperationTypeDefinition) {
	switch ot.Operation {
	case ast.Query:
		r.query = ot.Type
	case ast.Mutation:
		r.mutation = ot.Type
	case ast.Subscription:
		r.subscription = ot.Type
	}
}

// defaultRoot returns current when set, else the conventional name when a type
// of that name is defined, else "".
func defaultRoot(current, conventional string, defs map[string]*mergedDef) string {
	if current != "" {
		return current
	}
	if _, ok := defs[conventional]; ok {
		return conventional
	}
	return ""
}

// federationSpecHost identifies the Apollo Federation v2 @link spec URL.
const federationSpecHost = "specs.apollo.dev/federation"

// detectFederationVersion returns "2" when a federation @link is present, "1"
// when a v1 federation directive is used without @link, and "" otherwise
// (ir-design §8.4: v2 is detected by the @link to the federation spec URL).
func detectFederationVersion(doc *ast.SchemaDocument, defs map[string]*mergedDef) string {
	if hasFederationLink(doc) {
		return "2"
	}
	if usesFederationDirective(defs) {
		return "1"
	}
	return ""
}

// hasFederationLink reports whether any schema block @links the federation spec.
func hasFederationLink(doc *ast.SchemaDocument) bool {
	for _, block := range append(append(ast.SchemaDefinitionList(nil), doc.Schema...), doc.SchemaExtension...) {
		for _, d := range block.Directives.ForNames("link") {
			if linkTargetsFederation(d) {
				return true
			}
		}
	}
	return false
}

// linkTargetsFederation reports whether a @link application points at the
// federation spec URL.
func linkTargetsFederation(d *ast.Directive) bool {
	arg := d.Arguments.ForName("url")
	return arg != nil && arg.Value != nil && strings.Contains(arg.Value.Raw, federationSpecHost)
}

// usesFederationDirective reports whether any type or field carries a v1
// federation directive.
func usesFederationDirective(defs map[string]*mergedDef) bool {
	for _, md := range defs {
		if directivesIncludeFederation(md.def.Directives) {
			return true
		}
		for _, f := range md.def.Fields {
			if directivesIncludeFederation(f.Directives) {
				return true
			}
		}
	}
	return false
}

// directivesIncludeFederation reports whether any directive in the list is a
// federation directive.
func directivesIncludeFederation(dirs ast.DirectiveList) bool {
	for _, d := range dirs {
		if isFederationDirective(d.Name) {
			return true
		}
	}
	return false
}
