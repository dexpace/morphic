// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// judgeOwned returns j with the findings about the values the source's check
// left to the load phase among its own, each read in the dialect of where it
// is in the source, which the bindings, once read, give (Dialects).
func judgeOwned(j judgment, d Dialects) judgment {
	j.diags = append(j.diags, ownedFindings(j.owned, func(o oas.Owned) oas.SchemaRules {
		return d.rowsAt(bind.Position{Doc: bind.Source, Pointer: o.Schema})
	})...)
	return j
}

// ownedFindings returns the finding about each value of owned, which the
// table's check left to the load phase (oas.Options.Owned), read with the rows
// rules gives the schema writing it (ownedFinding). A node an alias places at
// several positions is one defect, reported where it is first read malformed.
func ownedFindings(owned []oas.Owned, rules func(oas.Owned) oas.SchemaRules) []ir.Diagnostic {
	var out []ir.Diagnostic
	var said map[ir.Diagnostic]bool
	for _, o := range owned {
		d, bad := ownedFinding(o, rules(o))
		if !bad || said[oas.AsPlaced(d)] {
			continue
		}
		if said == nil {
			said = map[ir.Diagnostic]bool{}
		}
		said[oas.AsPlaced(d)] = true
		out = append(out, d)
	}
	return out
}

// ownedFinding returns the error about o read with rules, the rows of the
// dialect the schema writing it is read in, and false where it is sound: an
// identifier or anchor of another format than the rows give it
// (oas.SchemaRules.FormatFinding),
// an `items` written as a list where the rows read no tuple, and where they
// read one, the rest beside it (oas.SchemaRules.TupleRestFinding), an exclusive bound
// in the other form than the rows give it, and a bound, a boolean exclusive
// bound aside, no decimal spells, quoted or not (value.NumericLiteral).
func ownedFinding(o oas.Owned, rules oas.SchemaRules) (ir.Diagnostic, bool) {
	switch o.Field {
	case "$id", "$anchor", "$dynamicAnchor":
		return rules.FormatFinding(o)
	case "items":
		switch {
		case o.Value.Kind != yaml.SequenceNode:
			return ir.Diagnostic{}, false
		case rules.Takes("items", oas.List):
			return rules.TupleRestFinding(o)
		}
		return o.Finding(diag.InvalidItemsList, "items must be a schema in this schema's dialect, not a list; %s",
			itemsListRemedy(rules)), true
	case "exclusiveMinimum", "exclusiveMaximum":
		boolean := rules.Takes(o.Field, oas.Boolean)
		if written := isBoolean(o.Value); written != boolean {
			want := map[bool]string{true: "a boolean", false: "a number"}[boolean]
			return o.Finding(diag.ExclusiveBoundForm, "%s must be %s in this schema's dialect", o.Field, want), true
		}
		if boolean {
			return ir.Diagnostic{}, false
		}
	case "minimum", "maximum", "multipleOf":
	default:
		return ir.Diagnostic{}, false
	}
	if _, err := value.NumericLiteral(o.Value); err != nil {
		return o.Finding(diag.NumericPrecision, "%s literal %q: %s", o.Field, o.Value.Value, err.Error()), true
	}
	return ir.Diagnostic{}, false
}

// itemsListRemedy says what a writer can do about an `items` list where rules
// read no tuple: write it as `prefixItems`, or name by `$schema` a draft that
// reads one, where the rows define both, as 2020-12's do; else move to 3.1,
// as 3.0's rows define neither.
func itemsListRemedy(rules oas.SchemaRules) string {
	if rules.Defines("prefixItems") && rules.Defines("$schema") {
		return "write a tuple as prefixItems, or name a JSON Schema draft that reads a list as one by $schema"
	}
	return "OpenAPI 3.0 writes no tuple, which 3.1 writes as prefixItems"
}
