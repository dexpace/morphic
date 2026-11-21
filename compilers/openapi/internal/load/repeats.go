// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	refscope "github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable/typed"
	"github.com/dexpace/morphic/ir"
)

// listRepeats reports each entry of a list that repeats what an earlier entry
// of the same list is identified by: a parameter's name and location
// (paramRepeats), or a server's name (serverRepeats), at the position at
// places in the document that writes the list.
//
// items is a walk that visits each list where its document writes it, lowered
// or not, and again wherever a YAML alias mounts it, but never through a $ref:
// walkSource's, or a landing's (landingRepeats). The lists beside a path item's
// $ref are sibs's model's (writtenItem). A panic in the walk is reported at the
// model it reached.
func listRepeats(at func(jsontext.Pointer) ir.Provenance, sibs Siblings, b *typed.Bindings,
	items iter.Seq[soa.WalkItem],
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	site, err := eachModel(items, "repeat check", func(site jsontext.Pointer, model any) error {
		switch m := model.(type) {
		case *soa.OpenAPI:
			// Placed from "", not site: the library renders the root's location
			// as "/", the pointer to a key named "". Read from the field, since
			// GetServers stands in a server the source never wrote.
			diags = append(diags, serverRepeats(at, m.Servers, "")...)
		case *soa.ReferencedPathItem:
			if item, ok := writtenItem(m, b, sibs, site); ok {
				diags = append(diags, paramRepeats(at, b, item.GetParameters(), site)...)
				diags = append(diags, serverRepeats(at, item.GetServers(), site)...)
			}
		case *soa.Operation:
			diags = append(diags, paramRepeats(at, b, m.GetParameters(), site)...)
			diags = append(diags, serverRepeats(at, m.GetServers(), site)...)
		default:
			// No other model holds a parameter or a servers list.
		}
		return nil
	})
	if err != nil {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.Validation, at(site), "%s", err.Error()))
	}
	return diags
}

// landingRepeats reports what listRepeats finds in each path item a reference
// lands on where its document's grammar reads none, such as under an x- key
// or in another document, which the lowering mounts and no walk of the source
// meets (GitHub #880). Each is walked from the landing's own pointer, entering
// the fields beside each $ref in it (sibs), so each list is checked where it is
// written, in whichever document, once, however many references mount it: a
// landing inside another draws its findings twice, identical, which the
// compile records once (compile.Diags).
func landingRepeats(ctx context.Context, sites places, sibs Siblings, b *typed.Bindings) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, objectLanding := range b.Table().ObjectLandings() {
		// A cancelled load reports nothing it found, so it reads no further.
		if ctx.Err() != nil {
			return nil
		}
		rp, built := b.ObjectAt(objectLanding.At, reftable.RefPathItem).(*soa.ReferencedPathItem)
		if objectLanding.Kind != reftable.RefPathItem || !objectLanding.Reached || !built {
			continue
		}
		doc := objectLanding.At.Doc
		at := func(pointer jsontext.Pointer) ir.Provenance {
			return sites.position(reftable.DocumentPointer{Doc: doc, Pointer: pointer})
		}
		items := sibs.interleaved(ctx, locatedWalk(walk(ctx, rp), objectLanding.At.Pointer))
		diags = append(diags, listRepeats(at, sibs, b, items)...)
	}
	return diags
}

// writtenItem returns the path item whose lists rp writes at site: its own
// object, or, where b reads a $ref there, the model of the fields beside it
// when site is the position that model was found at. The model's operations
// are walked from there alone, so its own lists are checked there alone too,
// wherever else an alias mounts it. A $ref with nothing beside it writes no
// list.
func writtenItem(rp *soa.ReferencedPathItem, b *typed.Bindings, sibs Siblings, site jsontext.Pointer,
) (*soa.PathItem, bool) {
	if _, isRef := b.Reference(rp); !isRef {
		return refscope.Object[soa.PathItem](b, rp), true
	}
	sib, ok := sibs.Of(rp)
	return sib.Item, ok && sib.Pointer == site
}

// paramRepeats reports each entry of the parameter list held by the object at
// owner that repeats an earlier entry's slot (ids.SlotOf), which OpenAPI
// forbids, at the repeat, naming the first holder, and its spelling where a
// header's differs. The rule table states the rule and leaves it here, where
// every entry is read through its $ref.
//
// An entry is read as the lowering reads it (refscope.Object), so a repeat
// reported here is one the lowering finds; one whose $ref resolved to nothing
// has no slot.
func paramRepeats(at func(jsontext.Pointer) ir.Provenance, b *typed.Bindings, params []*soa.ReferencedParameter,
	owner jsontext.Pointer,
) []ir.Diagnostic {
	type holder struct {
		at   jsontext.Pointer
		name string
	}
	firstAt := make(map[ids.ParamSlot]holder, len(params))
	var diags []ir.Diagnostic
	for i, rp := range params {
		p := refscope.Object[soa.Parameter](b, rp)
		if p == nil {
			continue
		}
		entry := owner.AppendToken("parameters").AppendToken(strconv.Itoa(i))
		name, in := p.GetName(), string(p.GetIn())
		first, repeated := firstAt[ids.SlotOf(name, in)]
		if !repeated {
			firstAt[ids.SlotOf(name, in)] = holder{at: entry, name: name}
			continue
		}
		diags = append(diags, diag.Newf(ir.SeverityError, diag.DuplicateParameter, at(entry),
			"%s", diag.RepeatedParameter(name, in, first.at, first.name)))
	}
	return diags
}

// serverRepeats reports each entry of the servers list held by the object at
// owner whose name an earlier entry already has, at the repeat's name, naming
// the first holder's. Names compare as written, and an entry without one
// repeats nothing: a URL hint is the lowering's, not a name the source wrote.
// A list shorter than two entries has nothing to compare, so most lists cost
// no map.
func serverRepeats(at func(jsontext.Pointer) ir.Provenance, servers []*soa.Server, owner jsontext.Pointer) []ir.Diagnostic {
	if len(servers) < 2 {
		return nil
	}
	firstAt := make(map[string]jsontext.Pointer, len(servers))
	var diags []ir.Diagnostic
	for i, s := range servers {
		name := s.GetName()
		if name == "" {
			continue
		}
		key := owner.AppendToken("servers").AppendToken(strconv.Itoa(i)).AppendToken("name")
		first, repeated := firstAt[name]
		if !repeated {
			firstAt[name] = key
			continue
		}
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.DuplicateServerName, at(key),
			"server name %q repeats the name at %s; both servers are kept, and a consumer "+
				"keying servers by name cannot tell them apart", name, first))
	}
	return diags
}
