package graphql

import (
	"encoding/json"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/ir"
)

// applyInaccessibleType maps @inaccessible on a type onto Access = "internal":
// the type is not part of the exported client surface. The directive itself is
// still preserved verbatim under federation:@inaccessible for losslessness.
func (l *lowerer) applyInaccessibleType(c *ir.TypeCommon, dirs ast.DirectiveList) {
	if dirs.ForName("inaccessible") != nil {
		c.Access = "internal"
	}
}

// applyInaccessibleProp maps @inaccessible on a field onto Visibility.None: the
// field is excluded from every projection (visible in no lifecycle), the closest
// IR fact to "not accessible to clients".
func (l *lowerer) applyInaccessibleProp(p *ir.Property, dirs ast.DirectiveList) {
	if dirs.ForName("inaccessible") != nil {
		p.Visibility = ir.Visibility{None: true}
	}
}

// reportUnknownType records a dangling type reference once per name; the validate
// pass reports the resulting dangling TypeRef downstream.
func (l *lowerer) reportUnknownType(name string, pos *ast.Position) {
	if l.unknownRefs[name] {
		return
	}
	l.unknownRefs[name] = true
	l.diags = append(l.diags, diagf(ir.SeverityWarning, codeUnknownType,
		positionProvenance(pos, l.srcIndex), "type %q referenced but not defined", name))
}

// extendOccurrence is one `extend` occurrence recorded for SDL round-trip
// fidelity (ir-design §8.4).
type extendOccurrence struct {
	// Source indexes into Document.Sources.
	Source int `json:"source"`
	// Pointer is the line:col of the extend occurrence.
	Pointer string `json:"pointer,omitempty"`
	// Baseless marks a type whose only occurrences are extensions (a federation
	// subgraph extending a type owned elsewhere).
	Baseless bool `json:"baseless,omitempty"`
}

// recordExtends records the `extend` occurrences of a type under
// graphql:extends, so the assembled node's provenance to each contributing
// occurrence survives (ir-design §8.4).
func (l *lowerer) recordExtends(c *ir.TypeCommon, md *mergedDef) {
	occ := l.extendOccurrences(md)
	if len(occ) == 0 {
		return
	}
	c.Extensions = mergeExtensions(c.Extensions, ir.Extensions{"graphql:extends": jsonArray(occ)})
}

// extendOccurrences renders every extension occurrence of md as a JSON object.
// A baseless type contributes its synthesized-base occurrence first.
func (l *lowerer) extendOccurrences(md *mergedDef) []json.RawMessage {
	var occ []json.RawMessage
	if md.baseless {
		occ = append(occ, l.occurrenceJSON(md.def.Position, true))
	}
	for _, ext := range md.extensions {
		occ = append(occ, l.occurrenceJSON(ext.Position, false))
	}
	return occ
}

// occurrenceJSON renders one extend occurrence.
func (l *lowerer) occurrenceJSON(pos *ast.Position, baseless bool) json.RawMessage {
	prov := positionProvenance(pos, l.srcIndex)
	raw, _ := json.Marshal(extendOccurrence{Source: prov.Source, Pointer: prov.Pointer, Baseless: baseless})
	return raw
}

// oneOfInputMarker flags a union that originated from a @oneOf input object, so
// emitters can recover its input-only nature (Union carries no InputOnly field).
func oneOfInputMarker() ir.Extensions {
	return ir.Extensions{"graphql:oneOfInput": json.RawMessage("true")}
}
