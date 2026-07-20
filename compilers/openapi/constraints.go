package openapi

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/ir"
)

// constraintsFromSchema reads a schema's scalar (string/number/object-count)
// value constraints into an ir.Constraints. Numeric bounds are read from the raw
// YAML nodes, never the *float64 model fields, so full decimal precision is
// preserved (the no-float64 invariant). Collection bounds (minItems/maxItems/
// uniqueItems) are List-owned and read elsewhere. A malformed numeric literal
// yields a codeNumericPrecision diagnostic and is skipped; nil is returned when
// no constraint is present.
func constraintsFromSchema(s *oas3.Schema) (*ir.Constraints, []ir.Diagnostic) {
	if s == nil {
		return nil, nil
	}
	c := &ir.Constraints{}
	diags := numericBounds(c, s)
	diags = append(diags, applyExclusive(c, s, true)...)
	diags = append(diags, applyExclusive(c, s, false)...)
	c.MinLength = s.MinLength
	c.MaxLength = s.MaxLength
	c.Pattern = s.GetPattern()
	c.MinProps = s.MinProperties
	c.MaxProps = s.MaxProperties
	if emptyConstraints(c) {
		return nil, diags
	}
	return c, diags
}

// numericBounds fills Min, Max, and MultipleOf from the raw minimum/maximum/
// multipleOf nodes, preserving exact decimal text.
func numericBounds(c *ir.Constraints, s *oas3.Schema) []ir.Diagnostic {
	var diags []ir.Diagnostic
	bounds := []struct {
		prop string
		dst  **ir.BigVal
	}{
		{"minimum", &c.Min},
		{"maximum", &c.Max},
		{"multipleOf", &c.MultipleOf},
	}
	for _, b := range bounds {
		node := rawPropertyNode(s, b.prop)
		if node == nil {
			continue
		}
		v, err := ir.NewBigVal(node.Value)
		if err != nil {
			diags = append(diags, diagf(ir.SeverityWarning, codeNumericPrecision,
				ir.Provenance{}, "%s literal %q: %s", b.prop, node.Value, err.Error()))
			continue
		}
		*b.dst = &v
	}
	return diags
}

// applyExclusive handles exclusiveMinimum/exclusiveMaximum in both dialects: the
// 3.0 boolean arm flags the corresponding Min/Max as exclusive, while the
// 2020-12 numeric arm carries the bound value itself (read from the raw node to
// avoid the float64 trap) and sets the exclusive flag.
func applyExclusive(c *ir.Constraints, s *oas3.Schema, isMin bool) []ir.Diagnostic {
	ev, prop := s.GetExclusiveMaximum(), "exclusiveMaximum"
	if isMin {
		ev, prop = s.GetExclusiveMinimum(), "exclusiveMinimum"
	}
	if ev == nil {
		return nil
	}
	if ev.IsLeft() {
		if b := ev.GetLeft(); b != nil && *b {
			setExclusiveFlag(c, isMin)
		}
		return nil
	}
	node := rawPropertyNode(s, prop)
	if node == nil {
		return nil
	}
	v, err := ir.NewBigVal(node.Value)
	if err != nil {
		return []ir.Diagnostic{diagf(ir.SeverityWarning, codeNumericPrecision,
			ir.Provenance{}, "%s literal %q: %s", prop, node.Value, err.Error())}
	}
	setExclusiveBound(c, isMin, &v)
	return nil
}

// setExclusiveFlag marks the low or high bound exclusive.
func setExclusiveFlag(c *ir.Constraints, isMin bool) {
	if isMin {
		c.ExclusiveMin = true
		return
	}
	c.ExclusiveMax = true
}

// setExclusiveBound sets an exclusive numeric bound (2020-12 arm) on Min or Max.
func setExclusiveBound(c *ir.Constraints, isMin bool, v *ir.BigVal) {
	if isMin {
		c.Min = v
		c.ExclusiveMin = true
		return
	}
	c.Max = v
	c.ExclusiveMax = true
}

// emptyConstraints reports whether c carries no scalar constraint set by
// constraintsFromSchema (collection bounds are not read here).
func emptyConstraints(c *ir.Constraints) bool {
	return c.Min == nil && c.Max == nil && !c.ExclusiveMin && !c.ExclusiveMax &&
		c.MultipleOf == nil && c.MinLength == nil && c.MaxLength == nil &&
		c.Pattern == "" && c.MinProps == nil && c.MaxProps == nil
}
