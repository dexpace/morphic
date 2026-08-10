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
// exclusiveMinimum/exclusiveMaximum dialect (see applyExclusive), and under the
// 2020-12 one a side that declares both of its keywords is settled by
// reconcileBound rather than by whichever ran last.
//
// The keyword that reconciliation leaves out of ir.Constraints comes back as the
// second return, an ir.Unmodeled the caller merges into whichever carrier its
// reading position owns. pointer and srcIndex locate it, exactly as they locate
// what Read keeps. Everything else a schema says about its values reaches a
// field, so on all but a co-declared numeric bound that map is nil.
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

// boundResidue is where a schema's bounds were written, and what became of the
// co-declared keywords that reached no field of ir.Constraints.
//
// One value serves both sides, so a schema co-declaring each of them leaves two
// entries here and each keyword survives — writing the map rather than adding to
// it would keep whichever side ran second.
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

// keepRedundant keeps the co-declared keyword that ir.Constraints has no room
// for, and returns the diagnostic reporting the pair.
//
// It writes back the literal already read rather than re-reading the keyword's
// raw node. The two produce the same bytes — RawFromNode renders a numeric
// scalar through the same value.NumericLiteral this bound came from — but only
// this one cannot fail, since BigVal's contract is that its text renders as a
// JSON number. That is what lets the message state the keyword is kept without
// a branch for the case where it was not.
func (b *boundResidue) keepRedundant(keptProp string, kept ir.BigVal, dropProp string, dropped ir.BigVal, compared bool) ir.Diagnostic {
	PreserveInto(&b.kept, "openapi:"+dropProp, ir.RawValue(dropped),
		ir.ReasonDegradedLowering, b.pointer+ids.Ptr(dropProp), b.srcIndex)
	return redundantBoundDiag(keptProp, kept, dropProp, dropped, compared)
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
// 3.0 boolean arm flags the corresponding Min/Max as exclusive; the 2020-12
// numeric arm (3.1/3.2) carries the bound value itself, read from the raw node to
// avoid the float64 trap, and hands it to reconcileBound, which decides how it
// meets any minimum/maximum declared beside it. side picks which of the two
// keywords is read, residue is where the reconciliation records the one that
// reaches no field, and exclusiveBoolean selects the dialect (true for 3.0).
// Because load suppresses the library's type-mismatch on these keywords, a
// value in the wrong form for the dialect is reported and dropped here rather
// than silently accepted.
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
		if b := ev.GetLeft(); b != nil && *b {
			setExclusiveFlag(c, side)
		}
		return nil
	}
	node := RawPropertyNode(s, prop)
	if node == nil {
		return nil
	}
	v, err := value.NumericLiteral(node)
	if err != nil {
		return []ir.Diagnostic{boundLiteralDiag(prop, node.Value, err)}
	}
	return reconcileBound(c, side, residue, v)
}

// reconcileBound settles one side's bound when the 2020-12 dialect declares
// both keywords for it: the inclusive minimum/maximum numericBounds has already
// put in c, and the exclusive bound excl read alongside it.
//
// The two are independent and conjunctive there — "x >= m and x > e" — so the
// tighter of them is the effective bound and the other adds nothing. ir.Constraints
// holds one bound plus one exclusivity flag per side, so the tighter one is kept;
// taking the exclusive bound unconditionally, as this did before, published a
// constraint weaker than the source wherever minimum was the tighter (GitHub #33).
//
// The discarded keyword is implied by the kept one, so no value the source
// admits or excludes changes. What would change is the record that the source
// spelled the bound twice, so it is kept verbatim on residue rather than left to a
// diagnostic message: a consumer reconstructing or diffing the source reads the
// document, not the diagnostics, and cannot otherwise tell
// {minimum: 10, exclusiveMinimum: 0} from {minimum: 10} (GitHub #286).
func reconcileBound(c *ir.Constraints, side boundSide, residue *boundResidue, excl ir.BigVal) []ir.Diagnostic {
	incl, inclProp, exclProp := c.Max, "maximum", "exclusiveMaximum"
	if side == minBound {
		incl, inclProp, exclProp = c.Min, "minimum", "exclusiveMinimum"
	}
	if incl == nil {
		setExclusiveBound(c, side, &excl)
		return nil
	}

	tighter, compared := inclusiveIsTighter(*incl, excl, side)
	if tighter {
		return []ir.Diagnostic{residue.keepRedundant(inclProp, *incl, exclProp, excl, compared)}
	}

	dropped := *incl
	setExclusiveBound(c, side, &excl)
	return []ir.Diagnostic{residue.keepRedundant(exclProp, excl, inclProp, dropped, compared)}
}

// inclusiveIsTighter reports whether the inclusive bound incl admits fewer
// values than the exclusive bound excl written on the same side, and whether
// the two could be compared at all.
//
// A minimum is tighter when it is the greater of the two, a maximum when it is
// the lesser; equal magnitudes are never tighter, which is what gives the
// exclusive bound the tie on both sides ("x >= 5 and x > 5" is "x > 5",
// "x <= 5 and x < 5" is "x < 5").
//
// The comparison is exact and never rounds to float64: these are the literals
// BigVal exists to keep intact, so comparing them as floats would let a pair
// that differs past float64's precision — or one beyond its range — pick the
// wrong bound, reintroducing the defect this reconciliation exists to fix. It
// is also total over every magnitude a spec may legally write, which a rational
// is not: math/big will not build 1e1000001 as one, and a bound it cannot order
// is a bound it may silently widen.
//
// What it cannot order is a literal outside the decimal grammar, and there the
// caller keeps the exclusive bound and says the other may have been the tighter.
// No schema reaches that today — every bound comes through ir.NewBigVal, whose
// grammar is the narrower of the two — so it stands for the day that changes:
// a bound this cannot order is one that could be silently replaced by the looser
// of its pair, which is the defect this reconciliation exists to prevent.
func inclusiveIsTighter(incl, excl ir.BigVal, side boundSide) (tighter, compared bool) {
	inclDec, inclOK := parseDecimalBound(incl)
	exclDec, exclOK := parseDecimalBound(excl)
	if !inclOK || !exclOK {
		return false, false
	}
	order := compareDecimalBounds(inclDec, exclDec)
	if order == 0 {
		return false, true
	}
	return (order > 0) == (side == minBound), true
}

// redundantBoundDiag reports the co-declared 2020-12 bound that reached no
// field of ir.Constraints, naming both keywords and both exact literals so a
// reader can see which bound the IR carries without going back to the source.
//
// It states that the other keyword is kept verbatim because keepRedundant has
// already kept it, by a route with no failure to report.
//
// compared tells the two cases apart. When the magnitudes did compare, the kept
// bound is provably the tighter and the other is redundant, which costs the
// consumer nothing — hence info severity. When they did not, the kept bound is
// the exclusive one by fallback and may be the looser of the two, so the message
// says so and the severity rises to warning.
func redundantBoundDiag(keptProp string, kept ir.BigVal, dropProp string, dropped ir.BigVal, compared bool) ir.Diagnostic {
	if !compared {
		return diag.Newf(ir.SeverityWarning, diag.DegradedConstruct, ir.Provenance{},
			"%s %s and %s %s both bound this value but their magnitudes could not be compared; "+
				"kept %s as the bound, and %s, which may be the tighter of the two, verbatim under Unmodeled",
			keptProp, kept, dropProp, dropped, keptProp, dropProp)
	}
	return diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, ir.Provenance{},
		"%s %s and %s %s both bound this value and the IR holds one bound per side; "+
			"kept %s as the tighter of the two, and %s, which it implies, verbatim under Unmodeled",
		keptProp, kept, dropProp, dropped, keptProp, dropProp)
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

// setExclusiveFlag marks the low or high bound exclusive.
func setExclusiveFlag(c *ir.Constraints, side boundSide) {
	if side == minBound {
		c.ExclusiveMin = true
		return
	}
	c.ExclusiveMax = true
}

// setExclusiveBound sets an exclusive numeric bound (2020-12 arm) on Min or Max,
// replacing whatever minimum/maximum put there. Only reconcileBound may call it,
// which is where the replacement is decided; calling it directly is the shape of
// GitHub #33.
func setExclusiveBound(c *ir.Constraints, side boundSide, v *ir.BigVal) {
	if side == minBound {
		c.Min = v
		c.ExclusiveMin = true
		return
	}
	c.Max = v
	c.ExclusiveMax = true
}

// emptyConstraints reports whether c carries no scalar constraint set by
// Constraints (collection bounds are not read here). Every scalar field that
// Constraints populates must appear in this check; a
// missing field silently leaks a non-nil *Constraints when it should be nil.
func emptyConstraints(c *ir.Constraints) bool {
	return c.Min == nil && c.Max == nil && !c.ExclusiveMin && !c.ExclusiveMax &&
		c.MultipleOf == nil && c.Precision == nil && c.Scale == nil &&
		c.MinLength == nil && c.MaxLength == nil &&
		c.Pattern == "" && c.PatternMessage == "" &&
		c.MinProps == nil && c.MaxProps == nil
}
