// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// landingsAt returns each landing of table in the source, by where it is, with
// the site it is attributed to, and requires each reached.
func landingsAt(t *testing.T, table *bind.Table) map[jsontext.Pointer]jsontext.Pointer {
	t.Helper()
	out := map[jsontext.Pointer]jsontext.Pointer{}
	for _, l := range table.Landings() {
		if l.At.Doc == bind.Source {
			out[l.At.Pointer] = l.Site.Pointer
			assert.True(t, l.Reached, "%s is reached", l.At.Pointer)
		}
	}
	return out
}

// TestTuples_AListOfItemsHoldsSchemas pins what a schema writing `items` as a
// list, a tuple as the drafts before 2020-12 spell one, holds where its
// dialect reads that list as a tuple (DialectRule.Tuples): each entry is a
// schema, its $ref bound, and so is the `additionalItems` beside it, each a
// landing the schema writing them is the site of, since the parser's model has
// no member for them. Beside one `items` schema additionalItems holds none, and
// a dialect reading no tuple there reads no schema in either.
func TestTuples_AListOfItemsHoldsSchemas(t *testing.T) {
	t.Parallel()
	src := head + "components:\n  schemas:\n" +
		"    T: {$schema: 'http://json-schema.org/draft-04/schema#', items: [{type: string}, {$ref: '#/components/schemas/B'}],\n" +
		"      additionalItems: {$ref: '#/components/schemas/B'}}\n" +
		"    U: {$schema: 'http://json-schema.org/draft-04/schema#', items: {type: string},\n" +
		"      additionalItems: {$ref: '#/components/schemas/Missing'}}\n" +
		"    B: {type: string}\n"
	table := bound(t, src, draftRule(self))
	const tuple = "/components/schemas/T"
	assert.Equal(t, map[jsontext.Pointer]jsontext.Pointer{
		tuple + "/items/0": tuple, tuple + "/items/1": tuple, tuple + "/additionalItems": tuple,
	}, landingsAt(t, table))
	for _, site := range []jsontext.Pointer{tuple + "/items/1", tuple + "/additionalItems"} {
		e := entryAt(t, table, site)
		assert.Equal(t, bind.Resolved, e.Hop.Status, "%s: %s", site, e.Hop.Why())
	}
	_, ok := table.Entry(at("/components/schemas/U/additionalItems"))
	assert.False(t, ok, "beside one items schema additionalItems is no schema the table reads")

	plain := bound(t, src, self)
	assert.Empty(t, landingsAt(t, plain), "2020-12 reads no tuple in a list of items")
	for _, site := range []jsontext.Pointer{tuple + "/items/1", tuple + "/additionalItems"} {
		_, ok := plain.Entry(at(site))
		assert.False(t, ok, "%s is no schema 2020-12 reads", site)
	}
}

// d4Rule is how a draft 4 schema names its resource and holds its subschemas.
var d4Rule = bind.DialectRule{Keyword: "id", Fragments: true, Definitions: true, Dependencies: true, PlainNames: true,
	Tuples: true, IgnoresRefFields: true}

// draftRule answers with d4Rule for a schema naming draft 4, and the zero rule
// for any other.
func draftRule(cfg bind.Config) bind.Config {
	return ruleFor(cfg, d4Rule)
}

// ruleFor answers with rule for a schema naming draft 4, and the zero rule for
// any other.
func ruleFor(cfg bind.Config, rule bind.DialectRule) bind.Config {
	cfg.Dialect = func(_, dialect string) bind.DialectRule {
		if dialect == "http://json-schema.org/draft-04/schema#" {
			return rule
		}
		return bind.DialectRule{}
	}
	return cfg
}

// TestDialectRule_AChainIntoAnotherDocumentReadsItsDialect pins a $ref the
// chain of a source $ref meets in another document before any walk of it,
// whose root names a draft that spells its identifier `$id` and ignores it
// beside a $ref, as draft 7 does: its base is read in that dialect, so the
// `$id` beside it names nothing and the $ref resolves against the document,
// though the source names no dialect at all, and in draft 4 the root's `id`
// gives it its base.
func TestDialectRule_AChainIntoAnotherDocumentReadsItsDialect(t *testing.T) {
	t.Parallel()
	other := "{$schema: 'http://json-schema.org/draft-04/schema#',\n" +
		"  definitions: {A: {type: string}, Sib: {$id: 'https://example.com/elsewhere/', $ref: '#/definitions/A'}}}\n"
	d7 := bind.DialectRule{Keyword: "$id", Fragments: true, Definitions: true, Dependencies: true, PlainNames: true,
		Tuples: true, IgnoresRefFields: true}
	table := bound(t, head+"components:\n  schemas:\n    S: {$ref: 'o.yaml#/definitions/Sib'}\n",
		ruleFor(withDocuments(map[string]string{"o.yaml": other}), d7))
	doc := docNamed(t, table, "o.yaml")
	e := entryAt(t, table, "/components/schemas/S")
	require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
	assert.Equal(t, bind.Position{Doc: doc, Pointer: "/definitions/A"}, e.Chain.End, "the chain ends where the draft reads it")

	based := "{$schema: 'http://json-schema.org/draft-04/schema#', id: 'https://example.com/lib/',\n" +
		"  definitions: {A: {id: 'a.json', type: integer}, Sib: {$ref: 'a.json'}}}\n"
	table = bound(t, head+"components:\n  schemas:\n    S: {$ref: 'o.yaml#/definitions/Sib'}\n",
		draftRule(withDocuments(map[string]string{"o.yaml": based})))
	e = entryAt(t, table, "/components/schemas/S")
	require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
	assert.Equal(t, bind.Position{Doc: docNamed(t, table, "o.yaml"), Pointer: "/definitions/A"}, e.Chain.End,
		"draft 4's id at the root gives the $ref its base")
}

// TestDialectRule_AnEmbedderIgnoringARefsSiblingsRootsNothingThere pins a// TestDialectRule_AnEmbedderIgnoringARefsSiblingsRootsNothingThere pins a
// schema writing a `$schema`, an identifier and a $ref, held by one whose rule
// ignores what is beside a $ref: it roots no resource, whichever keyword its
// own `$schema`'s dialect spells the identifier by, so a $ref naming its
// identifier resolves to nothing. Held by a schema reading them, it does.
func TestDialectRule_AnEmbedderIgnoringARefsSiblingsRootsNothingThere(t *testing.T) {
	t.Parallel()
	src := func(embedder string) string {
		return head + "components:\n  schemas:\n" +
			"    T: {" + embedder + "properties: {e: {$schema: 'https://json-schema.org/draft/2020-12/schema',\n" +
			"      $id: 'https://example.com/e', $ref: '#/components/schemas/B'}}}\n" +
			"    B: {type: string}\n    R: {$ref: 'https://example.com/e'}\n"
	}
	held := entryAt(t, bound(t, src("$schema: 'http://json-schema.org/draft-04/schema#', "), draftRule(self)), "/components/schemas/R")
	assert.NotEqual(t, bind.Resolved, held.Hop.Status, "draft 4 ignores the $schema and the $id beside e's $ref")
	e := entryAt(t, bound(t, src(""), draftRule(self)), "/components/schemas/R")
	require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
	assert.Equal(t, at("/components/schemas/T/properties/e"), e.Hop.Target)
}

// TestDialectRule_AnIdentifierBesideARefNamesNothing pins a rule ignoring what
// a schema writes beside its $ref (DialectRule.IgnoresRefFields), as draft 4
// does: an `id` there gives that $ref no base, roots no resource a $ref names,
// and, naming a fragment alone, declares no plain name, so the $ref resolves
// against the base above it. Under a rule applying them, as 2019-09's does,
// each `id` there is read.
func TestDialectRule_AnIdentifierBesideARefNamesNothing(t *testing.T) {
	t.Parallel()
	src := head + "components:\n  schemas:\n" +
		"    T: {$schema: 'http://json-schema.org/draft-04/schema#', id: 'https://example.com/t/',\n" +
		"      definitions: {A: {id: 'a', type: string}, B: {id: 'https://example.com/a', type: integer}},\n" +
		"      properties: {p: {id: 'https://example.com/', $ref: 'a'}, f: {id: '#f', $ref: 'a'},\n" +
		"        g: {$ref: '#f'}, h: {$ref: 'https://example.com/'}}}\n"
	const props = "/components/schemas/T/properties/"
	for name, c := range map[string]struct {
		ignores bool
		p       jsontext.Pointer
	}{
		"ignored": {ignores: true, p: "/components/schemas/T/definitions/A"},
		"applied": {ignores: false, p: "/components/schemas/T/definitions/B"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rule := d4Rule
			rule.IgnoresRefFields = c.ignores
			table := bound(t, src, ruleFor(self, rule))
			e := entryAt(t, table, props+"p")
			require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
			assert.Equal(t, at(c.p), e.Hop.Target, "the base p's $ref resolves against")
			for site, target := range map[jsontext.Pointer]jsontext.Pointer{"g": props + "f", "h": props + "p"} {
				e := entryAt(t, table, props+site)
				if !c.ignores {
					require.Equal(t, bind.Resolved, e.Hop.Status, "%s: %s", site, e.Hop.Why())
					assert.Equal(t, at(target), e.Hop.Target, site)
					continue
				}
				assert.NotEqual(t, bind.Resolved, e.Hop.Status, "%s names what an ignored id would", site)
			}
		})
	}
}

// TestDialectRule_ReadsWhatADialectSpellsOtherwise pins what Config.Dialect
// changes for a schema whose `$schema` names a dialect, and for what it holds:
// the keyword an identifier is written under, an identifier naming a fragment
// alone declaring that plain name, and `definitions` holding schemas. Without
// the rule, the same document declares none of them.
func TestDialectRule_ReadsWhatADialectSpellsOtherwise(t *testing.T) {
	t.Parallel()
	src := head + "components:\n  schemas:\n" +
		"    T: {$schema: 'http://json-schema.org/draft-04/schema#', id: 'https://example.com/t',\n" +
		"      properties: {a: {$ref: 'u'}, b: {$ref: '#foo'}}, definitions: {Foo: {id: '#foo', properties: {c: {$ref: '#foo'}}}}}\n" +
		"    U: {$schema: 'http://json-schema.org/draft-04/schema#', id: 'https://example.com/u'}\n"
	table := bound(t, src, draftRule(self))
	for site, want := range map[jsontext.Pointer]jsontext.Pointer{
		"/components/schemas/T/properties/a":                 "/components/schemas/U",
		"/components/schemas/T/properties/b":                 "/components/schemas/T/definitions/Foo",
		"/components/schemas/T/definitions/Foo/properties/c": "/components/schemas/T/definitions/Foo",
	} {
		e := entryAt(t, table, site)
		require.Equal(t, bind.Resolved, e.Hop.Status, "%s: %s", site, e.Hop.Why())
		assert.Equal(t, at(want), e.Hop.Target, site)
	}
	base, known := table.Base(at("/components/schemas/T/definitions/Foo"))
	assert.True(t, known)
	assert.Equal(t, "https://example.com/t", base, "a fragment id names no resource of its own")

	plain := bound(t, src, self)
	for _, site := range []jsontext.Pointer{"/components/schemas/T/properties/a", "/components/schemas/T/properties/b"} {
		assert.NotEqual(t, bind.Resolved, entryAt(t, plain, site).Hop.Status, "%s reads as 2020-12 does", site)
	}
	_, ok := plain.Entry(at("/components/schemas/T/definitions/Foo/properties/c"))
	assert.False(t, ok, "a definitions entry is no schema 2020-12 reads")
}

// TestDialectRule_AValueNamingNoDialectChoosesNone pins a `$schema` whose
// value the configuration says names no dialect (DialectRule.NamesNone): it
// roots no dialect, and its schema reads its document's, here draft 4's, so
// its `id` declares the URI a $ref names. Read as naming a dialect of 2020-12's
// keywords instead, the `id` declares nothing.
func TestDialectRule_AValueNamingNoDialectChoosesNone(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\njsonSchemaDialect: 'http://json-schema.org/draft-04/schema#'\n" +
		"paths: {}\ncomponents:\n  schemas:\n    I: {$schema: 'rel/x', id: 'https://example.com/i'}\n" +
		"    R: {$ref: 'https://example.com/i'}\n"
	answer := func(none bool) bind.Config {
		cfg := draftRule(self)
		draft := cfg.Dialect
		cfg.Dialect = func(name, dialect string) bind.DialectRule {
			if dialect == "rel/x" {
				return bind.DialectRule{NamesNone: none}
			}
			return draft(name, dialect)
		}
		return cfg
	}
	table := bound(t, src, answer(true))
	assert.Empty(t, table.Document(bind.Source).Dialects, "the value roots no dialect")
	e := entryAt(t, table, "/components/schemas/R")
	require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
	assert.Equal(t, at("/components/schemas/I"), e.Hop.Target)

	named := bound(t, src, answer(false))
	assert.Len(t, named.Document(bind.Source).Dialects, 1)
	assert.NotEqual(t, bind.Resolved, entryAt(t, named, "/components/schemas/R").Hop.Status)
}

// TestDialectRule_AWalkStartsInTheDialectAboveIt pins the dialect a walk from
// a landing of another document starts in: the one the schemas on the way to
// it name, so a draft 4 document's nested definitions hold schemas whose $ref
// the table binds, as a walk from its root would.
func TestDialectRule_AWalkStartsInTheDialectAboveIt(t *testing.T) {
	t.Parallel()
	other := "{$schema: 'http://json-schema.org/draft-04/schema#', definitions: {A: {definitions: {B: {$ref: '#/definitions/C'}}},\n" +
		"  C: {type: string}}}\n"
	table := bound(t, head+"components:\n  schemas:\n    S: {$ref: 'o.yaml#/definitions/A'}\n",
		draftRule(withDocuments(map[string]string{"o.yaml": other})))
	doc := docNamed(t, table, "o.yaml")
	e, ok := table.Entry(bind.Position{Doc: doc, Pointer: "/definitions/A/definitions/B"})
	require.True(t, ok, "the nested entry is a schema of the draft 4 document")
	assert.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
}

// TestDialectRule_ANestedSchemaNamesItsDialectAtAResourceRoot pins where a
// `$schema` a schema holds chooses the dialect: only where that schema roots a
// resource, by an identifier spelled as its embedder spells one, as JSON Schema
// 2020-12 has an embedded resource do (§9.3.1), or as the dialect it names
// does. Elsewhere it chooses nothing, so draft 4's `id` and `definitions` read
// as 2020-12 reads them, as the lowering reads that schema.
func TestDialectRule_ANestedSchemaNamesItsDialectAtAResourceRoot(t *testing.T) {
	t.Parallel()
	const d4 = "$schema: 'http://json-schema.org/draft-04/schema#'"
	src := head + "components:\n  schemas:\n    A:\n      properties:\n" +
		"        e: {" + d4 + ", $id: 'https://example.com/e', definitions: {Y: {id: '#bar'}}}\n" +
		"        f: {" + d4 + ", id: 'https://example.com/f', definitions: {Z: {id: '#baz'}}}\n" +
		"        p: {" + d4 + ", id: '#foo', definitions: {X: {$ref: '#/components/schemas/B'}}}\n" +
		"        r: {$ref: 'https://example.com/e#bar'}\n" +
		"        s: {$ref: 'https://example.com/f#baz'}\n" +
		"        u: {$ref: '#foo'}\n" +
		"    B: {type: string}\n"
	table := bound(t, src, draftRule(self))
	for site, want := range map[jsontext.Pointer]jsontext.Pointer{
		"/components/schemas/A/properties/r": "/components/schemas/A/properties/e/definitions/Y",
		"/components/schemas/A/properties/s": "/components/schemas/A/properties/f/definitions/Z",
	} {
		e := entryAt(t, table, site)
		require.Equal(t, bind.Resolved, e.Hop.Status, "%s: %s", site, e.Hop.Why())
		assert.Equal(t, at(want), e.Hop.Target, "%s: an embedded resource reads draft 4 below its root", site)
	}
	assert.NotEqual(t, bind.Resolved, entryAt(t, table, "/components/schemas/A/properties/u").Hop.Status,
		"a fragment id roots no resource, so its $schema names nothing and 2020-12 reads no anchor in it")
	_, ok := table.Entry(at("/components/schemas/A/properties/p/definitions/X"))
	assert.False(t, ok, "a schema rooting no resource holds no definitions")
}

// TestDialectRule_AWalkEntersARootBelowWhereItStarts pins a walk from a
// landing of another document reaching a resource root below it that names
// draft 4: the schemas under that root read draft 4's, so its definitions hold
// a schema whose $ref the table binds within that resource.
func TestDialectRule_AWalkEntersARootBelowWhereItStarts(t *testing.T) {
	t.Parallel()
	other := "{$defs: {A: {properties: {x: {$id: 'https://example.com/x', " +
		"$schema: 'http://json-schema.org/draft-04/schema#',\n" +
		"  definitions: {Y: {$ref: '#/definitions/Z'}, Z: {type: string}}}}}}}\n"
	table := bound(t, head+"components:\n  schemas:\n    S: {$ref: 'o.yaml#/$defs/A'}\n",
		draftRule(withDocuments(map[string]string{"o.yaml": other})))
	doc := docNamed(t, table, "o.yaml")
	const x = "/$defs/A/properties/x"
	e, ok := table.Entry(bind.Position{Doc: doc, Pointer: x + "/definitions/Y"})
	require.True(t, ok, "the definitions entry is a schema of the draft 4 resource")
	require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
	assert.Equal(t, bind.Position{Doc: doc, Pointer: x + "/definitions/Z"}, e.Hop.Target)
}

// TestDialectRule_ARootReadsItsOwnIdentifiersInItsDialect pins two readings of
// a draft 4 root no other schema holds: its own `id` naming a fragment alone
// declares that plain name, though the document's dialect spells no such
// anchor; and without an identifier it still holds its definitions as schemas,
// so an `id` among them gives the $ref below it its base.
func TestDialectRule_ARootReadsItsOwnIdentifiersInItsDialect(t *testing.T) {
	t.Parallel()
	const d4 = "$schema: 'http://json-schema.org/draft-04/schema#'"
	src := head + "components:\n  schemas:\n" +
		"    T: {" + d4 + ", id: '#t', type: string}\n" +
		"    U: {$ref: '#t'}\n" +
		"    R: {" + d4 + ", definitions: {A: {id: 'https://example.com/a/', properties: {p: {$ref: 'b'}}}}}\n" +
		"    B: {" + d4 + ", id: 'https://example.com/a/b', type: string}\n"
	table := bound(t, src, draftRule(self))
	for site, want := range map[jsontext.Pointer]jsontext.Pointer{
		"/components/schemas/U":                            "/components/schemas/T",
		"/components/schemas/R/definitions/A/properties/p": "/components/schemas/B",
	} {
		e := entryAt(t, table, site)
		require.Equal(t, bind.Resolved, e.Hop.Status, "%s: %s", site, e.Hop.Why())
		assert.Equal(t, at(want), e.Hop.Target, site)
	}
}

// TestDialectRule_AnAnchorTakesItsDialectsGrammar pins the name an identifier
// naming a fragment alone declares under a rule reading the drafts' plain-name
// grammar: `a:b`, which 2020-12's refuses, so a $ref naming it resolves there
// and not where the document's own dialect reads it.
func TestDialectRule_AnAnchorTakesItsDialectsGrammar(t *testing.T) {
	t.Parallel()
	src := head + "components:\n  schemas:\n" +
		"    T: {$schema: 'http://json-schema.org/draft-04/schema#', id: 'https://example.com/t',\n" +
		"      properties: {a: {$ref: '#a:b'}}, definitions: {L: {id: '#a:b'}}}\n" +
		"    U: {properties: {a: {$ref: '#c:d'}}, $defs: {L: {$anchor: 'c:d'}}}\n"
	table := bound(t, src, draftRule(self))
	e := entryAt(t, table, "/components/schemas/T/properties/a")
	require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
	assert.Equal(t, at("/components/schemas/T/definitions/L"), e.Hop.Target)
	assert.Equal(t, bind.NoAnchor, entryAt(t, table, "/components/schemas/U/properties/a").Hop.Reason,
		"2020-12 declares no anchor its grammar refuses, and a $ref naming one finds none")
}

// TestDialectRule_ADocumentOfSchemasReadsItsDraftAtItsRoot pins a document of
// schemas naming draft 4 at its root, which the binder reads loose: its
// root's tuple holds schemas, so an identifier in an entry declares its name,
// and its `dependencies` holds schemas, whose $ref the table binds.
func TestDialectRule_ADocumentOfSchemasReadsItsDraftAtItsRoot(t *testing.T) {
	t.Parallel()
	other := "{$schema: 'http://json-schema.org/draft-04/schema#', type: array, items: [{id: '#x', type: string}],\n" +
		"  dependencies: {a: {$ref: '#/definitions/C'}, b: [c]}, definitions: {C: {type: string}}}\n"
	table := bound(t, head+"components:\n  schemas:\n    S: {$ref: 'o.yaml#x'}\n    T: {$ref: 'o.yaml'}\n",
		draftRule(withDocuments(map[string]string{"o.yaml": other})))
	doc := docNamed(t, table, "o.yaml")
	e := entryAt(t, table, "/components/schemas/S")
	require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
	assert.Equal(t, bind.Position{Doc: doc, Pointer: "/items/0"}, e.Hop.Target)
	dep, ok := table.Entry(bind.Position{Doc: doc, Pointer: "/dependencies/a"})
	require.True(t, ok, "a schema in dependencies is one the table reads")
	assert.Equal(t, bind.Resolved, dep.Hop.Status, dep.Hop.Why())
}
