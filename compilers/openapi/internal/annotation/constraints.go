// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"math/big"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
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

// Bounds names the groups of value bounds a reader takes, each the keywords JSON
// Schema applies to one kind of instance alone.
type Bounds uint8

// The groups of Bounds.
const (
	// NumberBounds are minimum, maximum, their exclusive forms and multipleOf.
	NumberBounds Bounds = 1 << iota
	// StringBounds are minLength, maxLength and pattern.
	StringBounds
	// ObjectBounds are minProperties and maxProperties.
	ObjectBounds

	// AllBounds is every group, for a reader of a value of any kind.
	AllBounds = NumberBounds | StringBounds | ObjectBounds
)

// Constraints reads the value constraints of the groups take names that s
// writes, nil when none is, and nothing of a group not taken. Numeric bounds
// come from the raw YAML nodes, never the *float64 model fields. A literal no
// BigVal holds, an exclusive bound in the other form than exclusiveBoolean
// selects, or a count the model misreads (ExactCount) is skipped, for the rule
// table's check to report (docs/compilers/openapi.md#constraints) or the caller
// to keep. The second return holds a 3.0 exclusive modifier with no bound
// beside it (applyExclusiveFlag), placed by pointer and doc.
func Constraints(s *oas3.Schema, take Bounds, exclusiveBoolean bool, pointer jsontext.Pointer, doc provenance.Doc) (*ir.Constraints, ir.Unmodeled, []ir.Diagnostic) {
	if s == nil {
		return nil, nil, nil
	}
	c := &ir.Constraints{}
	residue := boundResidue{pointer: pointer, doc: doc}
	var diags []ir.Diagnostic
	if take&NumberBounds != 0 {
		numericBounds(c, s)
		diags = applyExclusive(c, s, minBound, &residue, exclusiveBoolean)
		diags = append(diags, applyExclusive(c, s, maxBound, &residue, exclusiveBoolean)...)
	}
	if take&StringBounds != 0 {
		c.MinLength = ExactCount(s, "minLength", s.MinLength)
		c.MaxLength = ExactCount(s, "maxLength", s.MaxLength)
		c.Pattern = s.GetPattern()
	}
	if take&ObjectBounds != 0 {
		c.MinProps = ExactCount(s, "minProperties", s.MinProperties)
		c.MaxProps = ExactCount(s, "maxProperties", s.MaxProperties)
	}
	if emptyConstraints(c) {
		return nil, residue.kept, diags
	}
	return c, residue.kept, diags
}

// ExactCount returns the count the integer keyword s writes states, where the
// parser's model holds one, count, and nil where the literal states no count:
// no number, or none that is a non-negative integer within int64. The count is the literal's, read as
// YAML 1.2's core schema reads it (value.NumericLiteral,
// docs/parser-library.md#scalar-resolution), never the model's,
// which reads 010 as 8, holds an integer past int64 as 0 and cuts a fraction to
// an integer. A negative one is the load phase's to report.
func ExactCount(s *oas3.Schema, keyword string, count *int64) *int64 {
	node := RawPropertyNode(s, keyword)
	if count == nil || node == nil || (node.ShortTag() != "!!int" && node.ShortTag() != "!!float") {
		return nil
	}
	literal, err := value.NumericLiteral(node)
	if err != nil {
		return nil
	}
	stated, ok := new(big.Rat).SetString(string(literal))
	if !ok || !stated.IsInt() || stated.Sign() < 0 || !stated.Num().IsInt64() {
		return nil
	}
	if v := stated.Num().Int64(); v != *count {
		return &v
	}
	return count
}

// CountIsExact reports whether the integer keyword s writes states a count,
// the parser's model holding one, count (ExactCount).
func CountIsExact(s *oas3.Schema, keyword string, count *int64) bool {
	return ExactCount(s, keyword, count) != nil
}

// boundResidue is where a schema's bounds were written, and what became of a
// bound keyword that reached no field of ir.Constraints.
//
// One value serves both sides, so a schema leaving residue on each leaves two
// entries and each keyword survives; writing the map rather than adding to it
// would keep whichever side ran second.
//
// The keyword is recorded here, not handed back, so the diagnostic naming it is
// written at the statement that keeps it. Announcing a preservation elsewhere
// is how a message comes to claim one that never happened (GitHub #144).
type boundResidue struct {
	pointer jsontext.Pointer
	doc     provenance.Doc
	kept    ir.Unmodeled
}

// keepUnmodifiable keeps a 3.0 exclusive-bound modifier that had no bound to
// modify, and returns the diagnostic reporting it.
//
// The literal is the boolean the keyword was written as, which is the whole of
// what it said; there is no numeric bound here to write back, because the
// absence of one is the reason it is being kept at all.
func (b *boundResidue) keepUnmodifiable(inclProp, exclProp string) ir.Diagnostic {
	PreserveInto(&b.kept, "openapi:"+exclProp, ir.RawValue("true"),
		ir.ReasonDegradedLowering, b.doc.At(b.pointer+ids.Ptr(exclProp)))
	return unmodifiableExclusiveDiag(inclProp, exclProp)
}

// numericBounds fills Min, Max, and MultipleOf from the raw minimum/maximum/
// multipleOf nodes, preserving exact decimal text, and skips one no decimal
// spells.
func numericBounds(c *ir.Constraints, s *oas3.Schema) {
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
		if v, err := value.NumericLiteral(node); err == nil {
			*b.dst = &v
		}
	}
}

// applyExclusive reads exclusiveMinimum or exclusiveMaximum, as side picks, in
// either form. The boolean arm (3.0, draft 4) modifies the bound beside it (see
// applyExclusiveFlag). The 2020-12 arm (3.1, 3.2) carries the bound itself,
// read from the raw node to avoid the float64 trap, and writes it to the side's
// exclusive field beside any minimum/maximum rather than in place of it.
// exclusiveBoolean is true for 3.0's dialect and draft 4's. A value in the
// wrong form for the dialect, or a literal no decimal spells, is dropped, and
// the rule table's check reports it (Constraints).
func applyExclusive(c *ir.Constraints, s *oas3.Schema, side boundSide, residue *boundResidue, exclusiveBoolean bool) []ir.Diagnostic {
	ev := s.GetExclusiveMaximum()
	if side == minBound {
		ev = s.GetExclusiveMinimum()
	}
	_, prop := boundProps(side)
	node := RawPropertyNode(s, prop)
	if ev == nil || node == nil {
		return nil
	}
	if ev.IsLeft() != exclusiveBoolean {
		return nil
	}
	if ev.IsLeft() {
		return applyExclusiveFlag(c, s, side, residue, ev.GetLeft())
	}
	if v, err := value.NumericLiteral(node); err == nil {
		setExclusiveBound(c, side, &v)
	}
	return nil
}

// applyExclusiveFlag reads the 3.0 boolean arm, where exclusiveMinimum modifies
// the minimum beside it: "minimum: 5, exclusiveMinimum: true" is "x > 5", which
// ir.Constraints spells as ExclusiveMin. The literal moves to the exclusive
// slot, so a 3.0 document and its 3.1 translation lower alike.
//
// A true modifier with no bound beside it (invalid in draft-4) has nothing to
// make exclusive; it is kept verbatim under Unmodeled and reported.
//
// Absence is read off the raw node, because the parsed slot is also nil for a
// bound numericBounds could not read. That bound has drawn its own error, and
// its modifier is dropped silently.
func applyExclusiveFlag(c *ir.Constraints, s *oas3.Schema, side boundSide, residue *boundResidue, flag *bool) []ir.Diagnostic {
	if flag == nil || !*flag {
		return nil
	}
	inclProp, exclProp := boundProps(side)
	if RawPropertyNode(s, inclProp) == nil {
		return []ir.Diagnostic{residue.keepUnmodifiable(inclProp, exclProp)}
	}
	incl := inclusiveBound(c, side)
	if *incl == nil {
		return nil
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
