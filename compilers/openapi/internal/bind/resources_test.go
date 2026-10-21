package bind_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// TestBind_ReadsIdentifiers pins how a $ref names a schema by an identifier,
// as JSON Schema 2020-12 §8.2 reads one: a plain name its resource declares as
// an $anchor or a $dynamicAnchor, every schema under no $id being in the
// document's resource, as OAS 3.1 reads one, and a URI an $id declares, by
// RFC 3986 and normalized. A relative URI read against an $id names no
// document, and a name two schemas declare names neither.
func TestBind_ReadsIdentifiers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, schemas string
		site, want    jsontext.Pointer
		why           bind.Reason
	}{
		{"an anchor another component declares", "    A: {$ref: '#a'}\n    B: {$anchor: a}\n",
			"/components/schemas/A", "/components/schemas/B", 0},
		{"an anchor a definition declares", "    A: {$ref: '#a'}\n    B: {$defs: {d: {$anchor: a}}}\n",
			"/components/schemas/A", "/components/schemas/B/$defs/d", 0},
		{"a dynamic anchor, read as an anchor", "    A: {$ref: '#a'}\n    B: {$dynamicAnchor: a}\n",
			"/components/schemas/A", "/components/schemas/B", 0},
		{"an anchor of its own resource", "    B: {$id: 'https://x.test/b', $defs: {d: {$anchor: a}}, " +
			"properties: {p: {$ref: '#a'}}}\n", "/components/schemas/B/properties/p", "/components/schemas/B/$defs/d", 0},
		{"no anchor of another resource", "    A: {$ref: '#a'}\n    B: {$id: 'https://x.test/b', $anchor: a}\n",
			"/components/schemas/A", "", bind.NoAnchor},
		{"an anchor the resource it names declares", "    A: {$ref: 'https://x.test/b#a'}\n" +
			"    B: {$id: 'https://x.test/b', $defs: {d: {$anchor: a}}}\n", "/components/schemas/A", "/components/schemas/B/$defs/d", 0},
		{"an anchor of the resource an $id writing it starts", "    A: {$ref: 'https://x.test/b#a'}\n" +
			"    B: {$id: 'https://x.test/b', $anchor: a}\n", "/components/schemas/A", "/components/schemas/B", 0},
		{"two anchors of one resource", "    A: {$ref: '#a'}\n    B: {$anchor: a}\n    C: {$dynamicAnchor: a}\n",
			"/components/schemas/A", "", bind.DuplicateAnchor},
		{"no name an anchor cannot hold", "    A: {$ref: '#a'}\n    B: {$anchor: '1a'}\n", "/components/schemas/A", "", bind.NoAnchor},
		{"no anchor written in an example", "    A: {$ref: '#a'}\n    B: {example: {$anchor: a}}\n",
			"/components/schemas/A", "", bind.NoAnchor},
		{"the schema an $id names", "    A: {$ref: 'https://x.test/b'}\n    B: {$id: 'https://x.test/b'}\n",
			"/components/schemas/A", "/components/schemas/B", 0},
		{"a pointer from the root of the resource an $id names", "    A: {$ref: 'https://x.test/b#/properties/p'}\n" +
			"    B: {$id: 'https://x.test/b', properties: {p: {}}}\n", "/components/schemas/A", "/components/schemas/B/properties/p", 0},
		{"a pointer from the root of its own resource", "    B: {$id: 'https://x.test/b', properties: {p: {$ref: '#/$defs/d'}}, " +
			"$defs: {d: {}}}\n", "/components/schemas/B/properties/p", "/components/schemas/B/$defs/d", 0},
		{"the root of its own resource", "    B: {$id: 'https://x.test/b', properties: {p: {$ref: '#'}}}\n",
			"/components/schemas/B/properties/p", "/components/schemas/B", 0},
		{"nothing past the root of its own resource", "    B: {$id: 'https://x.test/b', properties: {p: {$ref: " +
			"'#/components/schemas/C'}}}\n    C: {}\n", "/components/schemas/B/properties/p", "", bind.NoResourceTarget},
		{"a URI relative to the $id above it", "    B: {$id: 'https://x.test/s/b', properties: {p: {$ref: 'c'}}}\n" +
			"    C: {$id: 'https://x.test/s/c'}\n", "/components/schemas/B/properties/p", "/components/schemas/C", 0},
		{"an absolute path against the $id above it", "    B: {$id: 'https://x.test/s/b', properties: {p: {$ref: '/c'}}}\n" +
			"    C: {$id: 'https://x.test/c'}\n", "/components/schemas/B/properties/p", "/components/schemas/C", 0},
		{"an $id relative to the $id above it", "    A: {$ref: 'https://x.test/s/c'}\n" +
			"    B: {$id: 'https://x.test/s/b', $defs: {c: {$id: c}}}\n", "/components/schemas/A", "/components/schemas/B/$defs/c", 0},
		{"an $id relative to the document", "    A: {$ref: 'b.json'}\n    B: {$id: b.json}\n",
			"/components/schemas/A", "/components/schemas/B", 0},
		{"no document for a relative URI no $id declares", "    B: {$id: 'https://x.test/b', properties: {p: {$ref: 'c'}}}\n",
			"/components/schemas/B/properties/p", "", bind.UnknownID},
		{"a document for an absolute URI no $id declares", "    B: {$id: 'https://x.test/b', properties: {p: {$ref: " +
			"'https://x.test/c'}}}\n", "/components/schemas/B/properties/p", "", bind.ExternalOff},
		{"no URI two $ids declare", "    A: {$ref: 'https://x.test/b'}\n    B: {$id: 'https://x.test/b'}\n" +
			"    C: {$id: 'https://x.test/b'}\n", "/components/schemas/A", "", bind.DuplicateID},
		{"an $id restating the URI it is in starts nothing", "    A: {$ref: 'https://x.test/b'}\n" +
			"    B: {$id: 'https://x.test/b', $defs: {d: {$id: 'https://x.test/b', $ref: '#/$defs/e'}, e: {}}}\n",
			"/components/schemas/A", "/components/schemas/B", 0},
		{"an $id naming the document is the document's", "    B: {$id: 'sub/b.json', $defs: {d: {$id: '../spec.yaml', " +
			"$ref: '#/components/schemas/C'}}}\n    C: {}\n", "/components/schemas/B/$defs/d", "/components/schemas/C", 0},
		{"no $id with a fragment", "    A: {$ref: 'https://x.test/b'}\n    B: {$id: 'https://x.test/b#f'}\n",
			"/components/schemas/A", "", bind.ExternalOff},
		{"a URI as RFC 3986 normalizes it", "    A: {$ref: 'https://x.test/a~b%2f/./c'}\n" +
			"    B: {$id: 'HTTPS://X.Test/a%7Eb%2F/c'}\n", "/components/schemas/A", "/components/schemas/B", 0},
		{"a URI no base can name", "    B: {$id: 'https://x.test/b', properties: {p: {$ref: '%zz'}}}\n",
			"/components/schemas/B/properties/p", "", bind.Unreadable},
		{"an $id of a fragment alone starts nothing", "    B: {$id: '#', $ref: '#/components/schemas/C'}\n    C: {}\n",
			"/components/schemas/B", "/components/schemas/C", 0},
		{"an opaque URI's escapes as RFC 3986 normalizes them", "    A: {$ref: 'urn:x:a~%zz%2f%'}\n" +
			"    B: {$id: 'urn:x:a%7e%zz%2F%'}\n", "/components/schemas/A", "/components/schemas/B", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := entryAt(t, bound(t, head+"components:\n  schemas:\n"+tc.schemas, resolving), tc.site)
			if tc.want == "" {
				assert.Equal(t, tc.why, e.Hop.Reason, e.Hop.Why())
				return
			}
			require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
			assert.Equal(t, at(tc.want), e.Hop.Target)
			assert.Equal(t, tc.want, e.Hop.Spelled, "an identifier is spelled where its schema is written")
			assert.Equal(t, bind.Schema, e.Hop.Role)
		})
	}
}

// TestBind_ReadsNoIdentifierTheModelIgnores pins where no identifier is read:
// the fields beside a reference object's $ref, which the model ignores. One
// written beside a path item's $ref is read, and refused as any pointer into
// those fields is (GitHub #824).
func TestBind_ReadsNoIdentifierTheModelIgnores(t *testing.T) {
	t.Parallel()
	cfg := resolving
	cfg.Beside = func(p jsontext.Pointer) (jsontext.Pointer, string, bool) {
		if p == "/paths/~1a/parameters/0/schema" {
			return "/paths/~1a", "parameters", true
		}
		return "", "", false
	}
	table := bound(t, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"+
		"paths:\n  /a:\n    $ref: '#/components/pathItems/I'\n    parameters: [{name: p, in: query, schema: {$anchor: i}}]\n"+
		"components:\n  pathItems:\n    I: {}\n"+
		"  parameters:\n    P: {$ref: '#/components/parameters/Q', schema: {$anchor: p}}\n    Q: {name: q, in: query}\n"+
		"  schemas:\n    A: {$ref: '#p'}\n    B: {$ref: '#i'}\n", cfg)
	assert.Equal(t, bind.NoAnchor, entryAt(t, table, "/components/schemas/A").Hop.Reason)
	assert.Equal(t, bind.BesideRef, entryAt(t, table, "/components/schemas/B").Hop.Reason)
}

// TestBind_ReadsTheSourcesIdentifiersFromAnotherDocument pins whose
// identifiers answer where: the source's answer a $ref another document
// writes, and another document's are not read.
func TestBind_ReadsTheSourcesIdentifiersFromAnotherDocument(t *testing.T) {
	t.Parallel()
	table := bound(t, head+"components:\n  schemas:\n    A: {$ref: 'other.yaml#/S'}\n    B: {$id: 'https://x.test/b'}\n"+
		"    C: {$ref: 'other.yaml#t'}\n", withDocuments(map[string]string{
		"other.yaml": "S: {$ref: 'https://x.test/b'}\nT: {$anchor: t}\n",
	}))
	a := entryAt(t, table, "/components/schemas/A")
	require.Equal(t, bind.Resolved, a.Chain.Status)
	assert.Equal(t, at("/components/schemas/B"), a.Chain.End)
	assert.Equal(t, bind.AnchorUnread, entryAt(t, table, "/components/schemas/C").Hop.Reason)
}

// TestBind_ReadsAMappingValueByIdentifier pins that a discriminator's mapping
// value names a schema by an identifier as a $ref does, read against the
// resource the discriminator is in.
func TestBind_ReadsAMappingValueByIdentifier(t *testing.T) {
	t.Parallel()
	table := bound(t, head+"components:\n  schemas:\n"+
		"    D: {$id: 'https://x.test/d', discriminator: {propertyName: k, mapping: {x: '#x', y: 'y'}}}\n"+
		"    X: {$anchor: x}\n    Y: {$id: 'https://x.test/y'}\n", resolving)
	disc := bind.Position{Pointer: "/components/schemas/D/discriminator"}
	x, ok := table.Mapping(disc, "#x")
	require.True(t, ok)
	assert.Equal(t, bind.NoAnchor, x.Hop.Reason, "an anchor of the document's resource, read in d's")
	_, ok = table.Mapping(disc, "y")
	assert.False(t, ok, "a value with no '#' names a component, which the table does not read")
}

// TestBind_LocatesAnAbsoluteURIUnderAnID pins that an absolute URI written
// under an $id, which no $id declares, names the document Config.Locate
// places, and is unreadable where it places none.
func TestBind_LocatesAnAbsoluteURIUnderAnID(t *testing.T) {
	t.Parallel()
	cfg := resolving
	cfg.Locate = func(uri, base string) (string, bool) {
		if uri == "https://x.test/nowhere" {
			return "", false
		}
		return resolving.Locate(uri, base)
	}
	table := bound(t, head+"components:\n  schemas:\n"+
		"    B: {$id: 'https://x.test/b', properties: {p: {$ref: 'https://x.test/nowhere'}}}\n", cfg)
	e := entryAt(t, table, "/components/schemas/B/properties/p")
	assert.Equal(t, bind.Unreadable, e.Hop.Reason)
	assert.Equal(t, "the document it names could not be read: it names no document a reference can", e.Hop.Why())
}

// TestBind_NamesTheDocumentByItsNormalizedURI pins that an $id naming the
// source's own URI, spelled otherwise, names the document's resource, and
// that a name no URI parses names none.
func TestBind_NamesTheDocumentByItsNormalizedURI(t *testing.T) {
	t.Parallel()
	cfg := resolving
	cfg.Self = "https://x.test/Spec.yaml"
	table := bound(t, head+"components:\n  schemas:\n"+
		"    B: {$id: 'https://x.test/b', $defs: {d: {$id: 'HTTPS://X.TEST/Spec.yaml', $ref: '#/components/schemas/C'}}}\n"+
		"    C: {}\n", cfg)
	assert.Equal(t, at("/components/schemas/C"), entryAt(t, table, "/components/schemas/B/$defs/d").Hop.Target)

	table = bound(t, head+"components:\n  schemas:\n    A: {$ref: 'other.yaml#/X'}\n", bind.Config{Self: "/spec%zz.yaml"})
	assert.Equal(t, bind.ExternalOff, entryAt(t, table, "/components/schemas/A").Hop.Reason)
}
