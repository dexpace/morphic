// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// unheldField is a field OpenAPI 3.2 gives an Encoding Object whose content no
// model holds, and the extension key of the same length its twin moves it
// under, so every pointer and byte count through it keeps its length.
type unheldField struct {
	key, twin string
	// entry is what the field holds around one Encoding Object, written e, and
	// below is the token of that object beneath the field.
	entry func(e string) string
	below string
}

// unheldFields lists the three fields.
var unheldFields = []unheldField{
	{key: "encoding", twin: "x-coding", entry: func(e string) string { return "{g: " + e + "}" }, below: "/g"},
	{key: "prefixEncoding", twin: "x-efixEncoding", entry: func(e string) string { return "[" + e + "]" }, below: "/0"},
	{key: "itemEncoding", twin: "x-emEncoding", entry: func(e string) string { return e }, below: ""},
}

// twinPlace is where one document of a pair writes the field: its key as
// spelled there, and the pointers a row's references are written from.
type twinPlace struct {
	f   unheldField
	key string
	// deep puts the header one nested Encoding Object further beneath the field.
	deep bool
}

// encodingAt is the Encoding Object of a media type that writes the field.
const encodingAt = "/components/requestBodies/B/content/multipart~1form-data/encoding/f"

// partSchema is the schema of the part encodingAt encodes, declaring the
// property each nested Encoding Object is keyed by.
const partSchema = "{type: object, properties: {g: {type: object, properties: {h: {type: string}}}}}"

// field is the pointer to the field from the Encoding Object holding it.
func (p twinPlace) field() string { return "/" + p.key }

// header is the pointer to the header beneath the field, from the Encoding
// Object holding it.
func (p twinPlace) header() string {
	h := p.field() + p.f.below
	if p.deep {
		h += "/encoding/h"
	}
	return h + "/headers/H"
}

// escaped is the field's key with its third byte percent-escaped, as a $ref
// may spell it.
func (p twinPlace) escaped() string {
	return p.key[:2] + fmt.Sprintf("%%%02X", p.key[2]) + p.key[3:]
}

// encoding writes the Encoding Object holding the field, its header written as
// hdr.
func (p twinPlace) encoding(hdr string) string {
	e := "{headers: {H: " + hdr + "}}"
	if p.deep {
		e = "{encoding: {h: " + e + "}}"
	}
	return "{contentType: multipart/mixed, " + p.key + ": " + p.f.entry(e) + "}"
}

// twinDoc is what one row writes: the source's parts, and another document's
// content where it reads one.
type twinDoc struct {
	// schemas are written under components/schemas, beside T, the type the
	// rows' references out name.
	schemas, headers, responses, top string
	// header is the header beneath the field; schema its schema's keywords
	// beside its type and properties, and props its properties beside p.
	header, schema, props string
	// encoding replaces the Encoding Object holding the field where set, and
	// other is other.yaml.
	encoding, other string
}

// source writes the source d makes at p.
func (d twinDoc) source(p twinPlace) string {
	schema := "{type: object, properties: {p: {type: string}" + d.props + "}" + d.schema + "}"
	header := d.header
	if header == "" {
		header = "{schema: " + schema + "}"
	}
	encoding := d.encoding
	if encoding == "" {
		encoding = p.encoding(header)
	}
	return "openapi: 3.2.0\ninfo: {title: t, version: '1'}\npaths:\n  /a: {post: {requestBody: {$ref: " +
		"'#/components/requestBodies/B'}, responses: {'200': {$ref: '#/components/responses/Ok'}}}}\n" + d.top +
		"x-raw: {type: object, properties: {r: {type: string}}}\n" +
		"components:\n  schemas:\n    T: {type: string}\n" + d.schemas +
		"  headers:\n    X: {schema: {type: integer}}\n" + d.headers +
		"  responses:\n    Ok: {description: ok}\n" + d.responses +
		"  requestBodies:\n    B: {content: {multipart/form-data: {schema: {type: object, properties: {f: " +
		partSchema + "}}, encoding: {f: " + encoding + "}}}}\n"
}

// twinRow is one form of reference into or out of a schema beneath the field,
// with the answers its pair may differ in: each a line prefix, the pointers
// a row names spelled as the nested document spells them.
type twinRow struct {
	name string
	doc  func(p twinPlace) twinDoc
	// nestedOnly and twinOnly are the prefixes of the answers only the nested
	// document, or only its twin, gives, each the rule's.
	nestedOnly, twinOnly func(p twinPlace) []string
}

// none states that a pair gives the same answers.
func none(twinPlace) []string { return nil }

// rawField is what every nested document reports and no twin does: the field,
// kept under no_ir_home at an info finding
// (docs/compilers/openapi.md#unmodeled-dialect-fields).
func rawField(p twinPlace) []string {
	return []string{"diag info openapi/degraded-construct 0" + encodingAt + p.field() + ":",
		"unmodeled openapi:encoding/f" + p.field() + " no_ir_home"}
}

// rawExtension is what every twin keeps and no nested document does: the
// field, kept as an extension.
func rawExtension(p twinPlace) []string {
	return []string{"unmodeled openapi:encoding/f" + p.field() + " vendor_extension"}
}

// in is the source's $ref to the schema beneath the field.
func in(p twinPlace) string {
	return "    In: {$ref: '#" + encodingAt + p.header() + "/schema'}\n"
}

// twinRows are the forms of reference the sweep writes at each field and
// depth.
var twinRows = []twinRow{
	{name: "a $ref in", doc: func(p twinPlace) twinDoc { return twinDoc{schemas: in(p)} }},
	{name: "a $ref in below the schema", doc: func(p twinPlace) twinDoc {
		return twinDoc{schemas: "    In: {$ref: '#" + encodingAt + p.header() + "/schema/properties/p'}\n"}
	}},
	{
		name: "a $ref in to YAML shaped as no schema", doc: func(p twinPlace) twinDoc {
			return twinDoc{schemas: in(p), header: "{schema: text}"}
		},
		// The rule table judges the header the grammar places beneath the
		// field, as it judges each object it gives a document
		// (unmodeled-dialect-fields, validation), where an extension's value
		// is judged by no row.
		nestedOnly: func(p twinPlace) []string {
			return []string{"diag error openapi/validation/validation-type-mismatch 0" + encodingAt + p.header() +
				"/schema:"}
		},
		twinOnly: none,
	},
	{name: "a $ref out", doc: func(p twinPlace) twinDoc {
		return twinDoc{schemas: in(p), props: ", o: {$ref: '#/components/schemas/T'}"}
	}},
	{name: "a $ref out into raw YAML", doc: func(p twinPlace) twinDoc {
		return twinDoc{schemas: in(p), props: ", o: {$ref: '#/x-raw'}"}
	}},
	{name: "a $dynamicRef in", doc: func(p twinPlace) twinDoc {
		return twinDoc{schemas: "    In: {$dynamicRef: '#" + encodingAt + p.header() + "/schema'}\n"}
	}},
	{name: "a $dynamicRef out", doc: func(p twinPlace) twinDoc {
		return twinDoc{schemas: in(p), props: ", y: {$dynamicRef: '#/components/schemas/T'}"}
	}},
	{name: "a $defs pointer", doc: func(p twinPlace) twinDoc {
		return twinDoc{schemas: in(p), props: ", d: {$ref: '#/$defs/n'}", schema: ", $defs: {n: {type: integer}}"}
	}},
	{
		name: "a $defs pointer beneath an $id", doc: func(p twinPlace) twinDoc {
			return twinDoc{schemas: in(p), props: ", d: {$ref: '#/$defs/n'}",
				schema: ", $id: 'https://x.test/h', $defs: {n: {type: integer}}"}
		},
		// "Identifiers are read only where a schema is placed" (references):
		// the grammar places the schema, so its $id makes a resource the
		// pointer is read in, where an extension's makes none and the $defs
		// rule finds raw YAML above.
		nestedOnly: definedInResource, twinOnly: definedInRaw,
	},
	{
		name: "a draft 7 $schema naming its dialect", doc: func(p twinPlace) twinDoc {
			return twinDoc{schemas: "    In: {$ref: '#nested'}\n",
				schema: ", $schema: 'http://json-schema.org/draft-07/schema#', $id: '#nested'"}
		},
		// "A schema another holds roots a resource ... as the dialect its own
		// `$schema` names does", and "In drafts 4, 6 and 7 one naming a fragment
		// alone, `#name`, declares that plain name" (references): the compile
		// reads the `$schema` of the schema the grammar places, so In lowers it,
		// where an extension's names nothing and In is unresolved. The binder
		// above is handed no Config.Dialect, so it reads both as 2020-12 does.
		nestedOnly: draftIdentifiersKept, twinOnly: draftAnchorUnresolved,
	},
	{name: "an escaped pointer token", doc: func(p twinPlace) twinDoc {
		esc := strings.Replace(p.header(), p.key, p.escaped(), 1)
		return twinDoc{schemas: "    In: {$ref: '#" + encodingAt + esc + "/schema'}\n"}
	}},
	{name: "through an alias", doc: func(p twinPlace) twinDoc {
		return twinDoc{schemas: in(p), top: "x-anchors: {h: &h {schema: {type: object, properties: {p: {type: string}}}}}\n",
			header: "*h"}
	}},
	{name: "through a merge key", doc: func(p twinPlace) twinDoc {
		e := p.encoding("{schema: {type: object, properties: {p: {type: string}}}}")
		return twinDoc{schemas: in(p), top: "x-anchors: {e: &e " + e + "}\n", encoding: "{<<: *e}"}
	}},
	{name: "through a reference whose target is raw YAML", doc: func(p twinPlace) twinDoc {
		return twinDoc{
			top: "x-resp: {description: d, content: {multipart/form-data: {encoding: {f: " +
				p.encoding("{schema: {type: object}}") + "}}}}\n",
			responses: "    RRef: {$ref: '#/x-resp'}\n",
			schemas: "    In: {$ref: '#/components/responses/RRef/content/multipart~1form-data/encoding/f" +
				p.header() + "/schema'}\n",
		}
	}},
	{name: "to the field's parent", doc: func(twinPlace) twinDoc {
		return twinDoc{schemas: "    In: {$ref: '#" + encodingAt + "'}\n"}
	}},
	{
		name: "to the field", doc: func(p twinPlace) twinDoc {
			return twinDoc{schemas: "    In: {$ref: '#" + encodingAt + p.field() + "'}\n"}
		},
		// The field is an object or a list of them the grammar places, which
		// holds no schema, where under an extension it is raw YAML a $ref
		// reads as one ("The binder places each field the rule table gives an
		// object outside a schema", unmodeled-dialect-fields).
		nestedOnly: listRefused, twinOnly: listLanded,
	},
	{
		name: "to an Encoding Object the field holds", doc: func(p twinPlace) twinDoc {
			return twinDoc{schemas: "    In: {$ref: '#" + encodingAt + p.field() + p.f.below + "'}\n"}
		},
		// An Encoding Object the field holds is an object the grammar places,
		// as the field is.
		nestedOnly: refusedAt, twinOnly: landedAt,
	},
	{
		name: "an $anchor", doc: func(p twinPlace) twinDoc {
			return twinDoc{schemas: "    In: {$ref: '#nested'}\n", schema: ", $anchor: nested"}
		},
		// "Identifiers are read only where a schema is placed": the grammar
		// places the nested schema, so its anchor answers, where an
		// extension's declares nothing (references).
		nestedOnly: anchorAnswers, twinOnly: anchorUnanswered,
	},
	{
		name: "a header Reference Object in", doc: func(p twinPlace) twinDoc {
			return twinDoc{headers: "    HRef: {$ref: '#" + encodingAt + p.header() + "'}\n",
				schemas: "    In: {$ref: '#/components/headers/HRef/schema'}\n"}
		},
		// The header is an object the grammar places, so the reference
		// lands on a header, where under an extension it lands on raw YAML
		// the load phase builds one from ("raw YAML ... is read as the kind
		// it is reached as", references).
		nestedOnly: headerHopAt, twinOnly: rawHeaderAt,
	},
	{
		name: "a header Reference Object out", doc: func(p twinPlace) twinDoc {
			return twinDoc{header: "{$ref: '#/components/headers/X'}",
				schemas: "    In: {$ref: '#" + encodingAt + p.header() + "/schema'}\n"}
		},
		// "A `$ref` written beneath such a nested Encoding Object, a
		// schema's or a Reference Object's, is bound and reported", and "A
		// pointer passing a reference object ... reads on where that
		// reference's chain ends" (references); under an extension neither.
		nestedOnly: readThroughHeader, twinOnly: unreadHeader,
	},
	{
		name: "a header Reference Object to the schema", doc: func(p twinPlace) twinDoc {
			return twinDoc{headers: "    HRef: {$ref: '#" + encodingAt + p.header() + "/schema'}\n",
				header: "{schema: {description: d, schema: {type: integer}}}"}
		},
		// A reference object "lands on an object of its own kind: a position
		// the source's grammar reads as another kind, a schema, ... holds
		// none, and raw YAML, as an extension's value ..., is read as the kind
		// it is reached as" (references): the grammar places the schema.
		nestedOnly: headerRefused, twinOnly: headerFromSchema,
	},
	{name: "a $ref in from a second document", doc: func(p twinPlace) twinDoc {
		return twinDoc{other: otherOpenAPI(p, ""),
			schemas: "    In: {$ref: 'other.yaml#" + encodingAt + p.header() + "/schema'}\n"}
	}},
	{name: "a $defs pointer in a second document", doc: func(p twinPlace) twinDoc {
		return twinDoc{other: otherOpenAPI(p, ", properties: {d: {$ref: '#/$defs/n'}}, $defs: {n: {type: integer}}"),
			schemas: "    In: {$ref: 'other.yaml#" + encodingAt + p.header() + "/schema'}\n"}
	}},
	{name: "through a reference to a second document of no grammar", doc: func(p twinPlace) twinDoc {
		return twinDoc{
			other: "description: d\ncontent: {multipart/form-data: {encoding: {f: " +
				p.encoding("{schema: {type: object}}") + "}}}\n",
			responses: "    RRef: {$ref: 'other.yaml'}\n",
			schemas: "    In: {$ref: '#/components/responses/RRef/content/multipart~1form-data/encoding/f" +
				p.header() + "/schema'}\n",
		}
	}},
}

// otherOpenAPI is an OpenAPI document holding the request body B, its header's
// schema writing extra beside its type.
func otherOpenAPI(p twinPlace, extra string) string {
	return "openapi: 3.2.0\ninfo: {title: o, version: '1'}\npaths: {}\ncomponents:\n  requestBodies:\n" +
		"    B: {content: {multipart/form-data: {encoding: {f: " + p.encoding("{schema: {type: object"+extra+"}}") +
		"}}}}\n"
}

// definedInResource is d's "#/$defs/n" read in the resource the schema's $id
// makes, landing on raw YAML.
func definedInResource(p twinPlace) []string {
	return []string{"entry spec.yaml#" + encodingAt + p.header() + "/schema/properties/d: hop resolved 0 raw",
		"landing spec.yaml#" + encodingAt + p.header() + "/schema/$defs/n"}
}

// definedInRaw is d's "#/$defs/n" read by the $defs rule, which finds the
// definition only in raw YAML.
func definedInRaw(p twinPlace) []string {
	return []string{"entry spec.yaml#" + encodingAt + p.header() + "/schema/properties/d: hop unresolved",
		"diag error openapi/unresolved-ref 0" + encodingAt + p.header() + "/schema/properties/d:"}
}

// headerRefused is HRef refused as naming no header.
func headerRefused(twinPlace) []string {
	return []string{"reference spec.yaml#/components/headers/HRef header: hop unresolved",
		"diag error openapi/unresolved-ref 0/components/headers/HRef:"}
}

// headerFromSchema is HRef landing on the raw YAML at the header's schema,
// read as a header.
func headerFromSchema(p twinPlace) []string {
	return []string{"reference spec.yaml#/components/headers/HRef header: hop resolved 0 raw",
		"object-landing spec.yaml#" + encodingAt + p.header() + "/schema header"}
}

// refusedAt is a $ref from In refused as naming no schema.
func refusedAt(twinPlace) []string {
	return []string{"entry spec.yaml#/components/schemas/In: hop unresolved",
		"diag error openapi/unresolved-ref 0/components/schemas/In:"}
}

// landedAt is a $ref from In landing on raw YAML, lowered as a schema whose
// keywords no schema defines.
func landedAt(twinPlace) []string {
	return []string{"entry spec.yaml#/components/schemas/In: hop resolved", "landing spec.yaml#" + encodingAt,
		"diag info openapi/unknown-schema-keyword 0" + encodingAt}
}

// listRefused is refusedAt where the field holds a mapping, as raw YAML a
// $ref reads as a schema: a list, prefixEncoding's, is none either way.
func listRefused(p twinPlace) []string {
	if p.f.below == "/0" {
		return nil
	}
	return refusedAt(p)
}

// listLanded is landedAt where the field holds a mapping (listRefused).
func listLanded(p twinPlace) []string {
	if p.f.below == "/0" {
		return nil
	}
	return landedAt(p)
}

// anchorAnswers is In's $ref answered by the nested schema's anchor.
func anchorAnswers(p twinPlace) []string {
	return []string{"entry spec.yaml#/components/schemas/In: hop resolved 0 raw spec.yaml#" + encodingAt +
		p.header() + "/schema", "landing spec.yaml#" + encodingAt + p.header() + "/schema"}
}

// draftIdentifiersKept is the schema In lowers reporting the identifiers it
// keeps, its $id and its $schema.
func draftIdentifiersKept(p twinPlace) []string {
	return []string{"diag info openapi/degraded-construct 0" + encodingAt + p.header() + "/schema/$"}
}

// draftAnchorUnresolved is the compile refusing In's $ref.
func draftAnchorUnresolved(twinPlace) []string {
	return []string{"diag error openapi/unresolved-ref 0/components/schemas/In:"}
}

// anchorUnanswered is In's $ref naming an anchor no schema declares.
func anchorUnanswered(p twinPlace) []string { return refusedAt(p) }

// headerHopAt is HRef landing on a header the grammar places.
func headerHopAt(twinPlace) []string {
	return []string{"reference spec.yaml#/components/headers/HRef header: hop resolved 0 object"}
}

// rawHeaderAt is HRef landing on raw YAML read as a header.
func rawHeaderAt(p twinPlace) []string {
	return []string{"reference spec.yaml#/components/headers/HRef header: hop resolved 0 raw",
		"object-landing spec.yaml#" + encodingAt + p.header() + " header"}
}

// readThroughHeader is the header Reference Object beneath the field, bound,
// and In's pointer read through it into X's schema.
func readThroughHeader(p twinPlace) []string {
	return []string{"reference spec.yaml#" + encodingAt + p.header() + " header",
		"entry spec.yaml#/components/schemas/In: hop resolved 0 schema spec.yaml#/components/headers/X/schema"}
}

// unreadHeader is In's pointer naming a key the raw mapping does not write.
func unreadHeader(p twinPlace) []string { return refusedAt(p) }

// twinAnswers is what the binder and the compiler say of one document, a line
// each, its key spelled as the nested document spells it.
func twinAnswers(t *testing.T, p twinPlace, d twinDoc, nested string) []string {
	t.Helper()
	src := d.source(p)
	files := map[string]string{"main.yaml": src}
	if d.other != "" {
		files["other.yaml"] = d.other
	}
	cfg := bind.Config{Self: "spec.yaml", Open: func(name, _ string) (*yaml.Node, error) {
		text, ok := files[name]
		if !ok {
			return nil, errors.New("no such document")
		}
		var n yaml.Node
		return &n, yaml.Unmarshal([]byte(text), &n)
	}}
	root := openapitest.YAMLDocument(t, src)
	table := bind.Bind(t.Context(), root, cfg)
	lines := bindAnswers(table)
	// An incomplete total is never read, so its count says nothing.
	if named, complete := bind.NamedPointerBytes(t.Context(), nodeview.DocumentRoot(root), 1<<20); complete {
		lines = append(lines, fmt.Sprintf("named pointer bytes %d", named))
	}

	dir := t.TempDir()
	layout(t, dir, files)
	doc, diags := compileDir(t, dir, openapi.Options{})
	lines = append(lines, "compiled "+strconv.FormatBool(doc != nil), fmt.Sprintf("charged %d", charged(t, dir)))
	lines = append(lines, irAnswers(t, doc)...)
	for _, d := range diags {
		lines = append(lines, fmt.Sprintf("diag %s %s %d%s: %s", d.Severity, d.Code, d.Provenance.Source,
			d.Provenance.Pointer, d.Message))
	}
	for i := range lines {
		lines[i] = strings.ReplaceAll(lines[i], dir, "DIR")
		lines[i] = strings.ReplaceAll(lines[i], p.key, nested)
		lines[i] = strings.ReplaceAll(lines[i], p.escaped(), twinPlace{key: nested}.escaped())
	}
	slices.Sort(lines)
	return lines
}

// irAnswers is doc as a line for each value it holds, "ir <pointer> <value>",
// and each Unmodeled entry no type holds as "unmodeled <key> <reason>". The findings and the
// source's hash are left out, the findings answered for line by line beside, and
// so is the name of a type at a header's schema, which a header beneath the
// field names (docs/compilers/openapi.md#naming) and raw YAML does not.
func irAnswers(t *testing.T, doc *ir.Document) []string {
	t.Helper()
	if doc == nil {
		return nil
	}
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	var v any
	require.NoError(t, json.Unmarshal(data, &v))
	var lines []string
	var walk func(v any, at string)
	walk = func(v any, at string) {
		switch v := v.(type) {
		case map[string]any:
			for k, c := range v {
				below := at + "/" + strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
				if e, ok := c.(map[string]any); ok && strings.HasSuffix(at, "/unmodeled") && !strings.HasPrefix(at, "/types/") {
					lines = append(lines, fmt.Sprintf("unmodeled %s %v", k, e["reason"]))
					delete(e, "reason")
				}
				walk(c, below)
			}
		case []any:
			for i, c := range v {
				walk(c, at+"/"+strconv.Itoa(i))
			}
		default:
			lines = append(lines, fmt.Sprintf("ir %s %v", at, v))
		}
	}
	m, ok := v.(map[string]any)
	require.True(t, ok)
	delete(m, "diagnostics")
	for _, src := range m["sources"].([]any) {
		delete(src.(map[string]any), "hash")
	}
	for id, def := range m["types"].(map[string]any) {
		if strings.HasSuffix(id, "/headers/H/schema") {
			delete(def.(map[string]any), "name")
		}
	}
	walk(m, "")
	return lines
}

// bindAnswers is what table says of every reference it read: each schema
// $ref's hop and chain, each landing, each reference object's hop and chain,
// and each object landing.
func bindAnswers(table *bind.Table) []string {
	at := func(p bind.Position) string { return table.Document(p.Doc).Name + "#" + string(p.Pointer) }
	hop := func(h bind.Hop) string {
		return fmt.Sprintf("hop %s %d %s %s %s | ", h.Status, h.Reason, h.Role, at(h.Target), h.Detail)
	}
	chain := func(c bind.Chain) string { return fmt.Sprintf("chain %s %s %d", c.Status, at(c.End), c.Hops) }
	lines := make([]string, 0, len(table.Entries())+len(table.Landings())+len(table.References())+
		len(table.ObjectLandings()))
	for _, e := range table.Entries() {
		lines = append(lines, "entry "+at(e.Site)+": "+hop(e.Hop)+chain(e.Chain))
	}
	for _, l := range table.Landings() {
		lines = append(lines, fmt.Sprintf("landing %s site %s reached %t", at(l.At), at(l.Site), l.Reached))
	}
	for _, r := range table.References() {
		lines = append(lines, "reference "+at(r.Site)+" "+r.Kind.String()+": "+hop(r.Hop)+chain(r.Chain))
	}
	for _, l := range table.ObjectLandings() {
		lines = append(lines, "object-landing "+at(l.At)+" "+l.Kind.String())
	}
	return lines
}

// charged is what the copies a compile of the files in dir makes weigh: the
// least reference-expansion budget it is not refused past
// (docs/compilers/openapi.md#source-budgets).
func charged(t *testing.T, dir string) int {
	t.Helper()
	within := func(limit int) bool {
		_, diags := compileDir(t, dir, openapi.Options{Limits: openapi.Limits{MaxReferenceExpansion: limit}})
		return !slices.ContainsFunc(diags, func(d ir.Diagnostic) bool { return d.Code == diag.BudgetExceeded })
	}
	refused, admitted := 0, 1
	for !within(admitted) {
		require.Less(t, admitted, 1<<16, "the copies weigh past any budget the search tries")
		refused, admitted = admitted, 2*admitted
	}
	for refused+1 < admitted {
		if mid := (refused + admitted) / 2; within(mid) {
			admitted = mid
		} else {
			refused = mid
		}
	}
	return admitted
}

// TestCompile_AFieldNoModelHoldsReadsAsAnExtension holds a schema beneath each
// field an Encoding Object nests (unheldFields), one level beneath it and two,
// to the answers the same schema gets with the field moved under an extension
// key of its length: each reference's hop and chain, each landing, what a
// compile's copies are charged (charged), its findings and its document
// (irAnswers), each as often. They differ only by how each keeps the field
// (rawField, rawExtension), the pointer bound's total (namedTotal), and where a
// row says, by the rule its comment quotes, with the types a reference read
// apart lowers to.
func TestCompile_AFieldNoModelHoldsReadsAsAnExtension(t *testing.T) {
	t.Parallel()
	for _, f := range unheldFields {
		for _, deep := range []bool{false, true} {
			for _, row := range twinRows {
				t.Run(fmt.Sprintf("%s/deep=%t/%s", f.key, deep, row.name), func(t *testing.T) {
					t.Parallel()
					nested, twin := twinPlace{f: f, key: f.key, deep: deep}, twinPlace{f: f, key: f.twin, deep: deep}
					got, gotNamed, gotTaken := namedTotal(twinAnswers(t, nested, row.doc(nested), f.key))
					want, wantNamed, wantTaken := namedTotal(twinAnswers(t, twin, row.doc(twin), f.key))
					require.Equal(t, wantTaken, gotTaken, "the pointer bound takes a total of both or neither")
					// The binder reads each node beneath the field for the references
					// written there, merged in through `<<` or written, where an
					// extension's value adds its root alone (source-budgets).
					assert.Equal(t, gotTaken, gotNamed > wantNamed,
						"the nested document names more: %d against %d", gotNamed, wantNamed)
					onlyNested, onlyTwin := row.nestedOnly, row.twinOnly
					if onlyNested == nil {
						onlyNested, onlyTwin = none, none
					}
					if len(onlyNested(nested)) > 0 && len(onlyTwin(nested)) > 0 {
						// A reference the pair reads apart may lower to types apart.
						got, want = withoutTypes(got), withoutTypes(want)
					}
					assertDiffers(t, "nested", minus(got, want), append(rawField(nested), onlyNested(nested)...))
					assertDiffers(t, "twin", minus(want, got), append(rawExtension(nested), onlyTwin(nested)...))
				})
			}
		}
	}
}

// namedTotal is lines without the pointer bound's total, the total, and
// whether lines held one.
func namedTotal(lines []string) ([]string, int, bool) {
	for i, l := range lines {
		if n, ok := strings.CutPrefix(l, "named pointer bytes "); ok {
			total, err := strconv.Atoi(n)
			return slices.Delete(slices.Clone(lines), i, i+1), total, err == nil
		}
	}
	return lines, 0, false
}

// withoutTypes is lines without those irAnswers gives of a type.
func withoutTypes(lines []string) []string {
	return slices.DeleteFunc(slices.Clone(lines), func(l string) bool { return strings.HasPrefix(l, "ir /types/") })
}

// minus is a less b, as multisets: a line a gives more often than b is kept
// as often as a gives it more.
func minus(a, b []string) []string {
	left := map[string]int{}
	for _, l := range b {
		left[l]++
	}
	var out []string
	for _, l := range a {
		if left[l] > 0 {
			left[l]--
			continue
		}
		out = append(out, l)
	}
	return out
}

// assertDiffers holds lines, the answers only one document of a pair gives, to
// those the row states: each begins with one of prefixes, and each prefix
// begins one.
func assertDiffers(t *testing.T, which string, lines, prefixes []string) {
	t.Helper()
	for _, l := range lines {
		assert.True(t, slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(l, p) }),
			"only the %s document answers %q", which, l)
	}
	for _, p := range prefixes {
		require.True(t, slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, p) }),
			"the %s document alone answers %q...; it answers alone %q", which, p, lines)
	}
}

// TestCompile_ANestedEncodingIsChargedAlikeAtEveryLimit compiles a document
// writing references into, out of and beneath nested Encoding Objects at every
// reference-expansion budget up to what it is charged, its components in either
// order: each answer is the unbudgeted one or a refusal past the budget
// (docs/compilers/openapi.md#source-budgets), alike in both orders.
func TestCompile_ANestedEncodingIsChargedAlikeAtEveryLimit(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.2.0\ninfo: {title: t, version: '1'}\npaths:\n  /a: {post: {requestBody: " +
		"{$ref: '#/components/requestBodies/B'}, responses: {'200': {$ref: '#/components/responses/R'}}}}\n" +
		"components:\n"
	enc := func(key string) string {
		return "{contentType: multipart/mixed, " + key + ": {g: {headers: {H: {$ref: '#/components/headers/X'}, " +
			"J: {schema: {type: object, properties: {p: {$ref: '#/components/schemas/T'}}}}}}}}"
	}
	blocks := []string{
		"  schemas:\n    T: {type: string}\n    In: {$ref: '#/components/requestBodies/B/content/" +
			"multipart~1form-data/encoding/f/encoding/g/headers/J/schema'}\n",
		"  headers:\n    X: {description: x, schema: {type: integer}}\n    Y: {$ref: '#/components/headers/X'}\n",
		"  responses:\n    R: {description: r, headers: {Y: {$ref: '#/components/headers/Y'}}, content: {multipart/mixed: " +
			"{encoding: {f: " + enc("prefixEncoding") + "}}}}\n",
		"  requestBodies:\n    B: {content: {multipart/form-data: {schema: {type: object, properties: {f: " + partSchema +
			"}}, encoding: {f: " + enc("encoding") + "}}}}\n",
	}
	full, fullFound := assertOneReadingInEitherOrder(t, head, blocks, -1)
	require.NotNil(t, full, "%v", fullFound)
	for limit := 1; ; limit++ {
		require.Less(t, limit, 1<<12, "no budget admits the document")
		doc, found := assertOneReadingInEitherOrder(t, head, blocks, limit)
		if doc == nil {
			assert.Contains(t, found, "error openapi/budget-exceeded ", "refused at %d past the budget alone", limit)
			continue
		}
		assert.Empty(t, cmp.Diff(full, doc, cmpopts.IgnoreFields(ir.SourceInfo{}, "Hash")), "at %d", limit)
		assert.Equal(t, fullFound, found, "at %d", limit)
		return
	}
}
