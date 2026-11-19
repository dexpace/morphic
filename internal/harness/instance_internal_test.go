// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// Where instanceSpec writes its defaults.
const (
	carriedA     = "property/openapi/components/schemas/C/properties/a"
	carriedB     = "property/openapi/components/schemas/C/properties/b"
	carriedParam = "parameter/openapi/paths/~1c/get/parameters/q/query"
)

// instanceSpec writes a property a defaulting to "x", a property b defaulting
// to b, both of type string, and a query parameter q of type string
// defaulting to "p".
func instanceSpec(b string) []byte {
	return fmt.Appendf(nil, `{"openapi": "3.1.0", "info": {"title": "t", "version": "1"},
  "paths": {"/c": {"get": {"parameters": [{"name": "q", "in": "query", "schema": {"type": "string", "default": "p"}}],
    "responses": {"200": {"description": "ok"}}}}},
  "components": {"schemas": {"C": {"type": "object", "properties": {
    "a": {"type": "string", "default": "x"}, "b": {"type": "string", "default": %s}}}}}}`, b)
}

// instanceCase is a one-case product writing data with instances.
func instanceCase(data []byte, instances ...Instance) fake {
	return fake{axes: []Axis{coreAxis("case", "c")}, write: func([]int) (CaseDocument, bool) {
		return CaseDocument{Sources: []compilers.Source{{Path: "openapi.json", Data: data}}, Instances: instances}, true
	}}
}

// instanceVerdict is the instance oracle's verdict on fakeValue's one case.
func instanceVerdict(t *testing.T, fakeValue fake) Verdict {
	t.Helper()
	res, ok := CheckCase(t.Context(), fakeValue, []int{0})
	require.True(t, ok)
	require.False(t, res.Faulted(), "%s: %s", res.Outcome, res.Detail)
	return res.Verdicts[2]
}

// TestAdmitted_FindsTheInstancesTheirTypeExcludes asks a string property and
// parameter: instances inside the type pass; an instance marked valid outside
// it is found, named alone, the others' carriers drawing no finding; an
// invalid instance is not asked.
func TestAdmitted_FindsTheInstancesTheirTypeExcludes(t *testing.T) {
	inside := []Instance{{Node: carriedA, Valid: true, Name: "a string"}, {Node: carriedParam, Valid: true, Name: "q"}}
	verdict := instanceVerdict(t, instanceCase(instanceSpec(`"y"`), inside...))
	assert.Equal(t, passed(OracleInstance), verdict)

	outside := slices.Concat(inside, []Instance{{Node: carriedB, Valid: true, Name: "one\tnumber"}})
	verdict = instanceVerdict(t, instanceCase(instanceSpec(`1`), outside...))
	assert.Equal(t, found(OracleInstance, `"one\tnumber"`+"\n"+
		`"one\tnumber": default is a number, and type/primitive/string holds a string`), verdict)

	notAsked := slices.Concat(inside, []Instance{{Node: carriedB, Valid: false, Name: "one"}})
	verdict = instanceVerdict(t, instanceCase(instanceSpec(`1`), notAsked...))
	assert.Equal(t, passed(OracleInstance), verdict, "an invalid instance is not asked")
}

// TestAdmitted_DeclinesWhatItCannotAsk pins each decline: no valid instance,
// an error diagnostic, and an instance its carrier does not hold as a value,
// here the compile dropping b's default as a compiler keeping it verbatim
// would.
func TestAdmitted_DeclinesWhatItCannotAsk(t *testing.T) {
	data := instanceSpec(`1`)
	none := instanceCase(data, Instance{Node: carriedB, Valid: false, Name: "one"})
	assert.Equal(t, declined(OracleInstance, DeclineNoInstance), instanceVerdict(t, none))

	refused := instanceCase([]byte(`{"openapi": "3.1.0", "info": {"title": "t", "version": "1"}, "paths": {},
		"components": {"schemas": {"C": {"type": "object", "properties": {"a": {"$ref": "#/components/schemas/X",
		"default": 1}}}}}}`), Instance{Node: carriedA, Valid: true, Name: "one"})
	assert.Equal(t, declined(OracleInstance, DeclineErrorDiagnostic), instanceVerdict(t, refused))

	orig := compileSources
	t.Cleanup(func() { compileSources = orig })
	compileSources = func(ctx context.Context, sources []compilers.Source, options map[string]string,
		ps []pass.Pass,
	) (*ir.Document, []ir.Diagnostic, error) {
		doc, diags, err := orig(ctx, sources, options, ps)
		require.NoError(t, err)
		model, ok := doc.Types["type/openapi/components/schemas/C"].(*ir.Model)
		require.True(t, ok)
		for i := range model.Properties {
			model.Properties[i].Default = nil
		}
		return doc, diags, nil
	}
	kept := instanceCase(data, Instance{Node: carriedB, Valid: true, Name: "one"})
	assert.Equal(t, declined(OracleInstance, DeclineNotJudged), instanceVerdict(t, kept))
}

// TestAdmitted_ACompileErrorEndsTheCase pins a Go error compiling a case as
// the case's fault, not a verdict on the source.
func TestAdmitted_ACompileErrorEndsTheCase(t *testing.T) {
	swapCompile(t, func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, errors.New("the engine is misconfigured")
	})
	res, ok := CheckCase(t.Context(), instanceCase(nil, Instance{Node: carriedA, Valid: true, Name: "a"}), []int{0})
	require.True(t, ok)
	assert.True(t, res.Faulted())
	assert.Equal(t, OutcomePanic, res.Outcome)
	assert.Contains(t, res.Detail, "harness: compiling case [0]: the engine is misconfigured")
}

// kindSpec writes the components schemas, and a model C holding props.
func kindSpec(schemas, props string) []byte {
	return fmt.Appendf(nil, `{"openapi": "3.1.0", "info": {"title": "t", "version": "1"}, "paths": {},
  "components": {"schemas": {%s, "C": {"type": "object", "properties": {%s}}}}}`, schemas, props)
}

// Where kindSpec's defaults are: the property t of C, and the property m of O
// in the row that writes it.
const (
	kindT = "property/openapi/components/schemas/C/properties/t"
	kindM = "property/openapi/components/schemas/O/properties/m"
)

// TestAdmitted_ExcusesAnOtherKindOnlyWhereTheKindWasInferred holds the verdict
// on a property of C defaulting to def under its schema to what the node it is
// checked against and the default's kind decide: a default of another kind
// than a node marked kind-inference is excused, and every other finding
// stands, among them one of a member, an element or a bound of a marked node,
// and any of an unmarked one. The validate pass judges no member, no element
// and no bound of the node a decorator names, so a row asking one has the
// finding drawn at the carrier.
func TestAdmitted_ExcusesAnOtherKindOnlyWhereTheKindWasInferred(t *testing.T) {
	const (
		model  = `"M": {"properties": {"a": {"type": "string"}}, "required": ["a"], "maxProperties": 1}`
		list   = `"L": {"items": {"type": "string"}, "maxItems": 1}`
		typed  = `"U": {"type": "object", "properties": {"a": {"type": "string"}}}`
		nested = `"O": {"type": "object", "properties": {"m": {"properties": {"a": {"type": "string"}}, "default": "s"}}}`
		alias  = `"A": {"allOf": [{"$ref": "#/components/schemas/L"}], "description": "d"}`
		aliasU = `"AU": {"allOf": [{"$ref": "#/components/schemas/U"}], "description": "d"}`
	)
	tests := []struct {
		name    string
		schemas string
		ref     string
		def     string
		drawn   bool   // the pass draws no finding the row asks, so the test does
		also    string // a second instance, held at the node of the nested row
		want    string // the instances found, as the detail names them
	}{
		{"marked model against a string", model, "M", `"s"`, false, "", ""},
		{"marked model against a missing member", model, "M", `{"b": 1}`, true, "", `"t"`},
		{"marked model against a bound", model, "M", `{"a": "x", "b": 2}`, true, "", `"t"`},
		{"unmarked model against a string", typed, "U", `"s"`, false, "", `"t"`},
		{"marked list against an object", list, "L", `{"a": 1}`, false, "", ""},
		{"marked list against a wrong-typed element", list, "L", `[1]`, true, "", `"t"`},
		{"marked list against a bound", list, "L", `["a", "b"]`, false, "", `"t"`},
		{"marked node nested in an unmarked one", nested, "O", `"s"`, false, kindM, `"t"`},
		{"alias of a marked list against an object", list + ", " + alias, "A", `{"a": 1}`, false, "", ""},
		{"alias of an unmarked model against a string", typed + ", " + aliasU, "AU", `"s"`, false, "", `"t"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			props := fmt.Sprintf(`"t": {"$ref": "#/components/schemas/%s", "default": %s}`, tc.ref, tc.def)
			instances := []Instance{{Node: kindT, Valid: true, Name: "t"}}
			if tc.also != "" {
				instances = append(instances, Instance{Node: tc.also, Valid: true, Name: "m"})
			}
			orig := compileSources
			t.Cleanup(func() { compileSources = orig })
			compileSources = func(ctx context.Context, sources []compilers.Source, options map[string]string,
				ps []pass.Pass,
			) (*ir.Document, []ir.Diagnostic, error) {
				doc, diags, err := orig(ctx, sources, options, ps)
				if tc.drawn {
					diags = append(diags, ir.NewDiagnostic(ir.SeverityWarning, codeOutsideType,
						"default has a member it does not hold", ir.Provenance{Node: kindT}))
				}
				return doc, diags, err
			}
			verdict := instanceVerdict(t, instanceCase(kindSpec(tc.schemas, props), instances...))
			if tc.want == "" {
				assert.Equal(t, passed(OracleInstance), verdict)
				return
			}
			assert.True(t, verdict.Found(), "%+v", verdict)
			assert.Equal(t, tc.want, strings.SplitN(verdict.Detail, "\n", 2)[0])
		})
	}
}
