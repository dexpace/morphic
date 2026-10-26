package oas_test

import (
	"go/constant"
	"go/types"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/internal/decls"
)

// minors lists the minors the table covers.
func minors() []oas.Minor { return []oas.Minor{oas.V30, oas.V31, oas.V32} }

// declaredKinds returns the Kind constants package oas declares, read
// type-checked, so a kind added there is held to the table with no list here to
// keep in step.
func declaredKinds(t *testing.T) []oas.Kind {
	t.Helper()
	p, err := decls.Load("github.com/dexpace/morphic/compilers/openapi/internal/oas")
	require.NoError(t, err)
	kind, err := p.Named("Kind")
	require.NoError(t, err)
	var out []oas.Kind
	for c := range p.Objects[*types.Const]() {
		if !types.Identical(c.Type(), kind) {
			continue
		}
		v, exact := constant.Uint64Val(c.Val())
		require.True(t, exact, "%s is no small integer", c.Name())
		out = append(out, oas.Kind(v))
	}
	require.NotEmpty(t, out, "found no kind, so this proves nothing")
	return out
}

// TestTable_EveryKindHasOneObject pins that the table covers each declared kind
// once, that Object finds it, and that it holds no object of a kind the package
// does not declare.
func TestTable_EveryKindHasOneObject(t *testing.T) {
	spec := oas.Table()
	kinds := declaredKinds(t)
	seen := map[oas.Kind]bool{}
	for _, o := range spec.Objects() {
		assert.False(t, seen[o.Kind], "%s is declared twice", o.Name)
		assert.Contains(t, kinds, o.Kind, "%s is of no declared kind", o.Name)
		assert.NotEmpty(t, o.Name)
		seen[o.Kind] = true
	}
	for _, k := range kinds {
		got, ok := spec.Object(k)
		require.True(t, ok, "kind %d has no object", k)
		assert.Equal(t, k, got.Kind)
	}
	assert.NotContains(t, kinds, oas.Kind(0), "the zero kind is no object")
	_, ok := spec.Object(0)
	assert.False(t, ok, "the zero kind names no object")
}

// TestTable_EveryRowCitesTheSpecification pins that each field, pattern and
// rule names a sentence of a text it can have come from.
func TestTable_EveryRowCitesTheSpecification(t *testing.T) {
	valid := func(where string, cites []oas.Cite) {
		require.NotEmpty(t, cites, where)
		for _, c := range cites {
			assert.Contains(t, minors(), c.Minor, where)
			assert.NotEmpty(t, c.Section, where)
			assert.NotEmpty(t, c.Sentence, where)
		}
	}
	for _, o := range oas.Table().Objects() {
		for _, f := range o.Fields {
			valid(o.Name+"."+f.Name, f.Cites)
			assert.True(t, slices.ContainsFunc(f.Cites, func(c oas.Cite) bool { return f.Defined.Holds(c.Minor) }),
				"%s.%s: no citation comes from a minor that defines the field", o.Name, f.Name)
		}
		for _, p := range o.Patterned {
			valid(o.Name+"."+p.Name, p.Cites)
		}
		for i, r := range o.Rules {
			valid(o.Name+" rule "+itoa(i), r.Cites)
		}
	}
}

// itoa spells a small number for a failure message.
func itoa(n int) string { return string(rune('0' + n)) }

// TestTable_SpansAreWellFormed pins every span the table states.
func TestTable_SpansAreWellFormed(t *testing.T) {
	for _, o := range oas.Table().Objects() {
		assert.True(t, validSpan(o.Referenceable), o.Name)
		assert.True(t, validSpan(o.Boolean), o.Name)
		assert.True(t, validSpan(o.RefSiblings), o.Name)
		for _, f := range o.Fields {
			where := o.Name + "." + f.Name
			assert.NotZero(t, f.Defined, where+": a field is defined at some minor")
			for _, s := range []oas.Span{f.Defined, f.Required, f.NonEmpty} {
				assert.True(t, validSpan(s), where)
			}
			for _, m := range minors() {
				if f.Required.Holds(m) {
					assert.True(t, f.Defined.Holds(m), "%s is required at %s, where it is not defined", where, m)
				}
			}
			for _, v := range f.Values {
				assert.True(t, validSpan(v.Span) && v.Span != (oas.Span{}), where+" value "+v.Name)
			}
			for _, a := range f.Alts {
				assert.True(t, validSpan(a.Span), where)
			}
		}
		for _, p := range o.Patterned {
			assert.True(t, validSpan(p.Span), o.Name+"."+p.Name)
		}
		for i, r := range o.Rules {
			assert.True(t, validSpan(r.Span), o.Name+" rule "+itoa(i))
			assert.NotZero(t, r.Span, o.Name+" rule "+itoa(i)+" never applies")
		}
	}
}

// TestTable_FieldNamesDoNotCollide pins that an object names a field once at
// each minor: two rows of one name must be defined at disjoint minors.
func TestTable_FieldNamesDoNotCollide(t *testing.T) {
	for _, o := range oas.Table().Objects() {
		for i, a := range o.Fields {
			for _, b := range o.Fields[i+1:] {
				if a.Name == b.Name {
					assert.False(t, overlaps(a.Defined, b.Defined), "%s.%s is defined twice at one minor", o.Name, a.Name)
				}
			}
		}
	}
}

// TestTable_ContainersNameTheKindTheyHold pins that a field holding objects
// names an object the table has, and that no other field names a kind.
func TestTable_ContainersNameTheKindTheyHold(t *testing.T) {
	spec := oas.Table()
	holds := func(where string, shape oas.Shape, of oas.Kind) {
		if shape == oas.Nested || shape == oas.Map || shape == oas.List {
			_, ok := spec.Object(of)
			assert.True(t, ok, "%s holds a kind with no object", where)
			return
		}
		assert.Zero(t, of, "%s names a kind though it holds none", where)
	}
	for _, o := range spec.Objects() {
		for _, f := range o.Fields {
			holds(o.Name+"."+f.Name, f.Shape, f.Of)
		}
		for _, p := range o.Patterned {
			holds(o.Name+"."+p.Name, p.Shape, p.Of)
		}
	}
}

// TestTable_RulesNameFieldsTheObjectHas pins that a rule reads fields its
// object defines, and that a Code rule is either named or owned.
func TestTable_RulesNameFieldsTheObjectHas(t *testing.T) {
	for _, o := range oas.Table().Objects() {
		has := func(name string) bool {
			return name == "" || name == "$key" || slices.ContainsFunc(o.Fields, func(f oas.Field) bool { return f.Name == name })
		}
		for i, r := range o.Rules {
			where := o.Name + " rule " + itoa(i)
			names := []string{r.Field, r.Of}
			if r.Kind != oas.RuleForbiddenWhen {
				names = append(names, r.Fields...) // a ban may name fields the object does not define
			}
			for _, name := range names {
				assert.True(t, has(name), "%s names the field %q, which the object lacks", where, name)
			}
			for _, term := range r.When {
				assert.True(t, has(term.Field), "%s tests the field %q, which the object lacks", where, term.Field)
				assert.NotEmpty(t, term.In, where)
			}
			if r.Kind == oas.RuleCode {
				assert.NotEmpty(t, r.Name, where)
			}
		}
	}
}

// TestTable_OnlyReferenceAndRequirementsTakeNoExtensions pins which objects the
// specification bars from carrying x- keys.
func TestTable_OnlyReferenceAndRequirementsTakeNoExtensions(t *testing.T) {
	var closed []string
	for _, o := range oas.Table().Objects() {
		if !o.Extensible {
			closed = append(closed, o.Name)
		}
	}
	assert.Equal(t, []string{"Reference Object", "Security Requirement Object"}, closed)
}

// TestTable_OwnedRulesNameTheirOwners pins which packages the table defers to.
func TestTable_OwnedRulesNameTheirOwners(t *testing.T) {
	owners := map[string]bool{}
	for _, o := range oas.Table().Objects() {
		for _, r := range o.Rules {
			if r.Owner != "" {
				owners[r.Owner] = true
			}
		}
		for _, f := range o.Fields {
			for _, owner := range []string{f.Owner, f.ShapeOwner} {
				if owner != "" {
					owners[owner] = true
				}
			}
		}
	}
	assert.Equal(t, map[string]bool{"annotation": true, "auth": true, "load": true, "operation": true, "schema": true}, owners)
}

// laterRow is one construct a later minor added: the object, the field, the
// value added or "" for the field itself, and the minor that added it.
type laterRow struct {
	Object, Key, Value, Since string
}

// laterRows are the 34 constructs OpenAPI 3.1 and 3.2 added to an object, as
// the compiler has warned of them since it first read the versions apart.
func laterRows() []laterRow {
	return []laterRow{
		{"OpenAPI Object", "jsonSchemaDialect", "", "3.1"},
		{"OpenAPI Object", "webhooks", "", "3.1"},
		{"OpenAPI Object", "$self", "", "3.2"},
		{"Info Object", "summary", "", "3.1"},
		{"License Object", "identifier", "", "3.1"},
		{"Components Object", "pathItems", "", "3.1"},
		{"Components Object", "mediaTypes", "", "3.2"},
		{"Server Object", "name", "", "3.2"},
		{"Tag Object", "summary", "", "3.2"},
		{"Tag Object", "parent", "", "3.2"},
		{"Tag Object", "kind", "", "3.2"},
		{"Path Item Object", "query", "", "3.2"},
		{"Path Item Object", "additionalOperations", "", "3.2"},
		{"Parameter Object", "in", "querystring", "3.2"},
		{"Parameter Object", "style", "cookie", "3.2"},
		{"Media Type Object", "$ref", "", "3.2"},
		{"Media Type Object", "description", "", "3.2"},
		{"Media Type Object", "itemSchema", "", "3.2"},
		{"Media Type Object", "itemEncoding", "", "3.2"},
		{"Media Type Object", "prefixEncoding", "", "3.2"},
		{"Encoding Object", "encoding", "", "3.2"},
		{"Encoding Object", "prefixEncoding", "", "3.2"},
		{"Encoding Object", "itemEncoding", "", "3.2"},
		{"Example Object", "dataValue", "", "3.2"},
		{"Example Object", "serializedValue", "", "3.2"},
		{"Response Object", "summary", "", "3.2"},
		{"Security Scheme Object", "type", "mutualTLS", "3.1"},
		{"Security Scheme Object", "oauth2MetadataUrl", "", "3.2"},
		{"Security Scheme Object", "deprecated", "", "3.2"},
		{"OAuth Flows Object", "deviceAuthorization", "", "3.2"},
		{"Discriminator Object", "defaultMapping", "", "3.2"},
		{"XML Object", "nodeType", "", "3.2"},
		{"Reference Object", "summary", "", "3.1"},
		{"Reference Object", "description", "", "3.1"},
	}
}

// laterExtras are the constructs the table holds beyond laterRows, each with
// the reason: it is a 3.2 field that only a flow added in 3.2 can use, which the
// warning of the flow itself already covers.
func laterExtras() []laterRow {
	return []laterRow{{"OAuth Flow Object", "deviceAuthorizationUrl", "", "3.2"}}
}

// tableLaterRows reads off the table every construct a minor after 3.0 added:
// a field, a value of one, or a Reference Object standing in an object's place.
// A JSON Schema keyword is the schema lowering's to place.
func tableLaterRows() []laterRow {
	var rows []laterRow
	for _, o := range oas.Table().Objects() {
		if o.Referenceable.Since > oas.V30 {
			rows = append(rows, laterRow{o.Name, "$ref", "", o.Referenceable.Since.String()})
		}
		for _, f := range o.Fields {
			if f.Keyword {
				continue
			}
			if f.Defined.Since > oas.V30 {
				rows = append(rows, laterRow{o.Name, f.Name, "", f.Defined.Since.String()})
			}
			for _, v := range f.Values {
				if v.Span.Since > oas.V30 {
					rows = append(rows, laterRow{o.Name, f.Name, v.Name, v.Span.Since.String()})
				}
			}
		}
	}
	return rows
}

// TestTable_AgreesWithTheConstructsLaterMinorsAdded pins that the table states
// exactly the constructs the compiler warns of for a document declaring an
// older minor, and the one beyond them that it records.
func TestTable_AgreesWithTheConstructsLaterMinorsAdded(t *testing.T) {
	assert.Len(t, laterRows(), 34)
	order := func(a, b laterRow) int {
		return strings.Compare(a.Object+"/"+a.Key+"/"+a.Value, b.Object+"/"+b.Key+"/"+b.Value)
	}
	want := append(laterRows(), laterExtras()...)
	got := tableLaterRows()
	slices.SortFunc(want, order)
	slices.SortFunc(got, order)
	assert.Empty(t, cmp.Diff(want, got))
}
