// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"go/constant"
	"go/types"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/internal/decls"
)

// minors lists the minors the table covers.
func minors() []specrules.Minor {
	return []specrules.Minor{specrules.V30, specrules.V31, specrules.V32}
}

// declaredKinds returns the Kind constants package specrules declares, read
// type-checked, so a kind added there is held to the table with no list here to
// keep in step.
func declaredKinds(t *testing.T) []specrules.Kind {
	t.Helper()
	loadedPackage, err := decls.Load("github.com/dexpace/morphic/compilers/openapi/internal/specrules")
	require.NoError(t, err)
	kind, err := loadedPackage.Named("Kind")
	require.NoError(t, err)
	var out []specrules.Kind
	for c := range loadedPackage.Objects[*types.Const]() {
		if !types.Identical(c.Type(), kind) {
			continue
		}
		v, exact := constant.Uint64Val(c.Val())
		require.True(t, exact, "%s is no small integer", c.Name())
		out = append(out, specrules.Kind(v))
	}
	require.NotEmpty(t, out, "found no kind, so this proves nothing")
	return out
}

// TestTable_EveryKindHasOneObject pins that the table covers each declared kind
// once, that Object finds it, and that it holds no object of a kind the package
// does not declare.
func TestTable_EveryKindHasOneObject(t *testing.T) {
	spec := specrules.Table()
	kinds := declaredKinds(t)
	seen := map[specrules.Kind]bool{}
	for _, object := range spec.Objects() {
		assert.False(t, seen[object.Kind], "%s is declared twice", object.Name)
		assert.Contains(t, kinds, object.Kind, "%s is of no declared kind", object.Name)
		assert.NotEmpty(t, object.Name)
		seen[object.Kind] = true
	}
	for _, kind := range kinds {
		got, ok := spec.Object(kind)
		require.True(t, ok, "kind %d has no object", kind)
		assert.Equal(t, kind, got.Kind)
	}
	assert.NotContains(t, kinds, specrules.Kind(0), "the zero kind is no object")
	_, ok := spec.Object(0)
	assert.False(t, ok, "the zero kind names no object")
}

// TestTable_EveryRowCitesTheSpecification pins that each field, default,
// pattern and rule names a sentence of a text it can have come from.
func TestTable_EveryRowCitesTheSpecification(t *testing.T) {
	valid := func(where string, cites []specrules.Cite) {
		require.NotEmpty(t, cites, where)
		for _, cite := range cites {
			assert.Contains(t, minors(), cite.Minor, where)
			assert.NotEmpty(t, cite.Section, where)
			assert.NotEmpty(t, cite.Sentence, where)
		}
	}
	for _, object := range specrules.Table().Objects() {
		for _, field := range object.Fields {
			valid(object.Name+"."+field.Name, field.Cites)
			assert.True(t, slices.ContainsFunc(field.Cites, func(cite specrules.Cite) bool { return field.Defined.Holds(cite.Minor) }),
				"%s.%s: no citation comes from a minor that defines the field", object.Name, field.Name)
			for _, within := range field.Within {
				valid(object.Name+"."+field.Name+" within "+within.MediaType, within.Cites)
				assert.True(t, slices.ContainsFunc(within.Cites, func(cite specrules.Cite) bool { return within.Span.Holds(cite.Minor) }),
					"%s.%s: no citation of %s comes from a minor it holds at", object.Name, field.Name, within.MediaType)
			}
		}
		for _, defaultValue := range object.Defaults {
			valid(object.Name+"."+defaultValue.Field+" default", defaultValue.Cites)
			assert.True(t, slices.ContainsFunc(defaultValue.Cites, func(cite specrules.Cite) bool { return defaultValue.Span.Holds(cite.Minor) }),
				"%s.%s: no citation of a default comes from a minor it holds at", object.Name, defaultValue.Field)
		}
		for _, pattern := range object.Patterned {
			valid(object.Name+"."+pattern.Name, pattern.Cites)
		}
		for i, rule := range object.Rules {
			valid(object.Name+" rule "+itoa(i), rule.Cites)
		}
	}
}

// itoa spells a small number for a failure message.
func itoa(n int) string { return string(rune('0' + n)) }

// TestTable_SpansAreWellFormed pins every span the table states.
func TestTable_SpansAreWellFormed(t *testing.T) {
	for _, object := range specrules.Table().Objects() {
		assert.True(t, validSpan(object.Referenceable), object.Name)
		assert.True(t, validSpan(object.Boolean), object.Name)
		assert.True(t, validSpan(object.RefSiblings), object.Name)
		for _, field := range object.Fields {
			where := object.Name + "." + field.Name
			assert.NotZero(t, field.Defined, where+": a field is defined at some minor")
			for _, minorVersionRange := range []specrules.MinorVersionRange{field.Defined, field.Required, field.NonEmpty} {
				assert.True(t, validSpan(minorVersionRange), where)
			}
			for _, minor := range minors() {
				if field.Required.Holds(minor) {
					assert.True(t, field.Defined.Holds(minor), "%s is required at %s, where it is not defined", where, minor)
				}
			}
			for _, value := range field.Values {
				assert.True(t, validSpan(value.Span) && value.Span != (specrules.MinorVersionRange{}), where+" value "+value.Name)
			}
			for _, a := range field.Alts {
				assert.True(t, validSpan(a.Span), where)
			}
			withinIsWellFormed(t, where, field)
		}
		defaultsAreWellFormed(t, object)
		for i, pattern := range object.Patterned {
			assert.True(t, validSpan(pattern.Span), object.Name+"."+pattern.Name)
			for _, patternValue := range object.Patterned[i+1:] {
				assert.False(t, overlaps(pattern.Span, patternValue.Span), "%s admits two patterns at one minor", object.Name)
			}
		}
		for i, rule := range object.Rules {
			assert.True(t, validSpan(rule.Span), object.Name+" rule "+itoa(i))
			assert.NotZero(t, rule.Span, object.Name+" rule "+itoa(i)+" never applies")
		}
	}
}

// withinIsWellFormed holds each media type field takes effect under to a span
// that holds somewhere, inside the minors defining field, and to a name with no
// parameters, in lower case, as a holder normalizes the one it asks about, and
// written once at each minor.
func withinIsWellFormed(t *testing.T, where string, field specrules.Field) {
	t.Helper()
	for i, within := range field.Within {
		assert.True(t, validSpan(within.Span) && within.Span != (specrules.MinorVersionRange{}), where+" within "+within.MediaType)
		assert.Equal(t, strings.ToLower(strings.TrimSpace(within.MediaType)), within.MediaType, where)
		assert.NotContains(t, within.MediaType, ";", where)
		for _, minor := range minors() {
			if !within.Span.Holds(minor) {
				continue
			}
			assert.True(t, field.Defined.Holds(minor), "%s takes effect under %s at %s, where it is not defined", where, within.MediaType, minor)
			for _, other := range field.Within[i+1:] {
				assert.False(t, other.MediaType == within.MediaType && other.Span.Holds(minor),
					"%s names %s twice at %s", where, within.MediaType, minor)
			}
		}
	}
}

// defaultsAreWellFormed holds each default of object to a field object defines
// at every minor the default holds at, and the defaults of one field and case
// to disjoint minors, so at each minor a case has one value.
func defaultsAreWellFormed(t *testing.T, object specrules.Object) {
	t.Helper()
	defined := func(name string, minor specrules.Minor) bool {
		return slices.ContainsFunc(object.Fields, func(field specrules.Field) bool { return field.Name == name && field.Defined.Holds(minor) })
	}
	for i, defaultValue := range object.Defaults {
		where := object.Name + "." + defaultValue.Field + " default"
		assert.True(t, validSpan(defaultValue.Span) && defaultValue.Span != (specrules.MinorVersionRange{}), where)
		assert.NotZero(t, defaultValue.Case, where+" names no case")
		for _, minor := range minors() {
			if defaultValue.Span.Holds(minor) {
				assert.True(t, defined(defaultValue.Field, minor), "%s holds at %s, where the object defines no such field", where, minor)
			}
			for _, otherDefault := range object.Defaults[i+1:] {
				assert.False(t, otherDefault.Field == defaultValue.Field && otherDefault.Case == defaultValue.Case && otherDefault.Span.Holds(minor) && defaultValue.Span.Holds(minor),
					"%s has two values of one case at %s", where, minor)
			}
		}
	}
}

// TestTable_FieldNamesDoNotCollide pins that an object names a field once at
// each minor: two rows of one name must be defined at disjoint minors.
func TestTable_FieldNamesDoNotCollide(t *testing.T) {
	for _, object := range specrules.Table().Objects() {
		for i, a := range object.Fields {
			for _, b := range object.Fields[i+1:] {
				if a.Name == b.Name {
					assert.False(t, overlaps(a.Defined, b.Defined), "%s.%s is defined twice at one minor", object.Name, a.Name)
				}
			}
		}
	}
}

// TestTable_ContainersNameTheKindTheyHold pins that a field holding objects
// names an object the table has, and that no other field names a kind.
func TestTable_ContainersNameTheKindTheyHold(t *testing.T) {
	spec := specrules.Table()
	holds := func(where string, shape specrules.Shape, of specrules.Kind) {
		if shape == specrules.Nested || shape == specrules.Map || shape == specrules.List {
			_, ok := spec.Object(of)
			assert.True(t, ok, "%s holds a kind with no object", where)
			return
		}
		assert.Zero(t, of, "%s names a kind though it holds none", where)
	}
	for _, object := range spec.Objects() {
		for _, field := range object.Fields {
			holds(object.Name+"."+field.Name, field.Shape, field.Of)
		}
		for _, pattern := range object.Patterned {
			holds(object.Name+"."+pattern.Name, pattern.Shape, pattern.Of)
		}
	}
}

// TestTable_RulesNameFieldsTheObjectHas pins that a rule reads fields its
// object defines, and that a Code rule is either named or owned.
func TestTable_RulesNameFieldsTheObjectHas(t *testing.T) {
	for _, object := range specrules.Table().Objects() {
		has := func(name string) bool {
			return name == "" || name == "$key" || slices.ContainsFunc(object.Fields, func(field specrules.Field) bool { return field.Name == name })
		}
		for i, rule := range object.Rules {
			where := object.Name + " rule " + itoa(i)
			names := []string{rule.Field, rule.Of}
			if rule.Kind != specrules.RuleForbiddenWhen {
				names = append(names, rule.Fields...) // a ban may name fields the object does not define
			}
			for _, name := range names {
				assert.True(t, has(name), "%s names the field %q, which the object lacks", where, name)
			}
			for _, term := range rule.When {
				assert.True(t, has(term.Field), "%s tests the field %q, which the object lacks", where, term.Field)
				if !term.Written {
					assert.NotEmpty(t, term.In, where)
				}
			}
			if rule.Kind == specrules.RuleCode {
				assert.NotEmpty(t, rule.Name, where)
			}
		}
	}
}

// TestTable_OnlyReferenceAndRequirementsTakeNoExtensions pins which objects the
// specification bars from carrying x- keys.
func TestTable_OnlyReferenceAndRequirementsTakeNoExtensions(t *testing.T) {
	var closed []string
	for _, object := range specrules.Table().Objects() {
		if !object.Extensible {
			closed = append(closed, object.Name)
		}
	}
	assert.Equal(t, []string{"Reference Object", "Security Requirement Object"}, closed)
}

// TestTable_OwnedRulesNameTheirOwners pins which packages the table defers to.
func TestTable_OwnedRulesNameTheirOwners(t *testing.T) {
	owners := map[string]bool{}
	for _, object := range specrules.Table().Objects() {
		for _, rule := range object.Rules {
			if rule.Owner != "" {
				owners[rule.Owner] = true
			}
		}
		for _, field := range object.Fields {
			if field.Owner != "" {
				owners[field.Owner] = true
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
	for _, object := range specrules.Table().Objects() {
		if object.Referenceable.Since > specrules.V30 {
			rows = append(rows, laterRow{object.Name, "$ref", "", object.Referenceable.Since.String()})
		}
		for _, field := range object.Fields {
			if field.IsJSONSchemaKeyword {
				continue
			}
			if field.Defined.Since > specrules.V30 {
				rows = append(rows, laterRow{object.Name, field.Name, "", field.Defined.Since.String()})
			}
			for _, value := range field.Values {
				if value.Span.Since > specrules.V30 {
					rows = append(rows, laterRow{object.Name, field.Name, value.Name, value.Span.Since.String()})
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
