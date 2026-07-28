package openapi

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/ir"
)

// constraintsFromSchema reads a schema's scalar (string/number/object-count)
// value constraints into an ir.Constraints. Numeric bounds are read from the raw
// YAML nodes, never the *float64 model fields, to preserve full decimal
// precision (the no-float64 invariant). Collection bounds (minItems/maxItems/
// uniqueItems) are List-owned and read elsewhere. A non-finite bound literal
// yields an error-severity codeNumericPrecision diagnostic and is skipped; nil
// is returned when no constraint is present. exclusiveBoolean selects the
// exclusiveMinimum/exclusiveMaximum dialect (see applyExclusive).
func constraintsFromSchema(s *oas3.Schema, exclusiveBoolean bool) (*ir.Constraints, []ir.Diagnostic) {
	if s == nil {
		return nil, nil
	}
	c := &ir.Constraints{}
	diags := numericBounds(c, s)
	diags = append(diags, applyExclusive(c, s, true, exclusiveBoolean)...)
	diags = append(diags, applyExclusive(c, s, false, exclusiveBoolean)...)
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
		v, err := numericLiteral(node)
		if err != nil {
			diags = append(diags, boundLiteralDiag(b.prop, node.Value, err))
			continue
		}
		*b.dst = &v
	}
	return diags
}

// boundLiteralDiag reports a numeric bound whose literal is not a finite number.
// load already suppresses the library's own float64 type-mismatch check on
// these keywords (a valid magnitude beyond float64 range must not fail the
// spec), so this is the sole diagnostic for a bad bound — hence error severity:
// a non-numeric bound is an invalid schema, not a lossy-but-tolerable value.
func boundLiteralDiag(prop, literal string, err error) ir.Diagnostic {
	return diagf(ir.SeverityError, codeNumericPrecision, ir.Provenance{},
		"%s literal %q: %s", prop, literal, err.Error())
}

// applyExclusive handles exclusiveMinimum/exclusiveMaximum in both dialects: the
// 3.0 boolean arm flags the corresponding Min/Max as exclusive; the 2020-12
// numeric arm (3.1/3.2) carries the bound value itself, read from the raw node to
// avoid the float64 trap, and sets the exclusive flag. exclusiveBoolean selects
// the dialect (true for 3.0). Because load suppresses the library's type-mismatch
// on these keywords, a value in the wrong form for the dialect is reported and
// dropped here rather than silently accepted.
func applyExclusive(c *ir.Constraints, s *oas3.Schema, isMin, exclusiveBoolean bool) []ir.Diagnostic {
	ev, prop := s.GetExclusiveMaximum(), "exclusiveMaximum"
	if isMin {
		ev, prop = s.GetExclusiveMinimum(), "exclusiveMinimum"
	}
	if ev == nil {
		return nil
	}
	if ev.IsLeft() != exclusiveBoolean {
		return []ir.Diagnostic{exclusiveFormDiag(prop, exclusiveBoolean)}
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
	v, err := numericLiteral(node)
	if err != nil {
		return []ir.Diagnostic{boundLiteralDiag(prop, node.Value, err)}
	}
	setExclusiveBound(c, isMin, &v)
	return nil
}

// exclusiveFormDiag reports an exclusiveMinimum/exclusiveMaximum whose value form
// is wrong for the document's dialect: 3.0 spells it as a boolean modifier of
// minimum/maximum, while the 2020-12 dialect (3.1, 3.2) spells it as a numeric
// bound. The mismatched value carries no usable bound, so it is dropped with this
// error rather than accepted as a degenerate constraint.
func exclusiveFormDiag(prop string, exclusiveBoolean bool) ir.Diagnostic {
	want := "a number"
	if exclusiveBoolean {
		want = "a boolean"
	}
	return diagf(ir.SeverityError, codeExclusiveBoundForm, ir.Provenance{},
		"%s must be %s in this OpenAPI dialect", prop, want)
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
// constraintsFromSchema (collection bounds are not read here). Every scalar
// field that constraintsFromSchema populates must appear in this check; a
// missing field silently leaks a non-nil *Constraints when it should be nil.
func emptyConstraints(c *ir.Constraints) bool {
	return c.Min == nil && c.Max == nil && !c.ExclusiveMin && !c.ExclusiveMax &&
		c.MultipleOf == nil && c.Precision == nil && c.Scale == nil &&
		c.MinLength == nil && c.MaxLength == nil &&
		c.Pattern == "" && c.PatternMessage == "" &&
		c.MinProps == nil && c.MaxProps == nil
}

// exclusiveBoundIsBoolean reports whether this document's dialect spells
// exclusiveMinimum/exclusiveMaximum as a boolean modifier (OpenAPI 3.0) rather
// than a numeric bound (the 2020-12 dialect of 3.1 and 3.2). An unrecognized
// version defaults to the 2020-12 numeric form.
func (l *lowerer) exclusiveBoundIsBoolean() bool {
	minor, _ := supportedMinor(l.doc.OpenAPI)
	return minor == "3.0"
}

// appendConstraintDiags stamps constraint diagnostics with pointer's provenance,
// recording them at most once per pointer: a sub-schema reached from both its
// owning property and a $ref that hoists it is read twice, but a malformed bound
// must be reported only once. Constraint data still lands on both nodes; only
// the diagnostic is de-duplicated.
func (l *lowerer) appendConstraintDiags(diags []ir.Diagnostic, pointer string) {
	if l.diagnosedConstraints[pointer] {
		return
	}
	l.diagnosedConstraints[pointer] = true
	for i := range diags {
		diags[i].Provenance = ir.Provenance{Source: l.srcIndex, Pointer: pointer}
	}
	l.diags = append(l.diags, diags...)
}
