// This file is a package-level suite, not a per-source-file test: it crosses
// every spelling of a discriminator mapping target with every kind of position
// it can name, so it has no single source file to pair with.
package openapi_test // external test package — exercises only the public API

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
)

// spellingsBase is the discriminated schema whose one mapping entry is target.
func spellingsBase(target string) string {
	return "    Base:\n      type: object\n      properties: {kind: {type: string}}\n" +
		"      discriminator: {propertyName: kind, mapping: {m: '" + target + "'}}\n"
}

// spellingsOthers are the other components, each a line block, which the two
// orders declare forwards and reversed around Base.
var spellingsOthers = []string{
	"    T: {type: object, properties: {t: {type: string}}, $anchor: Anch}\n",
	"    K:\n      type: object\n      $defs: {D: {type: object}}\n      properties:\n" +
		"        p: {type: object, properties: {a: {type: string}}}\n" +
		"        arr: {type: array, items: {type: object}}\n" +
		"        en: {enum: [{type: object}]}\n" +
		"        r: {$ref: '#/components/schemas/T'}\n" +
		"        h: {$ref: '#/x-lib/E'}\n",
	"    U: {oneOf: [{type: object, properties: {u: {type: string}}}, {$ref: '#/components/schemas/T'}]}\n",
}

// spellingsDoc is the source naming target from Base, with the other
// components after it or, reversed, before it.
func spellingsDoc(target string, reversed bool) string {
	schemas := append([]string{spellingsBase(target)}, spellingsOthers...)
	if reversed {
		slices.Reverse(schemas)
	}
	return "openapi: 3.1.0\ninfo: {title: G, version: \"1\"}\npaths:\n  /a:\n    get:\n      responses:\n" +
		"        \"200\":\n          description: ok\n" +
		"          content: {application/json: {schema: {type: object}}}\n  x-p: {S: {type: object}}\n" +
		"components:\n  schemas:\n" + strings.Join(schemas, "") +
		"x-lib:\n  E: {type: object, properties: {q: {type: object}}}\n"
}

// TestMappingTargets_EverySpellingAndPositionIsOrderFree pins GitHub #757 across
// the spellings a mapping target takes and the positions it names: a
// component, a property, items, union branches inline and by $ref, a pure $ref
// property, an extension's value and what is under it, an enum member, a $defs
// entry, and schemas under /paths. Each document is compiled in both orders,
// with external references off and on, and must report the same either way. A
// spelling the lowering reads as internal resolves at every position.
func TestMappingTargets_EverySpellingAndPositionIsOrderFree(t *testing.T) {
	t.Parallel()
	positions := []string{
		"/components/schemas/T", "/components/schemas/K/properties/p",
		"/components/schemas/K/properties/arr/items", "/components/schemas/U/oneOf/0",
		"/components/schemas/U/oneOf/1", "/components/schemas/K/properties/r", "/x-lib/E",
		"/x-lib/E/properties/q", "/components/schemas/K/properties/en/enum/0",
		"/components/schemas/K/$defs/D",
		"/paths/~1a/get/responses/200/content/application~1json/schema", "/paths/x-p/S",
	}
	spellings := []struct {
		name     string
		spell    func(pointer string) string
		resolves bool
	}{
		{"internal", func(p string) string { return "#" + p }, true},
		{"percent-encoded", func(p string) string { return "#" + strings.ReplaceAll(p, "~", "%7E") }, true},
		{"by file name", func(p string) string { return "root.yaml#" + p }, true},
		{"through a directory", func(p string) string { return "./root.yaml#" + p }, false},
		{"in another document", func(p string) string { return "other.yaml#" + p }, false},
		{"a component's name", func(string) string { return "T" }, true},
		{"an anchor", func(string) string { return "#Anch" }, false},
		{"the whole document", func(string) string { return "#" }, false},
		{"a relative definition", func(string) string { return "#/$defs/D" }, false},
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"),
		[]byte("x-lib: {E: {type: object}}\ncomponents: {schemas: {T: {type: object}}}\n"), 0o600))
	for _, s := range spellings {
		for _, pointer := range positions {
			target := s.spell(pointer)
			for _, external := range []bool{false, true} {
				name := fmt.Sprintf("%s %s external=%t", s.name, pointer, external)
				first := mappingDiags(t, dir, spellingsDoc(target, false), external)
				assert.Empty(t, cmp.Diff(first, mappingDiags(t, dir, spellingsDoc(target, true), external)), name)
				unresolved := slices.ContainsFunc(first, func(d string) bool {
					return strings.Contains(d, "openapi/unresolved-ref /components/schemas/Base/discriminator/mapping/m")
				})
				assert.Equal(t, !s.resolves, unresolved, "%s: %v", name, first)
			}
		}
	}
}

// mappingDiags compiles src as root.yaml in dir and returns its diagnostics as
// "severity code pointer", sorted.
func mappingDiags(t *testing.T, dir, src string, external bool) []string {
	t.Helper()
	_, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(src)}},
		compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: external}})
	require.NoError(t, err)
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}
