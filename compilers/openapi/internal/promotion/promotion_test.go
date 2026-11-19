// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion_test

import (
	"encoding/json/jsontext"
	"go/types"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// mapping is a policy reading target from sources, in the order given.
func mapping(target promotion.ExtensionTarget, sources ...promotion.ExtensionSource) promotion.ExtensionPromotions {
	return promotion.ExtensionPromotions{
		Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{target: sources},
	}
}

// vendorExtension is one preserved x-* entry, as ExtensionsFrom writes it.
func vendorExtension(rawJSON string) ir.UnmodeledEntry {
	return ir.UnmodeledEntry{
		Reason:     ir.ReasonVendorExtension,
		Value:      ir.RawValue(rawJSON),
		Provenance: ir.Provenance{Pointer: "/components/schemas/S"},
	}
}

// TestPromoterDeprecation_FillsTheFieldsThePolicyNames pins what each mapping
// writes, one field at a time, because they share a struct and a promotion
// writing the wrong member of it would still look filled. The removal pair is
// why that matters most: a date written into the version field is the defect
// GitHub #417 records, and it reads as a filled Deprecation either way.
func TestPromoterDeprecation_FillsTheFieldsThePolicyNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target promotion.ExtensionTarget
		want   ir.Deprecation
	}{
		{"message", promotion.TargetDeprecationMessage, ir.Deprecation{Message: "why"}},
		{"since", promotion.TargetDeprecationSince, ir.Deprecation{Since: "why"}},
		{"removal version", promotion.TargetDeprecationRemovalVersion, ir.Deprecation{RemovalVersion: "why"}},
		{"removal date", promotion.TargetDeprecationRemovalDate, ir.Deprecation{RemovalDate: "why"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dep := new(ir.Deprecation)
			var prov ir.Provenance
			diags := promotion.New(mapping(tc.target, promotion.ExtensionSource{Key: "x-k"})).
				Deprecation(ir.Unmodeled{"openapi:x-k": vendorExtension(`"why"`)}, promotion.Referent{}, &dep, &prov)

			assert.Empty(t, diags)
			assert.Equal(t, tc.want, *dep)
			assert.Equal(t, promotion.ExtensionPromotionHeuristic, prov.Inferred)
		})
	}
}

// TestPromoterDeprecation_LeavesTheEntryItRead is the losslessness half: the
// promotion is a second reading of a preserved entry, never a move, so a
// consumer that disagrees with the guess can still read what the document
// actually wrote.
func TestPromoterDeprecation_LeavesTheEntryItRead(t *testing.T) {
	t.Parallel()
	unmodeled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`"why"`)}
	dep := new(ir.Deprecation)
	var prov ir.Provenance
	promotion.New(promotion.ExtensionPromotions{}).Deprecation(unmodeled, promotion.Referent{}, &dep, &prov)

	entry, kept := unmodeled["openapi:x-deprecated-reason"]
	require.True(t, kept, "the entry survives its own promotion")
	assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)
	assert.JSONEq(t, `"why"`, string(entry.Value))
}

// TestPromoterDeprecation_WritesNothing pins every shape that must leave the
// node exactly as it was. Each is a different reason, and a promotion that
// answered any of them by writing would put a value in the IR that the document
// did not say.
func TestPromoterDeprecation_WritesNothing(t *testing.T) {
	t.Parallel()
	filled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`"why"`)}
	tests := []struct {
		name      string
		policy    promotion.ExtensionPromotions
		unmodeled ir.Unmodeled
	}{
		{"promotion disabled", promotion.ExtensionPromotions{Disabled: true}, filled},
		{"no extensions kept", promotion.ExtensionPromotions{}, nil},
		{"a key the document did not write", promotion.ExtensionPromotions{}, ir.Unmodeled{
			"openapi:x-other": vendorExtension(`"why"`),
		}},
		{
			"a target no deprecation field answers to",
			mapping("pagination.items", promotion.ExtensionSource{Key: "x-deprecated-reason"}),
			filled,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dep := new(ir.Deprecation)
			var prov ir.Provenance
			diags := promotion.New(tc.policy).Deprecation(tc.unmodeled, promotion.Referent{}, &dep, &prov)
			assert.Empty(t, diags)
			assert.Equal(t, ir.Deprecation{}, *dep)
			assert.Empty(t, prov.Inferred, "nothing was inferred, so nothing is marked")
		})
	}
}

// TestPromoterDeprecation_UndeprecatedNodeIsTheWholeAnswer pins the nil cases,
// which are not an oversight to guard against but the ordinary shape: a node
// that never said it was deprecated has no Deprecation to fill, and its
// extension stays where it is.
func TestPromoterDeprecation_UndeprecatedNodeIsTheWholeAnswer(t *testing.T) {
	t.Parallel()
	c := promotion.New(promotion.ExtensionPromotions{})
	unmodeled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`"why"`)}
	assert.Empty(t, c.Deprecation(unmodeled, promotion.Referent{}, nil, &ir.Provenance{}))

	var undeprecated *ir.Deprecation
	var prov ir.Provenance
	assert.Empty(t, c.Deprecation(unmodeled, promotion.Referent{}, &undeprecated, &prov))
	assert.Nil(t, undeprecated, "a member states no deprecation")
	assert.Empty(t, prov.Inferred)

	dep := new(ir.Deprecation)
	assert.Empty(t, c.Deprecation(unmodeled, promotion.Referent{}, &dep, nil))
	assert.Equal(t, ir.Deprecation{}, *dep, "with nowhere to record the guess, none is made")
}

// TestPromoterDeprecation_ValueThatIsNotTextIsReported pins the one thing a
// promotion reports. Every Deprecation field is prose or a version, so a value
// of another shape means the document uses the key for something else — which
// is a reason to leave the field empty and say so, not to coerce.
func TestPromoterDeprecation_ValueThatIsNotTextIsReported(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, value string
	}{
		{"a number", `7`},
		// JSON null decodes into a plain string as "", so this row is what
		// separates a bare key from an empty string a document wrote on purpose:
		// the first is another shape and is reported, the second states nothing
		// (TestPromoterDeprecation_EmptyTextStatesNothing).
		{"a bare key", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			unmodeled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(tc.value)}
			dep := new(ir.Deprecation)
			var prov ir.Provenance
			diags := promotion.New(promotion.ExtensionPromotions{}).Deprecation(unmodeled, promotion.Referent{}, &dep, &prov)

			require.Len(t, diags, 1)
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
			assert.Equal(t, "openapi/degraded-construct", diags[0].Code)
			assert.Equal(t, jsontext.Pointer("/components/schemas/S"), diags[0].Provenance.Pointer,
				"the report names the extension rather than the node holding it")
			assert.Equal(t, ir.Deprecation{}, *dep)
			assert.Empty(t, prov.Inferred)
		})
	}
}

// TestPromoterDeprecation_EmptyTextStatesNothing pins what a field holding "" is:
// unset. So empty text writes nothing and leaves no marker saying it did, and it
// does not stand in front of a later source that has something to say.
func TestPromoterDeprecation_EmptyTextStatesNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		unmodeled ir.Unmodeled
		want      ir.Deprecation
		marked    bool
	}{
		{"alone", ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`""`)}, ir.Deprecation{}, false},
		{"before a source with text", ir.Unmodeled{
			"openapi:x-deprecated-reason":             vendorExtension(`""`),
			"openapi:x-stainless-deprecation-message": vendorExtension(`"why"`),
		}, ir.Deprecation{Message: "why"}, true},
		{"after one", ir.Unmodeled{
			"openapi:x-deprecated-reason":             vendorExtension(`"why"`),
			"openapi:x-stainless-deprecation-message": vendorExtension(`""`),
		}, ir.Deprecation{Message: "why"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dep := new(ir.Deprecation)
			var prov ir.Provenance
			diags := promotion.New(promotion.ExtensionPromotions{}).Deprecation(tc.unmodeled, promotion.Referent{}, &dep, &prov)

			assert.Empty(t, diags)
			assert.Equal(t, tc.want, *dep)
			assert.Equal(t, tc.marked, prov.Inferred == promotion.ExtensionPromotionHeuristic)
		})
	}
}

// TestPromoterDeprecation_MarksOnceBesideWhateverWasAlreadyThere pins the two
// halves of the marker. A heuristic already recorded must survive, since
// Provenance.Inferred holds one string; and a node annotated twice — which a
// component reached by two references is — must not accumulate the same name
// per reference.
func TestPromoterDeprecation_MarksOnceBesideWhateverWasAlreadyThere(t *testing.T) {
	t.Parallel()
	c := promotion.New(promotion.ExtensionPromotions{})
	unmodeled := ir.Unmodeled{"openapi:x-deprecated-reason": vendorExtension(`"why"`)}
	dep := new(ir.Deprecation)
	prov := ir.Provenance{Inferred: "group-path-prefix"}

	c.Deprecation(unmodeled, promotion.Referent{}, &dep, &prov)
	assert.Equal(t, ir.Inferred("group-path-prefix,extension-promotion"), prov.Inferred)

	c.Deprecation(unmodeled, promotion.Referent{}, &dep, &prov)
	assert.Equal(t, ir.Inferred("group-path-prefix,extension-promotion"), prov.Inferred,
		"a second reading of the same node adds no second marker")
}

// TestDefaultExtensionPromotions_IsCopiedNotShared holds the exported default
// to being a fresh map. A caller who starts from it and edits it must not be
// editing what the next compile reads.
func TestDefaultExtensionPromotions_IsCopiedNotShared(t *testing.T) {
	t.Parallel()
	first := promotion.DefaultExtensionPromotions()
	require.NotEmpty(t, first, "an empty mapping would make this vacuous")
	first[promotion.TargetDeprecationMessage][0].Key = "x-edited"
	delete(first, promotion.TargetDeprecationSince)
	again := promotion.DefaultExtensionPromotions()
	assert.Contains(t, again, promotion.TargetDeprecationSince)
	assert.Equal(t, "x-deprecated-reason", again[promotion.TargetDeprecationMessage][0].Key)
}

// TestPromoter_PolicyIsCopiedIntoThePromoter pins the same rule for the caller's
// own policy, down to its lists: a promoter is passed by value, so a map or a
// slice shared rather than copied would be the part of it a caller could write
// through after the fact. maps.Clone is not enough here, as it leaves each list
// shared, which the edit of the list's own element is what reddens.
func TestPromoter_PolicyIsCopiedIntoThePromoter(t *testing.T) {
	t.Parallel()
	sources := []promotion.ExtensionSource{{Key: "x-k"}}
	targets := map[promotion.ExtensionTarget][]promotion.ExtensionSource{promotion.TargetDeprecationMessage: sources}
	c := promotion.New(promotion.ExtensionPromotions{Targets: targets})

	sources[0].Key = "x-edited"
	delete(targets, promotion.TargetDeprecationMessage)

	dep := new(ir.Deprecation)
	var prov ir.Provenance
	c.Deprecation(ir.Unmodeled{"openapi:x-k": vendorExtension(`"why"`)}, promotion.Referent{}, &dep, &prov)
	assert.Equal(t, "why", dep.Message, "the policy the promoter read is the one it was given")
}

// TestPromoter_ReadsSourcesInPolicyOrder is the fix for two keys resolving by
// how they sort: the list states precedence, the first stating a value fills
// the field, and the loser is reported at its own key rather than dropped in
// silence (GitHub #655). Both orders run, because a promoter sorting its keys
// passes either one alone.
func TestPromoter_ReadsSourcesInPolicyOrder(t *testing.T) {
	t.Parallel()
	alpha, zulu := promotion.ExtensionSource{Key: "x-alpha-msg"}, promotion.ExtensionSource{Key: "x-zulu-msg"}
	for _, tc := range []struct {
		name    string
		order   []promotion.ExtensionSource
		want    string
		loserAt jsontext.Pointer
	}{
		{"alpha first", []promotion.ExtensionSource{alpha, zulu}, "FROM-ALPHA", "/x-zulu-msg"},
		{"zulu first", []promotion.ExtensionSource{zulu, alpha}, "FROM-ZULU", "/x-alpha-msg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dep := new(ir.Deprecation)
			var prov ir.Provenance
			diags := promotion.New(mapping(promotion.TargetDeprecationMessage, tc.order...)).Deprecation(ir.Unmodeled{
				"openapi:x-alpha-msg": {Value: ir.RawValue(`"FROM-ALPHA"`), Provenance: ir.Provenance{Pointer: "/x-alpha-msg"}},
				"openapi:x-zulu-msg":  {Value: ir.RawValue(`"FROM-ZULU"`), Provenance: ir.Provenance{Pointer: "/x-zulu-msg"}},
			}, promotion.Referent{}, &dep, &prov)

			assert.Equal(t, tc.want, dep.Message)
			require.Len(t, diags, 1, "the loser is reported")
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
			assert.Equal(t, "openapi/degraded-construct", diags[0].Code)
			assert.Equal(t, tc.loserAt, diags[0].Provenance.Pointer)
			assert.Contains(t, diags[0].Message, tc.order[1].Key)
			assert.Contains(t, diags[0].Message, tc.order[0].Key, "and names the key it lost to")
		})
	}
}

// TestPromoter_SourcesThatAgreeReportNothing pins the other side of the report:
// a later source stating the same value lost nothing, and one stating nothing
// has no value to lose, so neither is a disagreement.
func TestPromoter_SourcesThatAgreeReportNothing(t *testing.T) {
	t.Parallel()
	policy := mapping(promotion.TargetDeprecationMessage,
		promotion.ExtensionSource{Key: "x-a"}, promotion.ExtensionSource{Key: "x-b"}, promotion.ExtensionSource{Key: "x-c"})
	dep := new(ir.Deprecation)
	var prov ir.Provenance
	diags := promotion.New(policy).Deprecation(ir.Unmodeled{
		"openapi:x-b": vendorExtension(`"same"`),
		"openapi:x-c": vendorExtension(`"same"`),
	}, promotion.Referent{}, &dep, &prov)
	assert.Empty(t, diags)
	assert.Equal(t, "same", dep.Message, "a source the document does not write is passed over")
}

// TestPromoter_MemberReadsOneFactOfAnObject pins what a Member adds: one key
// holding several facts fills several fields, and how a value misfits is said
// of the member or of the key, whichever is wrong.
func TestPromoter_MemberReadsOneFactOfAnObject(t *testing.T) {
	t.Parallel()
	policy := promotion.ExtensionPromotions{Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{
		promotion.TargetDeprecationMessage:     {{Key: "x-k", Member: "text"}},
		promotion.TargetDeprecationRemovalDate: {{Key: "x-k", Member: "date"}},
	}}
	for _, tc := range []struct {
		name  string
		value string
		want  ir.Deprecation
		at    []jsontext.Pointer
	}{
		{"both members", `{"text":"why","date":"2030-01-01"}`, ir.Deprecation{Message: "why", RemovalDate: "2030-01-01"}, nil},
		{"an absent member states nothing", `{"text":"why"}`, ir.Deprecation{Message: "why"}, nil},
		{"no member at all", `{"other":1}`, ir.Deprecation{}, nil},
		{"a member of another shape", `{"text":"why","date":7}`, ir.Deprecation{Message: "why"}, []jsontext.Pointer{"/k/date"}},
		{"text where an object belongs", `"why"`, ir.Deprecation{}, []jsontext.Pointer{"/k", "/k"}},
		{"a bare key", `null`, ir.Deprecation{}, []jsontext.Pointer{"/k", "/k"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dep := new(ir.Deprecation)
			var prov ir.Provenance
			diags := promotion.New(policy).Deprecation(ir.Unmodeled{
				"openapi:x-k": {Value: ir.RawValue(tc.value), Provenance: ir.Provenance{Pointer: "/k"}},
			}, promotion.Referent{}, &dep, &prov)

			assert.Equal(t, tc.want, *dep)
			at := make([]jsontext.Pointer, 0, len(diags))
			for _, d := range diags {
				at = append(at, d.Provenance.Pointer)
			}
			assert.ElementsMatch(t, tc.at, at, "each report names the key or member at fault")
			if cmp.Equal(tc.want, ir.Deprecation{}) {
				assert.Empty(t, prov.Inferred, "nothing was read, so nothing is marked")
			}
		})
	}
}

// TestPromoter_NotAnObjectReportsOnceAcrossItsMembers is why that report names
// no member: both sources read one key, and the document has one defect, so the
// two reports are one diagnostic for compile.Diags to collapse.
func TestPromoter_NotAnObjectReportsOnceAcrossItsMembers(t *testing.T) {
	t.Parallel()
	policy := promotion.ExtensionPromotions{Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{
		promotion.TargetDeprecationMessage:     {{Key: "x-k", Member: "text"}},
		promotion.TargetDeprecationRemovalDate: {{Key: "x-k", Member: "date"}},
	}}
	dep := new(ir.Deprecation)
	var prov ir.Provenance
	diags := promotion.New(policy).Deprecation(ir.Unmodeled{"openapi:x-k": vendorExtension(`"why"`)}, promotion.Referent{}, &dep, &prov)
	require.Len(t, diags, 2)
	assert.Equal(t, diags[0], diags[1], "identical, so the compile's list holds one")
}

// TestExtensionSource_StringNamesWhatAReportNames pins the two spellings a
// report uses.
func TestExtensionSource_StringNamesWhatAReportNames(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "x-k", promotion.ExtensionSource{Key: "x-k"}.String())
	assert.Equal(t, "x-k.date", promotion.ExtensionSource{Key: "x-k", Member: "date"}.String())
}

// promotionPackage is the promotion package type-checked, loaded once per test
// binary.
var promotionPackage = sync.OnceValues(func() (*decls.Package, error) {
	return decls.Load("github.com/dexpace/morphic/compilers/openapi/internal/promotion")
})

// declaredTargets returns every ExtensionTarget constant the package declares,
// read type-checked rather than matched: a regex over one file misses a target
// declared in another file of the package, or spelled with different spacing,
// and misses it silently — which is the failure this check exists to prevent,
// reproduced in the check itself. A declaration typed, converted, aliased or
// computed is read the way the compiler reads it.
//
// A package-level var of the type fails: it is assignable wherever a target is
// but holds no value this check can read, so it would escape the applier check.
func declaredTargets(t *testing.T) []promotion.ExtensionTarget {
	t.Helper()
	p, err := promotionPackage()
	require.NoError(t, err)
	named, err := p.Named("ExtensionTarget")
	require.NoError(t, err)

	for v := range p.Objects[*types.Var]() {
		assert.False(t, types.Identical(v.Type(), named),
			"%s is a var of ExtensionTarget; a target must be a constant to be read here", v.Name())
	}

	consts, err := p.StringConsts("ExtensionTarget")
	require.NoError(t, err)
	require.NotEmpty(t, consts, "no targets found; the check below would pass vacuously")

	out := make([]promotion.ExtensionTarget, len(consts))
	for i, c := range consts {
		out[i] = promotion.ExtensionTarget(c.Value)
	}
	return out
}

// statingValue returns the value of "x-k" that states target's fact: a target
// reading a closed set of words or a boolean takes one of those, and the rest
// take any text. A target new to the vocabulary starts with text, and fails
// below until this says what states it.
func statingValue(target promotion.ExtensionTarget) string {
	switch target {
	case promotion.TargetDeprecation:
		return `"deprecated"`
	case promotion.TargetStability:
		return `"beta"`
	case promotion.TargetRedaction, promotion.TargetAllowReserved:
		return `true`
	default:
		return `"v"`
	}
}

// appliers is one closure per method of Promoter. Each runs p's policy against a
// carrier of its own holding unmodeled, the key "x-k" every caller below maps,
// and reports whether that carrier changed. An undeprecated carrier reads a key
// stating a deprecation, and a deprecated one a member of it, so the first two
// together answer for the five targets the method holds.
//
// The census is hand-written, so a method this list does not know about is the
// same gap one level down: the check beneath it would then declare a target
// unapplied that a real lowering does apply.
func appliers() []func(promotion.Promoter, ir.Unmodeled) (bool, []ir.Diagnostic) {
	return []func(promotion.Promoter, ir.Unmodeled) (bool, []ir.Diagnostic){
		// A stream's terminator and condition are filled once the document is
		// whole (stream.PromoteDetail), from the text their sources state: what is
		// read here is that text.
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			var stated bool
			var diags []ir.Diagnostic
			for _, target := range []promotion.ExtensionTarget{promotion.TargetStreamTerminator, promotion.TargetStreamCondition} {
				st, readDiags := p.TextOf(target, unmodeled)
				stated, diags = stated || st.Text != "", append(diags, readDiags...)
			}
			return stated, diags
		},
		// The choices among an operation's parameters are read once the
		// document is whole (paramchoice.PromoteChoices), from the value read here.
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			_, written, diags := p.StatementOf(promotion.TargetParamChoices, unmodeled)
			return written, diags
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			var dep *ir.Deprecation
			var prov ir.Provenance
			diags := p.Deprecation(unmodeled, promotion.Referent{}, &dep, &prov)
			return dep != nil, diags
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			dep := new(ir.Deprecation)
			var prov ir.Provenance
			diags := p.Deprecation(unmodeled, promotion.Referent{}, &dep, &prov)
			return !cmp.Equal(*dep, ir.Deprecation{}), diags
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			enum := ir.Enum{Closed: true}
			var prov ir.Provenance
			diags := promotion.EnumOpenness(p, unmodeled, &enum.Closed, &prov)
			return !enum.Closed, diags
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			u := ir.Union{Closed: true}
			var prov ir.Provenance
			diags := promotion.UnionOpenness(p, unmodeled, &u.Closed, &prov)
			return !u.Closed, diags
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			var tier ir.Stability
			var prov ir.Provenance
			diags := promotion.Stability(p, unmodeled, &tier, &prov)
			return tier != ir.StabilityUnstated, diags
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			names, _, diags := p.Replacements(unmodeled)
			return len(names) > 0, diags
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			var redact bool
			var prov ir.Provenance
			diags := p.Redaction(unmodeled, promotion.Referent{}, &redact, &prov)
			return redact, diags
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			name, diags := promotion.ServerName(p, unmodeled, &ir.Provenance{})
			return name != "", diags
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			return promotion.AllowReserved(p, unmodeled, &ir.Provenance{})
		},
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			var inferred ir.Inferred
			methods, diags := promotion.TokenEndpointAuthMethods(p, unmodeled, &inferred)
			return len(methods) > 0, diags
		},
		// A pagination is filled once its document is whole, from what each of
		// its targets' sources says (pagination.Promote): what is read
		// here is that statement.
		func(p promotion.Promoter, unmodeled ir.Unmodeled) (bool, []ir.Diagnostic) {
			var stated bool
			var diags []ir.Diagnostic
			for _, target := range promotion.Targets() {
				if !strings.HasPrefix(string(target), "pagination.") {
					continue
				}
				for _, src := range p.Sources(target) {
					st, readDiags := promotion.PageStatementOf(p, target, src, unmodeled, nil)
					stated, diags = stated || st.Stated, append(diags, readDiags...)
				}
			}
			return stated, diags
		},
	}
}

// TestExtensionTarget_EveryDeclaredTargetHasAnApplier holds the vocabulary to
// the appliers, the half of "a target is a constant and an applier" that
// nothing else checks: a constant alone compiles, maps cleanly, and promotes
// nothing, with no field written, no diagnostic and no marker.
//
// Every follow-up target (Pagination, Idempotency) arrives as a constant first,
// so this fails on purpose until the new target gets its applier and appliers()
// learns which method answers for it.
func TestExtensionTarget_EveryDeclaredTargetHasAnApplier(t *testing.T) {
	t.Parallel()
	for _, target := range declaredTargets(t) {
		p := promotion.New(mapping(target, promotion.ExtensionSource{Key: "x-k"}))
		unmodeled := ir.Unmodeled{"openapi:x-k": vendorExtension(statingValue(target))}
		var applied bool
		for _, apply := range appliers() {
			wrote, diags := apply(p, unmodeled)
			assert.Empty(t, diags, "%s: a declared target reports nothing when it is applied", target)
			applied = applied || wrote
		}
		assert.True(t, applied,
			"%s is declared in the vocabulary but no applier fills it, so a policy naming it "+
				"promotes nothing and says nothing", target)
	}
}

// targetFields names every field each target fills, as the table in
// promotionDoc spells them: a target writing a field at several carriers names
// each, so a carrier gained without a row fails here.
var targetFields = map[promotion.ExtensionTarget][]string{
	promotion.TargetDeprecation: {
		"Operation.Deprecation", "Parameter.Deprecation", "Property.Deprecation",
		"AuthScheme.Deprecation", "TypeCommon.Deprecation",
	},
	promotion.TargetDeprecationMessage:        {"Deprecation.Message"},
	promotion.TargetDeprecationSince:          {"Deprecation.Since"},
	promotion.TargetDeprecationRemovalVersion: {"Deprecation.RemovalVersion"},
	promotion.TargetDeprecationRemovalDate:    {"Deprecation.RemovalDate"},
	promotion.TargetDeprecationReplacedBy:     {"Deprecation.ReplacedBy"},
	promotion.TargetEnumOpen:                  {"Enum.Closed"},
	promotion.TargetUnionOpen:                 {"Union.Closed"},
	promotion.TargetStability:                 {"Operation.Stability"},
	promotion.TargetRedaction:                 {"TypeCommon.Sensitive", "Property.Secret", "Parameter.Secret"},
	promotion.TargetServerName:                {"Server.Name"},
	promotion.TargetAllowReserved:             {"HTTPParamBinding.AllowReserved"},
	promotion.TargetTokenEndpointAuthMethods:  {"OAuthFlow.TokenEndpointAuthMethods"},
	promotion.TargetPaginationCursor:          {"Operation.Pagination"},
	promotion.TargetPaginationOffset:          {"Operation.Pagination"},
	promotion.TargetPaginationPage:            {"Operation.Pagination"},
	promotion.TargetPaginationLimit:           {"Operation.Pagination"},
	promotion.TargetPaginationItems:           {"Operation.Pagination"},
	promotion.TargetPaginationNextCursor:      {"Operation.Pagination"},
	promotion.TargetPaginationNextLink:        {"Operation.Pagination"},
	promotion.TargetPaginationHasMore:         {"Operation.Pagination"},
	promotion.TargetPaginationTotalCount:      {"Operation.Pagination"},
	promotion.TargetPaginationTotalPages:      {"Operation.Pagination"},
	promotion.TargetStreamTerminator:          {"StreamDetail.Terminator"},
	promotion.TargetStreamCondition:           {"StreamDetail.Condition"},
	promotion.TargetParamChoices:              {"Operation.ParamChoices"},
}

// promotionDoc is the document whose table says what a promotion can fill.
const promotionDoc = "../../../../docs/ir/unmodeled.md"

// promotionTableFields returns the first column of the table in doc: the cells
// alone, so a field the prose around the table names does not count as a row.
func promotionTableFields(t *testing.T, doc string) string {
	t.Helper()
	_, section, found := strings.Cut(doc, "### What a promotion can fill\n")
	require.True(t, found, "%s has no table of what a promotion can fill", promotionDoc)
	section, _, _ = strings.Cut(section, "\n#")

	var fields strings.Builder
	for line := range strings.Lines(section) {
		if cells := strings.Split(line, "|"); len(cells) == 5 {
			fields.WriteString(cells[1])
		}
	}
	return fields.String()
}

// TestExtensionTarget_EveryTargetIsInThePromotionTable holds the vocabulary to
// the table, the direction ir's TestPromotionTable_EveryNodeCanRecord does not
// read: that test holds each row to the structs, so a target with no row would
// promote a fact nothing says which node records. A list of targets kept apart
// from the code is how the table's predecessor drifted (GitHub #674).
func TestExtensionTarget_EveryTargetIsInThePromotionTable(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile(promotionDoc)
	require.NoError(t, err)
	fields := promotionTableFields(t, string(doc))

	for _, target := range declaredTargets(t) {
		filled, named := targetFields[target]
		if !assert.True(t, named, "%s fills a field targetFields does not name", target) {
			continue
		}
		for _, field := range filled {
			assert.Contains(t, fields, "`"+field+"`",
				"%s fills a field %s lists no row for, so nothing says which node records it", target, promotionDoc)
		}
	}
}

// TestEnumOpenness_ClearsClosedAndMarksTheNode pins the promotion the
// vocabulary's one non-Deprecation target performs. Every enum the compiler
// builds is closed, so the write here is the whole of what x-extensible-enum
// buys a consumer, and the marker is what says a heuristic made it.
func TestEnumOpenness_ClearsClosedAndMarksTheNode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
	}{
		{"the member list the convention writes", `["a","b"]`},
		{"an explicit true", `true`},
		{"a value that states nothing", `"whatever"`},
		// JSON null decodes into a plain bool as false, so this row is what
		// separates the presence-only spelling from an explicit decline.
		{"a bare key, which is the presence-only spelling", `null`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			enum := ir.Enum{Closed: true}
			var prov ir.Provenance
			promotion.EnumOpenness(promotion.New(promotion.ExtensionPromotions{}),
				ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(tc.value)}, &enum.Closed, &prov)

			assert.False(t, enum.Closed, "the key says the member set is open")
			assert.Equal(t, promotion.ExtensionPromotionHeuristic, prov.Inferred)
		})
	}
}

// TestEnumOpenness_MarksOnceBesideWhateverWasAlreadyThere is the marker
// pin its deprecation twin makes, at this carrier: a name already in the list
// survives the promotion, and a second reading of the node adds none.
func TestEnumOpenness_MarksOnceBesideWhateverWasAlreadyThere(t *testing.T) {
	t.Parallel()
	c := promotion.New(promotion.ExtensionPromotions{})
	unmodeled := ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(`true`)}
	enum := ir.Enum{Closed: true}
	prov := ir.Provenance{Inferred: "an-earlier-heuristic"}

	promotion.EnumOpenness(c, unmodeled, &enum.Closed, &prov)
	assert.Equal(t, ir.Inferred("an-earlier-heuristic,extension-promotion"), prov.Inferred)

	promotion.EnumOpenness(c, unmodeled, &enum.Closed, &prov)
	assert.Equal(t, ir.Inferred("an-earlier-heuristic,extension-promotion"), prov.Inferred,
		"a second reading of the same node adds no second marker")
}

// TestEnumOpenness_LeavesTheEntryItRead is the losslessness half, for
// the same reason its deprecation twin is: the promotion is a second reading of
// a preserved entry, so a consumer that disagrees still has what was written —
// which for this key is the member list itself.
func TestEnumOpenness_LeavesTheEntryItRead(t *testing.T) {
	t.Parallel()
	unmodeled := ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(`["a","b"]`)}
	promotion.EnumOpenness(promotion.New(promotion.ExtensionPromotions{}), unmodeled, new(true), &ir.Provenance{})

	entry, kept := unmodeled["openapi:x-extensible-enum"]
	require.True(t, kept, "the entry survives its own promotion")
	assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)
	assert.JSONEq(t, `["a","b"]`, string(entry.Value))
}

// TestEnumOpenness_WritesNothing pins every shape that must leave the
// enum closed. The last row is the one that is not an absence: a document
// writing the key with a boolean false says the set is *not* extensible, and
// reading presence alone there would record the opposite of what it said.
func TestEnumOpenness_WritesNothing(t *testing.T) {
	t.Parallel()
	filled := ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(`["a","b"]`)}
	tests := []struct {
		name      string
		policy    promotion.ExtensionPromotions
		unmodeled ir.Unmodeled
	}{
		{"promotion disabled", promotion.ExtensionPromotions{Disabled: true}, filled},
		{"no extensions kept", promotion.ExtensionPromotions{}, nil},
		{"a key the document did not write", promotion.ExtensionPromotions{}, ir.Unmodeled{
			"openapi:x-other": vendorExtension(`["a","b"]`),
		}},
		{
			"a key mapped to another target",
			mapping(promotion.TargetDeprecationMessage, promotion.ExtensionSource{Key: "x-extensible-enum"}),
			filled,
		},
		{
			"an explicit false",
			promotion.ExtensionPromotions{},
			ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(`false`)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			enum := ir.Enum{Closed: true}
			var prov ir.Provenance
			promotion.EnumOpenness(promotion.New(tc.policy), tc.unmodeled, &enum.Closed, &prov)

			assert.True(t, enum.Closed, "the enum stays as the format declared it")
			assert.Empty(t, prov.Inferred, "nothing was inferred, so nothing is marked")
		})
	}
}

// TestEnumOpenness_NonEnumCarrierIsTheWholeAnswer pins the nil cases,
// which are the ordinary shape rather than a guard: most nodes an x-* key can
// sit on are not enums, and a node with nowhere to record the guess is not
// promoted from (promotion rule 4).
func TestEnumOpenness_NonEnumCarrierIsTheWholeAnswer(t *testing.T) {
	t.Parallel()
	c := promotion.New(promotion.ExtensionPromotions{})
	unmodeled := ir.Unmodeled{"openapi:x-extensible-enum": vendorExtension(`["a","b"]`)}
	promotion.EnumOpenness(c, unmodeled, nil, &ir.Provenance{})

	enum := ir.Enum{Closed: true}
	promotion.EnumOpenness(c, unmodeled, &enum.Closed, nil)
	assert.True(t, enum.Closed, "with nowhere to record the guess, none is made")
}

// TestEnumOpenness_FirstStatedDecides holds the enum to the order every
// other target reads in: the explicit false a policy reads first declines, and
// a later key stating the set open loses and is reported.
func TestEnumOpenness_FirstStatedDecides(t *testing.T) {
	t.Parallel()
	enum := ir.Enum{Closed: true}
	var prov ir.Provenance
	diags := promotion.EnumOpenness(promotion.New(mapping(promotion.TargetEnumOpen,
		promotion.ExtensionSource{Key: "x-a"}, promotion.ExtensionSource{Key: "x-b"})), ir.Unmodeled{
		"openapi:x-a": vendorExtension(`false`),
		"openapi:x-b": vendorExtension(`["a"]`),
	}, &enum.Closed, &prov)

	assert.True(t, enum.Closed)
	assert.Empty(t, prov.Inferred)
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, "x-b")
}

// TestPromoterDeprecation_KeyStatesIt pins the one target that creates what the
// others fill. Text and a boolean are the two shapes such a key takes, a key
// holding other words states nothing with them, and any other shape is reported
// because the document means something else by the key.
func TestPromoterDeprecation_KeyStatesIt(t *testing.T) {
	t.Parallel()
	policy := mapping(promotion.TargetDeprecation, promotion.ExtensionSource{Key: "x-k"})
	for _, tc := range []struct {
		name, value string
		deprecated  bool
		reported    bool
	}{
		{"the word", `"deprecated"`, true, false},
		{"a boolean true", `true`, true, false},
		{"a boolean false", `false`, false, false},
		{"another word", `"beta"`, false, false},
		{"a re-cased word", `"Deprecated"`, false, false},
		{"a number", `3`, false, true},
		{"an object", `{"deprecated":true}`, false, true},
		{"a bare key", `null`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var dep *ir.Deprecation
			var prov ir.Provenance
			diags := promotion.New(policy).Deprecation(ir.Unmodeled{"openapi:x-k": vendorExtension(tc.value)}, promotion.Referent{}, &dep, &prov)

			assert.Equal(t, tc.deprecated, dep != nil)
			if tc.deprecated {
				assert.Equal(t, promotion.ExtensionPromotionHeuristic, prov.Inferred)
			} else {
				assert.Empty(t, prov.Inferred)
			}
			assert.Equal(t, tc.reported, len(diags) == 1, "reports: %+v", diags)
		})
	}
}

// TestPromoterDeprecation_KeyLeavesADeclaredDeprecationUnmarked pins what a key
// beside the keyword adds: nothing. The Deprecation stays the keyword's, and a
// node the promotion wrote nothing into carries no marker, which is what tells
// an operation writing both from one the key alone deprecates.
func TestPromoterDeprecation_KeyLeavesADeclaredDeprecationUnmarked(t *testing.T) {
	t.Parallel()
	declared := new(ir.Deprecation)
	dep := declared
	var prov ir.Provenance
	diags := promotion.New(promotion.ExtensionPromotions{}).Deprecation(
		ir.Unmodeled{"openapi:x-fern-availability": vendorExtension(`"deprecated"`)}, promotion.Referent{}, &dep, &prov)

	assert.Empty(t, diags)
	assert.Same(t, declared, dep)
	assert.Empty(t, prov.Inferred)
}

// TestPromoterDeprecation_KeyGivesTheMembersSomethingToFill pins the order of the
// two readings: the deprecation a key creates is what the members then fill, so
// a member read first would find nothing and be dropped.
func TestPromoterDeprecation_KeyGivesTheMembersSomethingToFill(t *testing.T) {
	t.Parallel()
	var dep *ir.Deprecation
	var prov ir.Provenance
	diags := promotion.New(promotion.ExtensionPromotions{}).Deprecation(ir.Unmodeled{
		"openapi:x-fern-availability": vendorExtension(`"deprecated"`),
		"openapi:x-sunset":            vendorExtension(`"2030-01-01"`),
	}, promotion.Referent{}, &dep, &prov)

	assert.Empty(t, diags)
	require.NotNil(t, dep)
	assert.Equal(t, "2030-01-01", dep.RemovalDate)
	assert.Equal(t, promotion.ExtensionPromotionHeuristic, prov.Inferred)
}

// TestStability_ReadsEachWord is the vocabulary, one row a word, under a
// key only this target reads. Every word the IR does not hold is reported there,
// since nothing else takes it up.
func TestStability_ReadsEachWord(t *testing.T) {
	t.Parallel()
	policy := mapping(promotion.TargetStability, promotion.ExtensionSource{Key: "x-k"})
	for _, tc := range []struct {
		value string
		want  ir.Stability
	}{
		{`"alpha"`, ir.StabilityAlpha},
		{`"beta"`, ir.StabilityBeta},
		{`"preview"`, ir.StabilityBeta},
		{`"stable"`, ir.StabilityStable},
		{`"generally-available"`, ir.StabilityStable},
		{`"legacy"`, ir.StabilityUnstated},
		{`"deprecated"`, ir.StabilityUnstated},
		{`"Beta"`, ir.StabilityUnstated},
		{`""`, ir.StabilityUnstated},
		{`3`, ir.StabilityUnstated},
		{`true`, ir.StabilityUnstated},
		{`null`, ir.StabilityUnstated},
		{`{"tier":"beta"}`, ir.StabilityUnstated},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			var tier ir.Stability
			var prov ir.Provenance
			diags := promotion.Stability(promotion.New(policy), ir.Unmodeled{"openapi:x-k": vendorExtension(tc.value)}, &tier, &prov)

			assert.Equal(t, tc.want, tier)
			if tc.want == ir.StabilityUnstated {
				require.Len(t, diags, 1, "a value naming no tier, on a key nothing else reads, is reported")
				assert.Equal(t, jsontext.Pointer("/components/schemas/S"), diags[0].Provenance.Pointer)
				assert.Empty(t, prov.Inferred)
				return
			}
			assert.Empty(t, diags)
			assert.Equal(t, promotion.ExtensionPromotionHeuristic, prov.Inferred)
		})
	}
}

// TestStability_TheOrderDecidesAndNotTheTier pins what settles a
// disagreement of the default policy. On the golden spec the key read first is
// always the less stable, and the rule is not that: where it is the more stable
// it still fills the field, and the other key is what is reported.
func TestStability_TheOrderDecidesAndNotTheTier(t *testing.T) {
	t.Parallel()
	var tier ir.Stability
	var prov ir.Provenance
	diags := promotion.Stability(promotion.New(promotion.ExtensionPromotions{}), ir.Unmodeled{
		"openapi:x-stability":         vendorExtension(`"stable"`),
		"openapi:x-fern-availability": vendorExtension(`"alpha"`),
	}, &tier, &prov)

	assert.Equal(t, ir.StabilityStable, tier)
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, `"x-fern-availability" states another`)
}

// TestPromoter_OneKeyTwoTargetsReportsOnce runs the two readings an operation
// makes of a key the default policy maps to both, and holds each value to one
// report across them: a value both would refuse is one defect, and the word
// `deprecated` was read, so it is no defect at all. A true is no word the
// default lists for a deprecation, so it states none, and names no tier.
func TestPromoter_OneKeyTwoTargetsReportsOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value      string
		deprecated bool
		reports    int
	}{
		{`"deprecated"`, true, 0},
		{`true`, false, 1},
		{`false`, false, 1},
		{`"legacy"`, false, 1},
		{`3`, false, 1},
		{`null`, false, 1},
		{`["beta"]`, false, 1},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			p := promotion.New(promotion.ExtensionPromotions{})
			unmodeled := ir.Unmodeled{"openapi:x-stability": vendorExtension(tc.value)}
			var dep *ir.Deprecation
			var tier ir.Stability
			var prov ir.Provenance
			diags := append(p.Deprecation(unmodeled, promotion.Referent{}, &dep, &prov), promotion.Stability(p, unmodeled, &tier, &prov)...)

			assert.Equal(t, tc.deprecated, dep != nil)
			assert.Equal(t, ir.StabilityUnstated, tier)
			assert.Len(t, diags, tc.reports, "reports: %+v", diags)
		})
	}
}

// TestPromoterRedaction_OnlyTrueWrites pins the reading of a boolean key. A false
// states nothing and is no defect, and any other shape is reported, since a
// redaction read from a value that does not say so would hide what was not
// asked to be hidden.
func TestPromoterRedaction_OnlyTrueWrites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value    string
		redacted bool
		reported bool
	}{
		{`true`, true, false},
		{`false`, false, false},
		{`"yes"`, false, true},
		{`1`, false, true},
		{`null`, false, true},
		{`{}`, false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			var redact bool
			var prov ir.Provenance
			diags := promotion.New(promotion.ExtensionPromotions{}).Redaction(
				ir.Unmodeled{"openapi:x-sensitive": vendorExtension(tc.value)}, promotion.Referent{}, &redact, &prov)

			assert.Equal(t, tc.redacted, redact)
			assert.Equal(t, tc.redacted, prov.Inferred == promotion.ExtensionPromotionHeuristic)
			assert.Equal(t, tc.reported, len(diags) == 1, "reports: %+v", diags)
		})
	}
}

// TestPromoterRedaction_LeavesADeclaredRedactionUnmarked pins the marker to what
// the promotion wrote: a value `format: password` already redacts takes no guess
// from a key beside it.
func TestPromoterRedaction_LeavesADeclaredRedactionUnmarked(t *testing.T) {
	t.Parallel()
	redact := true
	var prov ir.Provenance
	diags := promotion.New(promotion.ExtensionPromotions{}).Redaction(
		ir.Unmodeled{"openapi:x-sensitive": vendorExtension(`true`)}, promotion.Referent{}, &redact, &prov)
	assert.Empty(t, diags)
	assert.True(t, redact)
	assert.Empty(t, prov.Inferred)
}

// TestPromoter_OffWritesNothing runs every method past each of its guards. Each
// row is a different reason nothing may be written: the policy is off, nothing
// was kept to read, or there is nowhere to write or to record the guess.
func TestPromoter_OffWritesNothing(t *testing.T) {
	t.Parallel()
	stating := ir.Unmodeled{
		"openapi:x-stability": vendorExtension(`"beta"`),
		"openapi:x-sensitive": vendorExtension(`true`),
	}
	on, off := promotion.New(promotion.ExtensionPromotions{}), promotion.New(promotion.ExtensionPromotions{Disabled: true})

	for _, tc := range []struct {
		name      string
		p         promotion.Promoter
		unmodeled ir.Unmodeled
		prov      *ir.Provenance
	}{
		{"promotion disabled", off, stating, &ir.Provenance{}},
		{"no entries", on, nil, &ir.Provenance{}},
		{"no provenance", on, stating, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var tier ir.Stability
			var redact bool
			assert.Empty(t, promotion.Stability(tc.p, tc.unmodeled, &tier, tc.prov))
			assert.Empty(t, tc.p.Redaction(tc.unmodeled, promotion.Referent{}, &redact, tc.prov))
			assert.Equal(t, ir.StabilityUnstated, tier)
			assert.False(t, redact)
		})
	}
	assert.Empty(t, promotion.Stability(on, stating, nil, &ir.Provenance{}))
	assert.Empty(t, on.Redaction(stating, promotion.Referent{}, nil, &ir.Provenance{}))
}

// TestPromoter_ReferentFallsBackBeneathOwnKeys pins the merge a use site makes, a
// key on the carrier winning field by field and the rest coming from the
// referent. A referent's misfit reports nothing here, since it reports where it
// is written, and a carrier's own still does.
func TestPromoter_ReferentFallsBackBeneathOwnKeys(t *testing.T) {
	t.Parallel()
	own := ir.Unmodeled{
		"openapi:x-deprecated-reason": vendorExtension(`"mine"`),
		"openapi:x-sunset":            vendorExtension(`7`),
	}
	from := promotion.Referent{Members: ir.Deprecation{Message: "theirs", RemovalDate: "2030-01-01"}}
	dep := new(ir.Deprecation)
	var prov ir.Provenance
	diags := promotion.New(promotion.ExtensionPromotions{}).Deprecation(own, from, &dep, &prov)

	assert.Equal(t, ir.Deprecation{Message: "mine", RemovalDate: "2030-01-01"}, *dep)
	assert.Equal(t, promotion.ExtensionPromotionHeuristic, prov.Inferred)
	require.Len(t, diags, 1, "a carrier reports its own misfit")
	assert.Contains(t, diags[0].Message, "x-sunset")
}

// TestPromoter_AReferentAloneFillsAnEmptyCarrier is why neither reader's guard
// asks only of own: a bare $ref keeps no entry of its own, and it is the shape
// most use sites have.
func TestPromoter_AReferentAloneFillsAnEmptyCarrier(t *testing.T) {
	t.Parallel()
	p := promotion.New(promotion.ExtensionPromotions{})

	dep := new(ir.Deprecation)
	var prov ir.Provenance
	p.Deprecation(nil, promotion.Referent{Members: ir.Deprecation{RemovalDate: "2030-01-01"}}, &dep, &prov)
	assert.Equal(t, "2030-01-01", dep.RemovalDate)

	var redact bool
	var marked ir.Provenance
	p.Redaction(nil, promotion.Referent{Redacted: true}, &redact, &marked)
	assert.True(t, redact)
	assert.Equal(t, promotion.ExtensionPromotionHeuristic, marked.Inferred)
}

// TestPromoter_AReferentsKeyDeprecatesTheUse pins the pairing the use-site
// merge makes of a referent's key with its keyword: a key on the referent
// counts as the referent's `deprecated`, which no keyword beside the $ref
// withdraws, as a `false` there states nothing.
func TestPromoter_AReferentsKeyDeprecatesTheUse(t *testing.T) {
	t.Parallel()
	var dep *ir.Deprecation
	var prov ir.Provenance
	diags := promotion.New(promotion.ExtensionPromotions{}).Deprecation(nil, promotion.Referent{Deprecated: true}, &dep, &prov)
	assert.Empty(t, diags)
	assert.NotNil(t, dep)
	assert.Equal(t, promotion.ExtensionPromotionHeuristic, prov.Inferred)
}

// TestPromoter_ADeprecatedBaseHomesTheNodesMembers pins Referent.Homes: a node
// deprecated only through the schema it composes, as a decorator over a
// deprecated target is, takes the members its own keys write, and stays
// unset, and unmarked, where they write none, since the keyword lent it is no
// key.
func TestPromoter_ADeprecatedBaseHomesTheNodesMembers(t *testing.T) {
	t.Parallel()
	sunset := ir.Unmodeled{"openapi:x-sunset": vendorExtension(`"2099-01-01"`)}
	for _, tc := range []struct {
		name  string
		own   ir.Unmodeled
		homes bool
		want  *ir.Deprecation
	}{
		{"a member on a node deprecated through its base", sunset, true, &ir.Deprecation{RemovalDate: "2099-01-01"}},
		{"no member on such a node", nil, true, nil},
		{"a member on a live node", sunset, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var dep *ir.Deprecation
			var prov ir.Provenance
			diags := promotion.New(promotion.ExtensionPromotions{}).Deprecation(tc.own, promotion.Referent{Homes: tc.homes}, &dep, &prov)
			assert.Empty(t, diags)
			assert.Equal(t, tc.want, dep)
			assert.Equal(t, tc.want != nil, prov.Inferred == promotion.ExtensionPromotionHeuristic)
		})
	}
}

// TestPromoter_ReferentReadsWhatASchemaOffersAsFallback pins the reading of a $ref's target.
// A key stating a deprecation or a redaction lends it, and a member lends only
// where the target is deprecated: on a live one it annotates nothing, there or at
// a use. What the target cannot hold lends nothing and returns no report, since
// the target makes it where it is written.
func TestPromoter_ReferentReadsWhatASchemaOffersAsFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		declared bool
		entries  ir.Unmodeled
		want     promotion.Referent
	}{
		{"no key", false, nil, promotion.Referent{}},
		{"a key deprecating gives its members a home", false, ir.Unmodeled{
			"openapi:x-stability": vendorExtension(`"deprecated"`),
			"openapi:x-sunset":    vendorExtension(`"2030-01-01"`),
		}, promotion.Referent{Deprecated: true, Members: ir.Deprecation{RemovalDate: "2030-01-01"}}},
		{"the keyword gives them one", true, ir.Unmodeled{
			"openapi:x-deprecated-reason": vendorExtension(`"why"`),
			"openapi:x-deprecated-since":  vendorExtension(`"1.0"`),
		}, promotion.Referent{Members: ir.Deprecation{Message: "why", Since: "1.0"}}},
		{"a member on a live schema lends nothing", false, ir.Unmodeled{
			"openapi:x-sunset": vendorExtension(`"2030-01-01"`),
		}, promotion.Referent{}},
		{"a redaction", false, ir.Unmodeled{"openapi:x-sensitive": vendorExtension(`true`)},
			promotion.Referent{Redacted: true}},
		{"misfits lend nothing", true, ir.Unmodeled{
			"openapi:x-sunset":    vendorExtension(`7`),
			"openapi:x-sensitive": vendorExtension(`"yes"`),
			"openapi:x-stability": vendorExtension(`3`),
		}, promotion.Referent{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := promotion.New(promotion.ExtensionPromotions{}).Referent(reftable.DocumentPointer{}, tc.declared,
				func() ir.Unmodeled { return tc.entries })
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestPromoter_ReferentReadsEachSchemaOnce is the seam behind the cost: a schema
// has as many uses as a document names it, and a key's value can be as large as
// the document, so a read per use is their product. Copies of a promoter share
// the answer, as copies of a context do, and a second schema is read for itself.
func TestPromoter_ReferentReadsEachSchemaOnce(t *testing.T) {
	t.Parallel()
	reads := 0
	entries := func() ir.Unmodeled {
		reads++
		return ir.Unmodeled{"openapi:x-sensitive": vendorExtension(`true`)}
	}
	first := reftable.DocumentPointer{Pointer: "/components/schemas/A"}
	second := reftable.DocumentPointer{Pointer: "/components/schemas/B"}

	p := promotion.New(promotion.ExtensionPromotions{})
	copied := p
	assert.True(t, p.Referent(first, false, entries).Redacted)
	assert.True(t, copied.Referent(first, false, entries).Redacted)
	assert.Equal(t, 1, reads, "a second use reads what the first remembered")
	assert.True(t, p.Referent(second, false, entries).Redacted)
	assert.Equal(t, 2, reads)
}

// TestPromoter_ReferentReadsNothingOffPolicy holds the promoter with no key to
// look for: it reads no schema, so a compile that turned promotion off spends
// nothing on a $ref.
func TestPromoter_ReferentReadsNothingOffPolicy(t *testing.T) {
	t.Parallel()
	p := promotion.New(promotion.ExtensionPromotions{Disabled: true})
	assert.Equal(t, promotion.Referent{}, p.Referent(reftable.DocumentPointer{}, true, func() ir.Unmodeled {
		t.Error("a policy mapping no key read the schema")
		return nil
	}))
}

// TestPromoter_KeysListsWhatThePolicyReads pins the list a use site looks up on
// its referent: one entry per key however many sources read it, so a lookup is
// not repeated, in an order of its own and not the map's. A disabled policy
// reads nothing, so a use site looks up nothing.
func TestPromoter_KeysListsWhatThePolicyReads(t *testing.T) {
	t.Parallel()
	got := slices.Collect(promotion.New(promotion.ExtensionPromotions{
		Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{
			promotion.TargetDeprecationMessage:     {{Key: "x-b", Member: "text"}, {Key: "x-a"}},
			promotion.TargetDeprecationRemovalDate: {{Key: "x-b", Member: "date"}},
		},
	}).Keys())
	assert.Equal(t, []string{"x-a", "x-b"}, got)
	assert.Empty(t, slices.Collect(promotion.New(promotion.ExtensionPromotions{Disabled: true}).Keys()))
}

// TestUnionOpenness_OpensOnAMappedKey pins the union twin of the enum
// promotion: no default key names TargetUnionOpen, so a union opens only under a
// mapping that names one, and then as an enum does, on any value but an
// explicit false, marking the node once.
func TestUnionOpenness_OpensOnAMappedKey(t *testing.T) {
	t.Parallel()
	mapped := mapping(promotion.TargetUnionOpen, promotion.ExtensionSource{Key: "x-open"})
	tests := []struct {
		name   string
		policy promotion.ExtensionPromotions
		value  string
		open   bool
	}{
		{"a mapped key", mapped, `true`, true},
		{"a bare mapped key", mapped, `null`, true},
		{"an explicit false", mapped, `false`, false},
		{"the default mapping, which names no key for it", promotion.ExtensionPromotions{}, `true`, false},
		{"a key mapped to the enum target", mapping(promotion.TargetEnumOpen, promotion.ExtensionSource{Key: "x-open"}), `true`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := ir.Union{Closed: true}
			var prov ir.Provenance
			diags := promotion.UnionOpenness(promotion.New(tc.policy), ir.Unmodeled{"openapi:x-open": vendorExtension(tc.value)}, &u.Closed, &prov)
			assert.Empty(t, diags)
			assert.Equal(t, !tc.open, u.Closed)
			if tc.open {
				assert.Equal(t, promotion.ExtensionPromotionHeuristic, prov.Inferred)
			} else {
				assert.Empty(t, prov.Inferred, "nothing was inferred, so nothing is marked")
			}
		})
	}
	assert.Nil(t, promotion.UnionOpenness(promotion.New(mapped), ir.Unmodeled{"openapi:x-open": vendorExtension(`true`)}, nil, &ir.Provenance{}))
	u := ir.Union{Closed: true}
	assert.Nil(t, promotion.UnionOpenness(promotion.New(mapped), ir.Unmodeled{"openapi:x-open": vendorExtension(`true`)}, &u.Closed, nil))
	assert.True(t, u.Closed, "with nowhere to record the guess, none is made")
}

// TestPromoter_ValuesNameWhatStatesTheFact pins a source listing Values: one of
// them states the fact, compared with a string's text or a scalar's JSON, and
// any other value, null and a list included, states nothing and is reported,
// where no other target reads the key. It is how a key spelling openness as a
// word reads: without the list, any value but false opened the enum.
func TestPromoter_ValuesNameWhatStatesTheFact(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value    string
		values   []string
		open     bool
		reported bool
	}{
		{`"allow"`, []string{"allow"}, true, false},
		{`"disallow"`, []string{"allow"}, false, true},
		{`true`, []string{"true"}, true, false},
		{`"true"`, []string{"true"}, true, false},
		{`1`, []string{"1"}, true, false},
		{`false`, []string{"true"}, false, true},
		{`null`, []string{"null"}, false, true},
		{`["allow"]`, []string{"allow"}, false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			p := promotion.New(mapping(promotion.TargetEnumOpen, promotion.ExtensionSource{Key: "x-k", Values: tc.values}))
			closed := true
			var prov ir.Provenance
			diags := promotion.EnumOpenness(p, ir.Unmodeled{"openapi:x-k": vendorExtension(tc.value)}, &closed, &prov)

			assert.Equal(t, tc.open, !closed)
			require.Equal(t, tc.reported, len(diags) == 1, "reports: %+v", diags)
			if tc.reported {
				assert.Equal(t, `extension "x-k" holds none of the values the policy lists, so it does not fill enum.open`,
					diags[0].Message)
			}
		})
	}
}

// TestPromoter_AValueAnotherTargetReadsIsItsToReport pins the half of a listed
// source a key shared with another source needs: a value the list leaves out
// states nothing and is not reported where another source, of its own target
// or another, reads it off the same member, listing it or reading its own
// vocabulary, as that reading decides what it is. Where every other reader's
// list leaves it out too, or none reads that member, no reading takes it, and
// the source reports it.
func TestPromoter_AValueAnotherTargetReadsIsItsToReport(t *testing.T) {
	t.Parallel()
	shared := promotion.ExtensionSource{Key: "x-k", Values: []string{"yes"}}
	for _, tc := range []struct {
		name     string
		other    map[promotion.ExtensionTarget][]promotion.ExtensionSource
		reported bool
		redacted bool
	}{
		{"a later source of its own target listing it", map[promotion.ExtensionTarget][]promotion.ExtensionSource{
			promotion.TargetRedaction: {shared, {Key: "x-k", Values: []string{"no"}}}}, false, true},
		{"another listing it", map[promotion.ExtensionTarget][]promotion.ExtensionSource{
			promotion.TargetEnumOpen: {{Key: "x-k", Values: []string{"yes", "no"}}}}, false, false},
		{"another reading its own vocabulary", map[promotion.ExtensionTarget][]promotion.ExtensionSource{
			promotion.TargetStability: {{Key: "x-k"}}}, false, false},
		{"another listing only others", map[promotion.ExtensionTarget][]promotion.ExtensionSource{
			promotion.TargetEnumOpen: {shared}}, true, false},
		{"another member listing it", map[promotion.ExtensionTarget][]promotion.ExtensionSource{
			promotion.TargetUnionOpen: {{Key: "x-k", Member: "m", Values: []string{"no"}}}}, true, false},
		{"another key listing it", map[promotion.ExtensionTarget][]promotion.ExtensionSource{
			promotion.TargetDeprecation: {{Key: "x-other", Values: []string{"no"}}}}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			targets := map[promotion.ExtensionTarget][]promotion.ExtensionSource{promotion.TargetRedaction: {shared}}
			maps.Copy(targets, tc.other)
			p := promotion.New(promotion.ExtensionPromotions{Targets: targets})
			var redact bool
			var prov ir.Provenance
			diags := p.Redaction(ir.Unmodeled{"openapi:x-k": vendorExtension(`"no"`)}, promotion.Referent{}, &redact, &prov)
			assert.Equal(t, tc.redacted, redact)
			if !tc.reported {
				assert.Empty(t, diags)
				return
			}
			require.Len(t, diags, 1)
			assert.Equal(t, `extension "x-k" holds none of the values the policy lists, so it does not fill redaction`,
				diags[0].Message)
		})
	}
	assert.True(t, slices.Equal([]string{"yes"}, shared.Values), "the caller's list is read, not written")
}

// TestPromoter_ACallersValuesAreCopied holds the policy a promoter carries apart
// from the caller's: a list of values changed after the promoter is built
// changes nothing it reads.
func TestPromoter_ACallersValuesAreCopied(t *testing.T) {
	t.Parallel()
	values := []string{"deprecated"}
	p := promotion.New(mapping(promotion.TargetDeprecation, promotion.ExtensionSource{Key: "x-k", Values: values}))
	values[0] = "retired"
	var dep *ir.Deprecation
	var prov ir.Provenance
	p.Deprecation(ir.Unmodeled{"openapi:x-k": vendorExtension(`"deprecated"`)}, promotion.Referent{}, &dep, &prov)
	assert.NotNil(t, dep)
}

// TestUnheldDeprecation_ReportsEachKeyStatingOne holds a tag's deprecation keys
// to one finding each where they state a deprecation: none where nothing
// states one, none for a listed source whose value is another, and none for a
// tier key of a tag forming no group, which UnformedTier reports.
func TestUnheldDeprecation_ReportsEachKeyStatingOne(t *testing.T) {
	t.Parallel()
	p := promotion.New(promotion.ExtensionPromotions{})
	at := func(u ir.Unmodeled, formed bool) []string {
		diags := promotion.UnheldDeprecation(p, "T", formed, u)
		out := make([]string, 0, len(diags))
		for _, d := range diags {
			out = append(out, string(d.Provenance.Pointer))
		}
		return out
	}
	entry := func(key, raw string) ir.UnmodeledEntry {
		return ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Value: ir.RawValue(raw),
			Provenance: ir.Provenance{Pointer: jsontext.Pointer("/tags/0/" + key)}}
	}
	deprecated := ir.Unmodeled{
		"openapi:x-stability":         entry("x-stability", `"deprecated"`),
		"openapi:x-fern-availability": entry("x-fern-availability", `"beta"`),
		"openapi:x-deprecated-reason": entry("x-deprecated-reason", `"old"`),
	}
	assert.Equal(t, []string{"/tags/0/x-stability", "/tags/0/x-deprecated-reason"}, at(deprecated, true))
	assert.Equal(t, []string{"/tags/0/x-deprecated-reason"}, at(deprecated, false), "the tier keys are UnformedTier's")
	assert.Empty(t, at(ir.Unmodeled{"openapi:x-deprecated-reason": entry("x-deprecated-reason", `"old"`)}, true),
		"a member beside no deprecation states none")
}
