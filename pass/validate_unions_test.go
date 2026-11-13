// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestValidate_ReportsAVariantNamingAnEarlierVariantsType pins what a union
// whose variants name one type draws: a warning under one code where it is
// exclusive and an info under another where it is not, at each variant naming
// a type an earlier one names, nullable or not, untagged or tagged in the
// payload, and nothing where its variants name distinct types or a key tells
// them apart.
func TestValidate_ReportsAVariantNamingAnEarlierVariantsType(t *testing.T) {
	t.Parallel()
	variants := func(targets ...ir.TypeID) []ir.Variant {
		vs := make([]ir.Variant, 0, len(targets))
		for _, target := range targets {
			vs = append(vs, ir.Variant{Type: ir.TypeRef{Target: target}})
		}
		return vs
	}
	const (
		exclusive = "warning pass/exclusive-union-repeated-variant t/u/variants/"
		inclusive = "info pass/inclusive-union-redundant-variant t/u/variants/"
	)
	tests := []struct {
		name  string
		union ir.Union
		want  []string
	}{
		{"exclusive", ir.Union{Exclusive: true, Variants: variants("t/m", "t/m")},
			[]string{exclusive + "1: variants 0 and 1"}},
		{"inclusive", ir.Union{Variants: variants("t/m", "t/m")},
			[]string{inclusive + "1: variants 0 and 1"}},
		{"a nullable repeat", ir.Union{Exclusive: true, Variants: []ir.Variant{
			{Type: ir.TypeRef{Target: "t/prim/string"}}, {Type: ir.TypeRef{Target: "t/prim/string", Nullable: true}},
		}}, []string{exclusive + "1: variants 0 and 1"}},
		{"three times", ir.Union{Exclusive: true, Variants: variants("t/m", "t/prim/string", "t/m", "t/m")},
			[]string{exclusive + "2: variants 0 and 2", exclusive + "3: variants 0 and 3"}},
		{"three times, inclusive", ir.Union{Variants: variants("t/m", "t/prim/string", "t/m", "t/m")},
			[]string{inclusive + "2: variants 0 and 2", inclusive + "3: variants 0 and 3"}},
		{"after another type", ir.Union{Exclusive: true, Variants: variants("t/prim/string", "t/m", "t/m")},
			[]string{exclusive + "2: variants 1 and 2"}},
		{"tagged in the payload", ir.Union{Exclusive: true, WireTagged: true,
			Discriminator: &ir.Discriminator{PropertyName: "kind"}, Variants: variants("t/m", "t/m")},
			[]string{exclusive + "1: variants 0 and 1"}},
		{"tagged by key", ir.Union{Exclusive: true, WireTagged: true, Variants: variants("t/m", "t/m")}, []string{}},
		{"distinct types", ir.Union{Exclusive: true, Variants: variants("t/m", "t/prim/string")}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			u := tt.union
			u.ID = "t/u"
			doc.Types[u.ID] = &u
			diags := validated(t, doc)
			got := make([]string, 0, len(diags))
			for _, d := range diags {
				pair, _, _ := strings.Cut(d.Message, " of union")
				got = append(got, fmt.Sprintf("%s %s %s: %s", d.Severity, d.Code, d.Provenance.Node, pair))
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestValidate_SaysWhatARepeatedVariantCosts pins the message of each code:
// what an exclusive union refuses, and what an inclusive one's two variants
// share.
func TestValidate_SaysWhatARepeatedVariantCosts(t *testing.T) {
	t.Parallel()
	for exclusive, want := range map[bool]struct{ code, message string }{
		true: {"pass/exclusive-union-repeated-variant",
			"variants 0 and 1 of union t/u both name t/m, so a value of it matches both, and the union, " +
				"which admits a value only one variant matches, refuses it"},
		false: {"pass/inclusive-union-redundant-variant",
			"variants 0 and 1 of union t/u both name t/m, so neither admits a value the other does not, null aside"},
	} {
		doc := validDoc()
		doc.Types["t/u"] = &ir.Union{ID: "t/u", Exclusive: exclusive,
			Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/m"}}, {Type: ir.TypeRef{Target: "t/m"}}}}

		assert.Equal(t, want.message, messageForCode(t, validated(t, doc), want.code))
	}
}
