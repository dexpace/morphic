package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// checkHeaderSerialization reports a Property outside a headers list that sets
// Style or Explode.
//
// The two say how a header writes its value into its field, so ir.Property
// defines them for a header alone: an entry of Response, ErrorCase or
// PartEncoding Headers. Set on a model's property or a resource's, they state a
// serialization no exchange applies. The positions are named here because
// nothing in a Property's Go type says which list holds it;
// TestHeaderSerialization_NamesEveryPropertyCarrier fails when the IR grows
// another. The ir/ code namespace is explained at checkEncodingKeys.
func checkHeaderSerialization(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range liveTypeIDs(doc) {
		if m, ok := doc.Types[id].(*ir.Model); ok {
			diags = appendHeaderSerializationDiags(diags, m.Properties, string(id), "properties")
		}
	}
	// A truncated walk can only leave a resource unjudged, and
	// checkGroupWalkTruncated reports that once for the document.
	ir.ForEachGroup(doc, func(g *ir.OperationGroup) {
		if g.Resource == nil {
			return
		}
		diags = appendHeaderSerializationDiags(diags, g.Resource.Identifiers, string(g.ID), "resource/identifiers")
		diags = appendHeaderSerializationDiags(diags, g.Resource.Properties, string(g.ID), "resource/properties")
	})
	return diags
}

// appendHeaderSerializationDiags appends to dst a diagnostic per property of
// props, which no headers list holds, that sets Style or Explode; owner and
// field locate props, spelled only for a diagnostic.
func appendHeaderSerializationDiags(dst []ir.Diagnostic, props []ir.Property, owner, field string) []ir.Diagnostic {
	for i, p := range props {
		if p.Style == "" && p.Explode == nil {
			continue
		}
		at := fmt.Sprintf("%s/%s/%d", owner, field, i)
		dst = append(dst, diag(ir.SeverityError, "ir/header-serialization-outside-header",
			fmt.Sprintf("property %s sets a header's style or explode, which only an entry of a headers list carries", at),
			at))
	}
	return dst
}
