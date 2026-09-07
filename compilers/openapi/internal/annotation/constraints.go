package annotation

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// boundSide names which of a numeric constraint's two sides a read applies to.
// It is a named type rather than a bool because applyExclusive takes a dialect
// flag beside it, and two bare bools in a row say nothing at the call site about
// which is which.
type boundSide int

const (
	minBound boundSide = iota // minimum / exclusiveMinimum
	maxBound                  // maximum / exclusiveMaximum
)

// Constraints reads a schema's scalar (string/number/object-count) value
// constraints into an ir.Constraints. Numeric bounds are read from the raw YAML
// nodes, never the *float64 model fields, to preserve full decimal precision
// (the no-float64 invariant). Collection bounds (minItems/maxItems/uniqueItems)
// are List-owned and read elsewhere. A non-finite bound literal yields an
// error-severity diag.NumericPrecision diagnostic and is skipped; nil is
// returned when no constraint is present. exclusiveBoolean selects the
// exclusiveMinimum/exclusiveMaximum dialect (see applyExclusive); under the
// 2020-12 one a side may declare both of its keywords, and both reach a field
// of their own, so neither is chosen over the other.
//
// A keyword that reaches no field comes back as the second return, an
// ir.Unmodeled the caller merges into whichever carrier its reading position
// owns. pointer and srcIndex locate it, exactly as they locate what Read keeps.
// One keyword can land there: a 3.0 exclusiveMinimum/exclusiveMaximum true with
// no bound beside it to make exclusive (see applyExclusiveFlag). Everything
// else a schema says about its values reaches a field, so that map is usually
// nil.
//
// It reads beside the other readers here for the reason they are here at all:
// what a schema says about the values admitted at a position is read the same
// way whoever asks, and none of it needs the lowering walk. Which dialect
// applies is the caller's to decide — that is a fact about the document, not
// about the schema, and it is the one thing this reader will not go and find.
func Constraints(s *oas3.Schema, exclusiveBoolean bool, pointer string, srcIndex int) (*ir.Constraints, ir.Unmodeled, []ir.Diagnostic) {
	if s == nil {
		return nil, nil, nil
	}
	c := &ir.Constraints{}
	residue := boundResidue{pointer: pointer, srcIndex: srcIndex}
	diags := numericBounds(c, s)
	diags = append(diags, applyExclusive(c, s, minBound, &residue, exclusiveBoolean)...)
	diags = append(diags, applyExclusive(c, s, maxBound, &residue, exclusiveBoolean)...)
	c.MinLength = s.MinLength
	c.MaxLength = s.MaxLength
	c.Pattern = s.GetPattern()
	c.MinProps = s.MinProperties
	c.MaxProps = s.MaxProperties
	if emptyConstraints(c) {
		return nil, residue.kept, diags
	}
	return c, residue.kept, diags
}

// boundResidue is where a schema's bounds were written, and what became of a
// bound keyword that reached no field of ir.Constraints.
//
// One value serves both sides, so a schema leaving residue on each of them
// leaves two entries here and each keyword survives — writing the map rather
// than adding to it would keep whichever side ran second.
//
// The keyword is recorded here rather than handed back for a caller to record,
// so that the diagnostic naming it is written at the same statement that keeps
// it. Announcing a preservation from anywhere else is how a message comes to
// claim one that never happened (GitHub #144).
//
// Named for the residue rather than the site so it cannot be misread as the
// boundSide beside it in the same signatures.
type boundResidue struct {
	pointer  string
	srcIndex int
	kept     ir.Unmodeled
}

// keepUnmodifiable keeps a 3.0 exclusive-bound modifier that had no bound to
// modify, and returns the diagnostic reporting it.
//
// The literal is the boolean the keyword was written as, which is the whole of
// what it said; there is no numeric bound here to write back, because the
// absence of one is the reason it is being kept at all.
func (b *boundResidue) keepUnmodifiable(inclProp, exclProp string) ir.Diagnostic {
	PreserveInto(&b.kept, "openapi:"+exclProp, ir.RawValue("true"),
		ir.ReasonDegradedLowering, b.pointer+ids.Ptr(exclProp), b.srcIndex)
	return unmodifiableExclusiveDiag(inclProp, exclProp)
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
		node := RawPropertyNode(s, b.prop)
		if node == nil {
			continue
		}
		v, err := value.NumericLiteral(node)
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
	return diag.Newf(ir.SeverityError, diag.NumericPrecision, ir.Provenance{},
		"%s literal %q: %s", prop, literal, err.Error())
}

// applyExclusive handles exclusiveMinimum/exclusiveMaximum in both dialects: the
// 3.0 boolean arm modifies the minimum/maximum written beside it (see
// applyExclusiveFlag); the 2020-12 numeric arm (3.1/3.2) carries the bound value
// itself, read from the raw node to avoid the float64 trap, and writes it to the
// side's own exclusive field, where it stands beside any minimum/maximum
// declared with it rather than in place of it. side picks which of the two
// keywords is read, residue is where the one keyword that can reach no field is
// recorded, and exclusiveBoolean selects the dialect (true for 3.0). Because
// load suppresses the library's type-mismatch on these keywords, a value in the
// wrong form for the dialect is reported and dropped here rather than silently
// accepted.
func applyExclusive(c *ir.Constraints, s *oas3.Schema, side boundSide, residue *boundResidue, exclusiveBoolean bool) []ir.Diagnostic {
	ev, prop := s.GetExclusiveMaximum(), "exclusiveMaximum"
	if side == minBound {
		ev, prop = s.GetExclusiveMinimum(), "exclusiveMinimum"
	}
	if ev == nil {
		return nil
	}
	if ev.IsLeft() != exclusiveBoolean {
		return []ir.Diagnostic{exclusiveFormDiag(prop, exclusiveBoolean)}
	}
	if ev.IsLeft() {
		return applyExclusiveFlag(c, side, residue, ev.GetLeft())
	}
	node := RawPropertyNode(s, prop)
	if node == nil {
		return nil
	}
	v, err := value.NumericLiteral(node)
	if err != nil {
		return []ir.Diagnostic{boundLiteralDiag(prop, node.Value, err)}
	}
	setExclusiveBound(c, side, &v)
	return nil
}

// applyExclusiveFlag reads the 3.0 boolean arm, where exclusiveMinimum is not a
// bound but a modifier of the minimum written beside it: "minimum: 5,
// exclusiveMinimum: true" is "x > 5", which is what ir.Constraints spells as
// ExclusiveMin. So the literal moves from the inclusive slot to the exclusive
// one and the inclusive slot is emptied — the 2020-12 spelling of the same
// restriction, not a lowering of it, and the only reading under which a 3.0
// document and its 3.1 translation lower alike.
//
// A false modifier says the bound beside it is inclusive, which is where
// numericBounds already put it, so it moves nothing.
//
// A true modifier with no bound beside it modifies nothing: draft-4 requires
// minimum wherever exclusiveMinimum appears, so such a schema is invalid, and
// there is no bound for the IR to make exclusive. Dropping it would be a
// declared keyword lost without a word, so it is kept verbatim under Unmodeled
// and reported.
func applyExclusiveFlag(c *ir.Constraints, side boundSide, residue *boundResidue, flag *bool) []ir.Diagnostic {
	if flag == nil || !*flag {
		return nil
	}
	incl := inclusiveBound(c, side)
	if *incl == nil {
		inclProp, exclProp := boundProps(side)
		return []ir.Diagnostic{residue.keepUnmodifiable(inclProp, exclProp)}
	}
	setExclusiveBound(c, side, *incl)
	*incl = nil
	return nil
}

// boundProps names the inclusive and exclusive keyword that bound one side.
func boundProps(side boundSide) (inclProp, exclProp string) {
	if side == minBound {
		return "minimum", "exclusiveMinimum"
	}
	return "maximum", "exclusiveMaximum"
}

// inclusiveBound addresses the Min or Max slot of c, so that a caller reading
// one side can both read and clear it without repeating the side branch.
func inclusiveBound(c *ir.Constraints, side boundSide) **ir.BigVal {
	if side == minBound {
		return &c.Min
	}
	return &c.Max
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
	return diag.Newf(ir.SeverityError, diag.ExclusiveBoundForm, ir.Provenance{},
		"%s must be %s in this OpenAPI dialect", prop, want)
}

// unmodifiableExclusiveDiag reports a 3.0 exclusive-bound modifier written
// without the bound it modifies. Draft-4 requires minimum wherever
// exclusiveMinimum appears (and maximum wherever exclusiveMaximum does), so the
// schema is invalid; but the keyword is one the loader hands to Morphic
// unchecked, and an invalid schema is still a schema whose text a consumer may
// need, so this is a warning over a kept construct rather than an error over a
// dropped one.
func unmodifiableExclusiveDiag(inclProp, exclProp string) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.DegradedConstruct, ir.Provenance{},
		"%s is true with no %s beside it to make exclusive, so it bounds nothing; "+
			"kept verbatim under Unmodeled", exclProp, inclProp)
}

// setExclusiveBound writes the exclusive bound of one side. It never touches
// the inclusive slot: the two keywords are independent, both apply where both
// are declared, and overwriting one with the other published a bound the source
// never wrote (GitHub #33) and hid a change to the overwritten one (GitHub
// #425).
func setExclusiveBound(c *ir.Constraints, side boundSide, v *ir.BigVal) {
	if side == minBound {
		c.ExclusiveMin = v
		return
	}
	c.ExclusiveMax = v
}

// emptyConstraints reports whether c carries no scalar constraint set by
// Constraints (collection bounds are not read here). Every scalar field that
// Constraints populates must appear in this check; a
// missing field silently leaks a non-nil *Constraints when it should be nil.
func emptyConstraints(c *ir.Constraints) bool {
	return c.Min == nil && c.Max == nil && c.ExclusiveMin == nil && c.ExclusiveMax == nil &&
		c.MultipleOf == nil && c.Precision == nil && c.Scale == nil &&
		c.MinLength == nil && c.MaxLength == nil &&
		c.Pattern == "" && c.PatternMessage == "" &&
		c.MinProps == nil && c.MaxProps == nil
}
