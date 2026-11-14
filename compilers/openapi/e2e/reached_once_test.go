// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// allowedValues is the code of a value outside the values its field admits.
const allowedValues = diag.Validation + "/validation-allowed-values"

// nestedSource is #571's document: a path item and a parameter inside it, both
// reached by a $ref, the item as a path item and the parameter as a parameter.
// firstA is set where /a, which reaches the container, is written before /b,
// which reaches what it holds. param is the parameter object written at
// x-lib/item/get/parameters/0, and exactly 220 nodes with the one the issue gives.
func nestedSource(firstA bool, param string) string {
	a := "  /a: {$ref: '#/x-lib/item'}\n"
	b := "  /b:\n    get:\n      operationId: b\n      parameters:\n        - {$ref: '#/x-lib/item/get/parameters/0'}\n" +
		"      responses: {\"200\": {description: ok}}\n"
	paths := a + b
	if !firstA {
		paths = b + a
	}
	return "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n" + paths +
		"x-lib:\n  item:\n    get:\n      operationId: a\n      parameters:\n        - " + param + "\n" +
		"      responses: {\"200\": {description: ok}}\n"
}

// nestedParam is a parameter of an object schema of forty properties.
func nestedParam() string {
	props := make([]string, 40)
	for i := range props {
		props[i] = fmt.Sprintf("p%d: {type: string}", i)
	}
	return "{name: p, in: query, schema: {type: object, properties: {" + strings.Join(props, ", ") + "}}}"
}

// nestedLimits is the node budget the container and its content cost together
// twice over, and no alias surplus.
func nestedLimits() openapi.Options {
	return openapi.Options{Limits: openapi.Limits{MaxSourceNodes: 230, MaxAliasSurplus: 1}}
}

// TestCompile_AnObjectReachedInsideAContainerAndAsTheContainerIsCheckedOnce holds
// a document whose content is reached twice, as a parameter and inside the path
// item that holds it, to the budget its nodes are read within once: it validates
// at 230 nodes, whichever of the two references is written first.
func TestCompile_AnObjectReachedInsideAContainerAndAsTheContainerIsCheckedOnce(t *testing.T) {
	t.Parallel()
	for name, firstA := range map[string]bool{"container first": true, "content first": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, diags := compileText(t, "spec.yaml", nestedSource(firstA, nestedParam()), nestedLimits())

			require.NotNil(t, doc, "%+v", diags)
			assert.Empty(t, diags, "a charge for what was already read is not the document's cost")
		})
	}
}

// TestCompile_AFindingOnAnObjectReachedTwiceIsReportedOnceAtItsLeastSite holds
// the object checked once to reporting once: a parameter written with an
// explode that is no boolean, reached inside its container and on its own, in
// either order, is one finding at the one place it is written.
func TestCompile_AFindingOnAnObjectReachedTwiceIsReportedOnceAtItsLeastSite(t *testing.T) {
	t.Parallel()
	for name, firstA := range map[string]bool{"container first": true, "content first": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, diags := compileText(t, "spec.yaml", nestedSource(firstA, "{name: p, in: query, explode: 7}"), openapi.Options{})

			var explode []string
			for _, d := range byCode(diags, typeMismatch) {
				if strings.HasSuffix(string(d.Provenance.Pointer), "/explode") {
					explode = append(explode, string(d.Provenance.Pointer))
				}
			}
			assert.Equal(t, []string{"/x-lib/item/get/parameters/0/explode"}, explode, "%+v", diags)
		})
	}
}

// TestCompile_AnExtensionInsideAContainerIsStillReadAsTheKindASecondReferenceAsks
// holds a container checked as a path item not to cover what a reference reads
// from an extension inside it as another kind: the parameter that `x-inner`
// holds is checked as a parameter when a $ref names it, in either order.
func TestCompile_AnExtensionInsideAContainerIsStillReadAsTheKindASecondReferenceAsks(t *testing.T) {
	t.Parallel()
	const item = "  item:\n    get:\n      operationId: a\n      responses: {\"200\": {description: ok}}\n    x-inner: {name: q, in: bogus}\n"
	const viaItem = "  /a: {$ref: '#/x-lib/item'}\n"
	const viaInner = "  /b:\n    get:\n      operationId: b\n      parameters:\n        - {$ref: '#/x-lib/item/x-inner'}\n" +
		"      responses: {\"200\": {description: ok}}\n"
	for name, paths := range map[string]string{"item first": viaItem + viaInner, "inner first": viaInner + viaItem} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n" + paths + "x-lib:\n" + item

			_, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

			assert.Equal(t, []string{"/x-lib/item/x-inner/in"}, codedPointers(diags, allowedValues), "%+v", diags)
		})
	}
}

// brokenRefs is #1877's document: a reference whose target the parser faults on,
// beside references that are valid.
const brokenRefs = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - $ref: '#/x-lib/broken'
      responses:
        "200": {description: ok}
  /b:
    get:
      parameters:
        - $ref: '#/x-lib/good'
      responses:
        "200": {$ref: '#/x-lib/resp'}
x-lib:
  broken: {$ref: ~}
  good: {name: p, in: query, schema: {type: string}}
  resp: {description: ok}
`

// brokenMediaType is a 3.2 document whose content entry names a media type
// whose $ref is null.
const brokenMediaType = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/M'}
components:
  mediaTypes:
    M: {schema: {type: string}, $ref: ~}
`

// TestCompile_AFaultingTargetCostsOnlyItsReference holds a reference whose
// target the parser faults on to costing that reference alone: every other
// reference of the document lowers as it does without it, and the compile ends
// in findings, not a refusal of the compiler.
func TestCompile_AFaultingTargetCostsOnlyItsReference(t *testing.T) {
	t.Parallel()

	doc, diags := compileText(t, "later.yaml", brokenRefs, openapi.Options{})

	require.NotNil(t, doc, "%+v", diags)
	var errs int
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			errs++
		}
	}
	assert.Positive(t, errs, "the broken reference is reported: %+v", diags)
	assert.Empty(t, byCode(diags, diag.UnresolvedRef), "no reference is reported lost to a fault: %+v", diags)
	byID := map[string]ir.Operation{}
	for _, op := range allOperations(doc) {
		byID[string(op.ID)] = op
	}
	a, b := byID["op/openapi/paths/~1a/get"], byID["op/openapi/paths/~1b/get"]
	assert.Empty(t, a.Params, "the faulting parameter lowers to none")
	require.Len(t, b.Params, 1, "/b keeps the parameter it references")
	assert.Equal(t, "p", b.Params[0].Name.Source)
	assert.Len(t, b.Responses, 1, "and its response")
}

// TestCompile_AMediaTypeWhoseRefIsNullIsAFindingAndNotAFault holds the second
// case of the same cost: a media type the parser faults on ends the compile in
// findings, and the document is not refused as a broken compiler.
func TestCompile_AMediaTypeWhoseRefIsNullIsAFindingAndNotAFault(t *testing.T) {
	t.Parallel()

	_, diags := compileText(t, "media.yaml", brokenMediaType, openapi.Options{})

	var errs int
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			errs++
		}
	}
	assert.Positive(t, errs, "%+v", diags)
}
