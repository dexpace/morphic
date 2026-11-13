// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"math/big"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// ConstraintsOf returns the value constraints s and its voices write, or nil
// where they write none, for the node or carrier at pointer to hold, so a node a
// position owns, a property and a parameter read them alike. A bound several
// write meets (merge.Constraints): a voice speaks only where its bounds meet
// every other writer's (docs/compilers/openapi.md#allof-inline-residue).
//
// p is the Unmodeled map of the node or carrier the constraints land on. A 3.0
// exclusive modifier with no bound beside it to modify, which no ir.Constraints
// field holds, is kept there, beside the constraints it did not reach (GitHub
// #286).
func ConstraintsOf(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, voices []annotation.Voice, pointer jsontext.Pointer) (*ir.Constraints, []ir.Diagnostic) {
	cons, diags := constraintsAt(c, p, s, pointer)
	for _, v := range voices {
		voiced, voicedDiags := constraintsAt(c, p, v.Node, v.At)
		diags = append(diags, voicedDiags...)
		cons = merge.Constraints(cons, voiced)
	}
	writers := append([]annotation.Voice{{Node: s, At: pointer}}, voices...)
	return cons, append(diags, keepCounts(c, p, writers)...)
}

// exclusiveIsBoolean reports whether s spells an exclusive bound as a boolean
// modifying the bound beside it, the shape 3.0's rows and draft 4's give
// exclusiveMinimum, rather than as the bound itself. A keyword's shape is the
// dialect's, which a schema can name, so it is asked of the rows of where s is
// (rulesOf), as the agreement reads a conjunct's bounds where it has s alone.
// The exclusiveMaximum row agrees in every row set, so one answer reads both
// (oas.TestSchemaRules_AnswerAsTheTableDoes).
func exclusiveIsBoolean(c lowering.Ctx, s *oas3.Schema) bool {
	return rulesOf(c, s).Takes("exclusiveMinimum", oas.Boolean)
}

// constraintsAt reads what s, written at pointer, bounds.
func constraintsAt(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) (*ir.Constraints, []ir.Diagnostic) {
	if s == nil {
		return nil, nil
	}
	cons, kept, diags := annotation.Constraints(s, exclusiveIsBoolean(c, s), pointer, c.Place())
	*p = annotation.MergeUnmodeled(*p, kept)
	return cons, stampConstraintDiags(c, diags, pointer)
}

// valueCountKeywords are the counts annotation.Constraints reads, in the order
// keepCounts keeps them.
var valueCountKeywords = []string{"maxLength", "maxProperties", "minLength", "minProperties"}

// keepCounts keeps verbatim in p each value count that writers, a position's
// schema and its voices, write and no field states (keptCount): of several
// writing one keyword, the one binding the rest (bindingCount), so the entry is
// the same whatever their order. Each writer's kept counts are reported once,
// where it was written (docs/compilers/openapi.md#constraints).
func keepCounts(c lowering.Ctx, p *ir.Unmodeled, writers []annotation.Voice) []ir.Diagnostic {
	binding := make([]int, len(valueCountKeywords))
	for k, keyword := range valueCountKeywords {
		binding[k] = bindingCount(writers, keyword)
	}
	var diags []ir.Diagnostic
	for w, writer := range writers {
		var kept []string
		for k, keyword := range valueCountKeywords {
			if binding[k] != w {
				continue
			}
			ok, keptDiags := annotation.PreserveSchemaKeyword(p, writer.Node, keyword, ir.ReasonDegradedLowering, writer.At, c.Place())
			diags = append(diags, stampConstraintDiags(c, keptDiags, writer.At)...)
			if ok {
				kept = append(kept, keyword)
			}
		}
		if len(kept) > 0 {
			diags = append(diags, countsKept(c, kept, writer.At))
		}
	}
	return diags
}

// countsKept reports the counts kept, which no field states, at pointer.
func countsKept(c lowering.Ctx, kept []string, pointer jsontext.Pointer) ir.Diagnostic {
	return c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"%s is past what a 64-bit integer holds, so no Constraints field states it; kept verbatim under Unmodeled",
		strings.Join(kept, ", "))
}

// bindingCount returns the index of the writer whose keyword, a count keptCount
// keeps, binds every other writer's: the smallest maximum or the largest
// minimum, compared as the integers they are (compareCounts), the first of one
// value written twice. It returns -1 where no writer writes one.
func bindingCount(writers []annotation.Voice, keyword string) int {
	best, sign := -1, 1
	if strings.HasPrefix(keyword, "max") {
		sign = -1
	}
	var bound string
	for w, writer := range writers {
		count, ok := keptLiteral(writer.Node, keyword)
		if ok && (best < 0 || sign*compareCounts(count, bound) > 0) {
			best, bound = w, count
		}
	}
	return best
}

// keptLiteral returns the decimal literal of the count keyword s writes, where
// keptCount keeps it: the value YAML resolved it to (value.NumericLiteral), or
// its text where that carries a power of ten no ir.BigVal holds.
func keptLiteral(s *oas3.Schema, keyword string) (string, bool) {
	if !keptCount(s, keyword, countOf(s, keyword)) {
		return "", false
	}
	node := nodeview.Deref(annotation.RawPropertyNode(s, keyword))
	if count, err := value.NumericLiteral(node); err == nil {
		return string(count), true
	}
	return strings.ReplaceAll(node.Value, "_", ""), true
}

// compareCounts returns -1, 0 or +1 as a, a positive integer literal, is less
// than, equal to or greater than b, exactly however large: by the power of ten
// each leading digit carries, then by their digits. An ir.Decimal holds no power
// past ir.MaxDecimalExp, which a count may be written past.
func compareCounts(a, b string) int {
	aPower, aDigits := magnitude(a)
	bPower, bDigits := magnitude(b)
	if order := aPower.Cmp(bPower); order != 0 {
		return order
	}
	return strings.Compare(aDigits, bDigits)
}

// magnitude returns the power of ten the leading digit of lit, a positive
// decimal literal, carries, and its significant digits, with no leading or
// trailing zero, so two spellings of one value read alike.
func magnitude(lit string) (*big.Int, string) {
	mantissa, exponent, _ := strings.Cut(strings.ToLower(lit), "e")
	whole, frac, _ := strings.Cut(strings.TrimPrefix(mantissa, "+"), ".")
	digits := strings.TrimLeft(whole+frac, "0")
	power, ok := new(big.Int).SetString(cmp.Or(exponent, "0"), 10)
	if !ok {
		// No count the check admits writes one, but a power read as none
		// orders it rather than failing.
		power = new(big.Int)
	}
	leading := len(whole+frac) - len(digits)
	power.Add(power, big.NewInt(int64(len(whole)-1-leading)))
	return power, strings.TrimRight(digits, "0")
}

// keptCount reports whether keyword, a count s writes, is one the lowering keeps
// verbatim rather than in Constraints: a non-negative integer, as the check
// reads one (oas.Natural), that the parser's model does not hold exactly, as it
// does not one past int64 (annotation.CountIsExact). Any other literal the model
// misreads is the load phase's to report, and reaches no field.
func keptCount(s *oas3.Schema, keyword string, model *int64) bool {
	node := annotation.RawPropertyNode(s, keyword)
	return node != nil && oas.Natural.Admits(node) && !annotation.CountIsExact(s, keyword, model)
}

// refusedCount reports whether keyword is a count s writes that is no
// non-negative integer, which the load phase reports and no reader holds.
func refusedCount(s *oas3.Schema, keyword string) bool {
	node := annotation.RawPropertyNode(s, keyword)
	return isCount(keyword) && node != nil && !oas.Natural.Admits(node)
}

// isCount reports whether keyword is one of the six counts.
func isCount(keyword string) bool {
	return slices.Contains(valueCountKeywords, keyword) || keyword == "maxItems" || keyword == "minItems"
}

// countOf returns the parser model's reading of keyword, one of the six counts,
// that s writes, and nil for no schema.
func countOf(s *oas3.Schema, keyword string) *int64 {
	if s == nil {
		return nil
	}
	switch keyword {
	case "minLength":
		return s.MinLength
	case "maxLength":
		return s.MaxLength
	case "minItems":
		return s.MinItems
	case "maxItems":
		return s.MaxItems
	case "minProperties":
		return s.MinProperties
	default:
		return s.MaxProperties
	}
}

// stampConstraintDiags gives every constraint diagnostic the provenance of the
// pointer that read the schema, which is what makes two reads of one sub-schema
// — its owning property and a $ref that hoists it — identical and so deduped by
// Diags.Append rather than reported twice.
func stampConstraintDiags(c lowering.Ctx, diags []ir.Diagnostic, pointer jsontext.Pointer) []ir.Diagnostic {
	for i := range diags {
		diags[i].Provenance = c.Place().At(pointer)
	}
	return diags
}

// CarrierConstraintsOf is ConstraintsOf for the carrier of a position of type t.
// A position owning its node leaves the collection bounds to it, one home per
// declaration, so the carrier reads the value bounds alone. One owning none holds
// those a reference does (referenceConstraintsOf), since the census hoists an
// alias to keep any other. PreserveRefSiteKeywords draws the same line.
func CarrierConstraintsOf(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *oas3.Schema, voices []annotation.Voice, t ir.TypeRef, pointer jsontext.Pointer) (*ir.Constraints, []ir.Diagnostic) {
	if LoweredToOwnNode(c, ts, pointer, t) {
		return ConstraintsOf(c, p, s, voices, pointer)
	}
	return referenceConstraintsOf(c, ts, p, s, voices, t, pointer)
}

// referenceConstraintsOf is ConstraintsOf for a position that lowered to target,
// a reference, a $ref site, a collapsed union, an expanded $dynamicRef or a
// refinement: no node of its own holds the collection bounds it wrote, so they
// join its value bounds on the alias or carrier it owns
// (docs/compilers/openapi.md#constraints), with those its voices write where
// the position holds them (homesAt).
func referenceConstraintsOf(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *oas3.Schema, voices []annotation.Voice, target ir.TypeRef, pointer jsontext.Pointer) (*ir.Constraints, []ir.Diagnostic) {
	cons, diags := ConstraintsOf(c, p, s, voices, pointer)
	if s == nil {
		return cons, diags
	}
	cons = merge.Constraints(cons, heldCollectionBounds(s))
	if len(voices) > 0 && homesAt(c, ts, s, target, pointer).collection {
		cons = voicedCollectionBounds(cons, voices)
	}
	return cons, diags
}

// voicedCollectionBounds is cons met with the collection bounds voices write,
// which agree where they speak (voicesAmong).
func voicedCollectionBounds(cons *ir.Constraints, voices []annotation.Voice) *ir.Constraints {
	for _, v := range voices {
		cons = merge.Constraints(cons, heldCollectionBounds(v.Node))
	}
	return cons
}

// heldCollectionBounds returns the collection bounds of s that a reference's
// constraints hold (referenceHolds), and nil where it writes none of them.
func heldCollectionBounds(s *oas3.Schema) *ir.Constraints {
	if !slices.ContainsFunc(collectionBoundKeywords, func(k string) bool { return referenceHolds(s, k) }) {
		return nil
	}
	cons := &ir.Constraints{UniqueItems: referenceHolds(s, "uniqueItems")}
	if referenceHolds(s, "minItems") {
		cons.MinItems = s.MinItems
	}
	if referenceHolds(s, "maxItems") {
		cons.MaxItems = s.MaxItems
	}
	return cons
}

// referenceHolds reports whether referenceConstraintsOf fills the collection
// bound keyword from s, which is the census's whole test at a reference, so a
// bound ir.Constraints cannot state stays verbatim: a count the parser misread
// (annotation.CountIsExact), and a false uniqueItems, which a plain bool cannot
// tell from unset and which would stop an alias being a decorator.
func referenceHolds(s *oas3.Schema, keyword string) bool {
	switch keyword {
	case "minItems":
		return annotation.CountIsExact(s, keyword, s.MinItems)
	case "maxItems":
		return annotation.CountIsExact(s, keyword, s.MaxItems)
	case "uniqueItems":
		return s.GetUniqueItems()
	default:
		return false
	}
}

// valueConstraintKeywords are the keywords annotation.Constraints reads into an
// ir.Constraints, sorted — which is both the order declaredConstraints walks
// them in and what makes the list comparable to the reader it must keep pace
// with.
//
// One list, read by the predicate that hoists a node for them
// (declaresValueConstraints) and by the recorder that keeps the ones no node can
// hold (declaredConstraints), so the two can never disagree about what a schema
// wrote. Collection bounds are absent because they belong to the collection
// nodes, or to what a reference owns, and are read by collectionConstraints or
// referenceConstraintsOf, not here.
var valueConstraintKeywords = []string{
	"exclusiveMaximum", "exclusiveMinimum", "maxLength", "maxProperties",
	"maximum", "minLength", "minProperties", "minimum", "multipleOf", "pattern",
}

// collectionBoundKeywords are the keywords collectionConstraints reads.
var collectionBoundKeywords = []string{"maxItems", "minItems", "uniqueItems"}

// declaresValueConstraints reports whether s sets any keyword
// annotation.Constraints reads. It does not call it: that reports a malformed
// bound, and a predicate must not emit diagnostics. The keywords are detected on
// their raw nodes for the same reason numericBounds reads them there — a
// magnitude beyond float64 leaves the model field nil while the keyword is
// plainly written.
func declaresValueConstraints(s *oas3.Schema) bool {
	return annotation.DeclaresAny(s, valueConstraintKeywords)
}

// declaredConstraints returns the value-constraint keywords s writes, in
// valueConstraintKeywords order.
func declaredConstraints(s *oas3.Schema) []string {
	out := make([]string, 0, len(valueConstraintKeywords))
	for _, keyword := range valueConstraintKeywords {
		if annotation.RawPropertyNode(s, keyword) != nil {
			out = append(out, keyword)
		}
	}
	return out
}
