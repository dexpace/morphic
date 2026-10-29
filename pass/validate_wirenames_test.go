package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func TestValidate_DuplicateWireName(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	m := doc.Types["t/m"].(*ir.Model)
	m.Properties = append(m.Properties, ir.Property{
		ID: "p/m/b", WireName: "a", Type: ir.TypeRef{Target: "t/prim/string"},
	})
	diags := validated(t, doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, irtest.DiagCodes(diags), "pass/duplicate-wire-name")
}

// TestValidate_EmptyEffectiveWireNameIsSkipped covers the source-name fallback
// of ir.Property.EffectiveWireName and the empty-name continue in
// checkDuplicateWireNames.
func TestValidate_EmptyEffectiveWireNameIsSkipped(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/blank"] = &ir.Model{
		ID: "t/blank",
		Properties: []ir.Property{
			// No WireName, no Source name -> effective name "" -> skipped.
			{ID: "p/a", Type: ir.TypeRef{Target: "t/prim/string"}},
			{ID: "p/b", Type: ir.TypeRef{Target: "t/prim/string"}},
			// No WireName but a Source name -> the effective name is Source.
			{ID: "p/c", Name: ir.Naming{Source: "named"}, Type: ir.TypeRef{Target: "t/prim/string"}},
		},
	}
	// Two blank-named properties must NOT collide (both skipped), and the
	// source-named one is unique, so no duplicate-wire-name diagnostic fires.
	assert.NotContains(t, irtest.DiagCodes(validated(t, doc)), "pass/duplicate-wire-name")
}

// aliasProperty returns a string property of model t/m named source and
// answering to aliases besides.
func aliasProperty(source string, aliases ...string) ir.Property {
	return ir.Property{
		ID:   ir.PropID("p/m/" + source),
		Name: ir.Naming{Source: source, Canonical: source, Aliases: aliases},
		Type: ir.TypeRef{Target: "t/prim/string"},
	}
}

// aliasDoc returns validDoc with model t/m holding props.
func aliasDoc(props ...ir.Property) *ir.Document {
	doc := validDoc()
	doc.Types["t/m"].(*ir.Model).Properties = props
	return doc
}

// ambiguousAliasDiags returns the pass/ambiguous-alias diagnostics Validate
// reports on doc.
func ambiguousAliasDiags(t *testing.T, doc *ir.Document) []ir.Diagnostic {
	t.Helper()
	var out []ir.Diagnostic
	for _, d := range validated(t, doc) {
		if d.Code == "pass/ambiguous-alias" {
			out = append(out, d)
		}
	}
	return out
}

// TestValidate_AmbiguousAliasIsReported covers each way a sibling answers to an
// alias: by its wire name declared after the alias or before it, by an explicit
// WireName rather than its Source, by the wire name the property itself shares,
// and by an alias of its own. Each is reported once, at the property whose
// alias it is, naming the sibling.
func TestValidate_AmbiguousAliasIsReported(t *testing.T) {
	t.Parallel()
	wired := ir.Property{ID: "p/m/w", WireName: "id", Type: ir.TypeRef{Target: "t/prim/string"}}
	for _, tc := range []struct {
		name    string
		props   []ir.Property
		node    string
		message string
	}{
		{
			name:    "sibling's name declared after the alias",
			props:   []ir.Property{aliasProperty("b", "a"), aliasProperty("a")},
			node:    "t/m/properties/0",
			message: `alias "a" of property "p/m/b" is a name property "p/m/a" also answers to`,
		},
		{
			name:    "sibling's name declared before the alias",
			props:   []ir.Property{aliasProperty("a"), aliasProperty("b", "a")},
			node:    "t/m/properties/1",
			message: `alias "a" of property "p/m/b" is a name property "p/m/a" also answers to`,
		},
		{
			name:    "sibling's explicit wire name",
			props:   []ir.Property{wired, aliasProperty("b", "id")},
			node:    "t/m/properties/1",
			message: `alias "id" of property "p/m/b" is a name property "p/m/w" also answers to`,
		},
		{
			name: "sibling sharing the property's own wire name",
			props: []ir.Property{aliasProperty("a"), {
				ID:   "p/m/a2",
				Name: ir.Naming{Source: "a", Canonical: "a", Aliases: []string{"a"}},
				Type: ir.TypeRef{Target: "t/prim/string"},
			}},
			node:    "t/m/properties/1",
			message: `alias "a" of property "p/m/a2" is a name property "p/m/a" also answers to`,
		},
		{
			name:    "sibling's alias",
			props:   []ir.Property{aliasProperty("b", "x"), aliasProperty("c", "x")},
			node:    "t/m/properties/1",
			message: `alias "x" of property "p/m/c" is a name property "p/m/b" also answers to`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ambiguousAliasDiags(t, aliasDoc(tc.props...))
			require.Len(t, got, 1)
			assert.Equal(t, ir.SeverityError, got[0].Severity)
			assert.Equal(t, tc.node, got[0].Provenance.Node)
			assert.Equal(t, tc.message, got[0].Message)
		})
	}
}

// TestValidate_UnambiguousAliasIsClean pins what the check leaves alone. An
// alias repeating its own property's name is irverify's, as is a repeat within
// one list; an empty alias answers to nothing; and two models are two scopes,
// since a field alias is resolved within its record, whether the other model
// holds the same alias or, read just before, a property of that name at
// another position.
func TestValidate_UnambiguousAliasIsClean(t *testing.T) {
	t.Parallel()
	twoModels := aliasDoc(aliasProperty("b", "x"))
	twoModels.Types["t/n"] = &ir.Model{ID: "t/n", Properties: []ir.Property{
		{ID: "p/n/c", Name: ir.Naming{Source: "c", Canonical: "c", Aliases: []string{"x"}},
			Type: ir.TypeRef{Target: "t/prim/string"}},
	}}
	claimedBefore := aliasDoc(aliasProperty("b", "x"), aliasProperty("c"))
	claimedBefore.Types["t/a"] = &ir.Model{ID: "t/a", Properties: []ir.Property{aliasProperty("a"), aliasProperty("x")}}
	for name, doc := range map[string]*ir.Document{
		"distinct aliases":                 aliasDoc(aliasProperty("a", "x"), aliasProperty("b", "y")),
		"its own name":                     aliasDoc(aliasProperty("a", "a")),
		"repeated in one list":             aliasDoc(aliasProperty("a", "x", "x")),
		"empty on two siblings":            aliasDoc(aliasProperty("a", ""), aliasProperty("b", "")),
		"same alias, two models":           twoModels,
		"a name another model reads first": claimedBefore,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, ambiguousAliasDiags(t, doc))
		})
	}
}
