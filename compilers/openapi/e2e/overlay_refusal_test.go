// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// refusalSchema is a component schema S whose mapping holds the line a row of
// TestCompile_AnOverlayKeepsWhatTheSourceIsRefusedFor writes in place of the
// first %s, and a merge the source accepts or nothing in place of the second,
// beside a member and a mapping inside it the overlays write.
const refusalSchema = srMin + `x-l: &l [k]
x-s: &s 5
components:
  schemas:
    S:
      type: object
      description: d
      properties: {p: {type: string}}
      %s
      %s
`

// refusalHeaders is a response whose headers mapping holds the header the
// first %s names and the second line, beside one the overlays write.
const refusalHeaders = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          headers:
            %s: {schema: {type: string}}
            %s
            X-Ok: {schema: {type: string}}
`

// refusalPaths is a Paths Object holding the path the first %s names and the
// second line, beside one the overlays write.
const refusalPaths = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  %s: {}
  %s
  /a: {get: {responses: {"200": {description: ok}}}}
`

// TestCompile_AnOverlayKeepsWhatTheSourceIsRefusedFor holds each finding above
// info the source alone draws for a mapping, a key or a merge to standing at its
// pointer, an error still failing the compile, under an overlay writing into the
// mapping, writing or removing a member, writing inside one, or copying the
// mapping, merging or not. The rows are written by hand from what
// oas.CheckFormat, oas.CheckRepeatedKeys and the load phase's tagged-mapping and
// anchor-cycle refusals report of a mapping's YAML, so a report added there
// needs its row. A key's text is judged on the overlaid tree, so four rules of
// it stand for the rest.
func TestCompile_AnOverlayKeepsWhatTheSourceIsRefusedFor(t *testing.T) {
	t.Parallel()
	const s, h = "$.components.schemas.S", "$.paths['/a'].get.responses['200'].headers"
	type place struct{ template, merge, mapping, member, sibling, value, inside string }
	schema := place{refusalSchema, "<<: {x-m: 1}", s, "{x-new: 1}", s + ".description", "d2", s + ".properties.p"}
	headers := place{refusalHeaders, "<<: {X-M: {schema: {type: string}}}", h, "{X-New: {schema: {type: string}}}",
		h + "['X-Ok']", "{description: x}",
		h + "['X-Ok'].schema"}
	paths := place{refusalPaths, "<<: {x-m: 1}", "$.paths", "{x-new: 1}", "$.paths['/a']", "{summary: x}", "$.paths['/a'].get"}
	rows := []struct {
		name, line string
		at         place
	}{
		{"a key that is a mapping", "? {k: 1} : v", schema},
		{"a key that is a list", "? [k] : v", schema},
		{"a key that aliases a list", "? *l : v", schema},
		{"a merge of a scalar", "<<: 5", schema},
		{"a merge of an alias of a scalar", "<<: *s", schema},
		{"a merge of a list holding a scalar", "<<: [{title: m}, 5]", schema},
		{"a merge of a mapping whose key is a list", "<<: {? [k] : v}", schema},
		{"a merge of a mapping merging a scalar", "<<: {<<: 5}", schema},
		{"a merge of a list whose mapping merges a scalar", "<<: [{<<: [5]}]", schema},
		{"a key written twice", "title: a\n      title: b", schema},
		{"a key written twice through a merge", "title: a\n      <<: {title: m}\n      title: b", schema},
		{"a tagged mapping", "x-t: !t {a: 1}", schema},
		{"an anchor cycle", "x-c: &c {self: *c}", schema},
		{"a map key out of its format", `"bad name"`, headers},
		{"a header the specification reserves", "Content-Type", headers},
		{"a header named twice in another case", "x-dup: {schema: {type: string}}\n            X-Dup", headers},
		{"a patterned key out of its format", "foo", paths},
	}
	for _, row := range rows {
		for name, beside := range map[string]string{"as written": "", "beside a merge": row.at.merge} {
			t.Run(row.name+" "+name, func(t *testing.T) {
				t.Parallel()
				spec := fmt.Sprintf(row.at.template, row.line, beside)
				_, alone := compileText(t, "spec.yaml", spec, openapi.Options{})
				want := findingsAbove(alone)
				require.NotEmpty(t, want, "the source alone draws the finding: %+v", alone)
				for kind, actions := range overlaysAround(row.at.mapping, row.at.member, row.at.sibling, row.at.value, row.at.inside) {
					patch := "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n" + actions
					_, diags := compileWithPatch(t, spec, patch)

					got := findingsAbove(diags)
					for _, f := range []string{diag.OverlayFailed, diag.OverlayInvalid} {
						assert.NotContains(t, codesOf(diags), f, "%s: %+v", kind, diags)
					}
					for f := range want {
						assert.Contains(t, got, f, "%s keeps the source's finding: %+v", kind, diags)
					}
					assert.Equal(t, ir.HasError(alone), ir.HasError(diags), "%s fails as the source does", kind)
				}
			})
		}
	}
}

// overlaysAround returns the actions of each overlay
// TestCompile_AnOverlayKeepsWhatTheSourceIsRefusedFor applies, by what it does
// to the mapping: member is written into it, value at sibling, a member beside
// the refused one, and inside is a mapping within a member.
func overlaysAround(mapping, member, sibling, value, inside string) map[string]string {
	action := func(target, body string) string {
		return fmt.Sprintf("  - target: %q\n    %s\n", target, body)
	}
	return map[string]string{
		"a write into the mapping":   action(mapping, "update: "+member),
		"a write of a member beside": action(sibling, "update: "+value),
		"a write inside a member":    action(inside, "update: {description: i}"),
		"a removal of a member":      action(sibling, "remove: true"),
		"a copy of the mapping":      action("$", "update: {x-copy: {}}") + action("$['x-copy']", "copy: "+mapping),
	}
}

// findingsAbove returns the code and pointer of each finding of diags above
// info.
func findingsAbove(diags []ir.Diagnostic) map[string]bool {
	out := map[string]bool{}
	for _, d := range diags {
		if d.Severity != ir.SeverityInfo {
			out[d.Code+" "+string(d.Provenance.Pointer)] = true
		}
	}
	return out
}

// codesOf returns the code of each finding of diags.
func codesOf(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}
