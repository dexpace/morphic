// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/replacement"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// phraseMembers are the members every phrase case's model declares beside the
// deprecated one, and so the names a message may mention.
var phraseMembers = []string{"a", "b", "c", "d", "X"}

// phraseRoutes are the operations every phrase case declares beside the
// deprecated one, by the route that names each.
var phraseRoutes = map[string]ir.OpID{
	"GET /v2/zones":  "op/v2",
	"GET /v0/zones":  "op/v0",
	"POST /v0/zones": "op/v0-post",
}

// phraseDocument is a document holding one deprecated operation and one
// deprecated member, each with message, beside the operations phraseRoutes
// names and the members phraseMembers names. It returns the document, the
// deprecated operation and the deprecated member.
func phraseDocument(message string) (*ir.Document, *ir.Operation, *ir.Property) {
	old := route("op/old", http.MethodGet, "/old", says(message))
	old.Provenance = ir.Provenance{Pointer: "/paths/~1old/get"}
	doc := routes(old,
		route("op/v2", http.MethodGet, "/v2/zones", nil),
		route("op/v0", http.MethodGet, "/v0/zones", nil),
		route("op/v0-post", http.MethodPost, "/v0/zones", nil))
	props := make([]ir.Property, 0, 1+len(phraseMembers))
	props = append(props, ir.Property{ID: "p/old", WireName: "old", Deprecation: says(message),
		Provenance: ir.Provenance{Pointer: "/components/schemas/m/properties/old"}})
	for _, name := range phraseMembers {
		props = append(props, ir.Property{ID: ir.PropID("p/" + name), WireName: name})
	}
	model := &ir.Model{ID: "t/m", Properties: props}
	doc.Types = ir.TypeRegistry{"t/m": model}
	return doc, &doc.Services[0].Groups[0].Operations[0], &model.Properties[0]
}

// entities returns the references naming each of names: a route by its
// operation, anything else by the member it spells.
func entities(names []string) (ops, members []ir.EntityRef) {
	for _, name := range names {
		if op, ok := phraseRoutes[name]; ok {
			ops = append(ops, ir.EntityRef{Operation: op})
			continue
		}
		members = append(members, ir.EntityRef{Property: ir.PropID("p/" + name)})
	}
	return ops, members
}

// TestMessage_ReadsOnlyIntroducedNames pins that a message names a replacement
// only where a phrase introduces the name as one: `use` opening its sentence,
// or one of the cue phrases anywhere in it, with no negation before the cue,
// and then at most a filler word. A name any other words lead to, a route or a
// member in code, is neither read nor reported, whatever else the message says
// of it. Each row is read as an operation's message and as a member's, and
// draws no finding.
func TestMessage_ReadsOnlyIntroducedNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		message string
		want    []string
	}{
		{"Unlike `GET /v0/zones`, this pages.", nil},
		{"This endpoint replaced POST /v0/zones.", nil},
		{"Formerly `b`.", nil},
		{"Use `d`, not `b`.", []string{"d"}},
		{"Use `a` instead of `b`.", []string{"a"}},
		{"Do not use `b`; use `a`.", []string{"a"}},
		{"If you use `c`, set `d`.", nil},
		{"You must not use `b`.", nil},
		{"This was not replaced by `c`.", nil},
		{"Stop using `b`.", nil},
		{"Callers should use `a`. `b` is also deprecated.", []string{"a"}},
		{"Deprecated. Use `a` (see `b`).", []string{"a"}},
		{"Please use the new `a` instead.", []string{"a"}},
		{"Instead, use `a`.", []string{"a"}},
		{"Use `a`, `b` or `c`.", []string{"a", "b", "c"}},
		{"This endpoint is deprecated in favor of the GET `/v2/zones` endpoint.", []string{"GET /v2/zones"}},
		{"This field has been renamed to `X`. Both names are accepted during the deprecation period.", []string{"X"}},
		// A sentence ends at a full stop, a mark or a semicolon, and a later
		// sentence is no part of the phrase before it.
		{"Use `a`; `b` and `c` go too.", []string{"a"}},
		{"Use `a`! `b` is deprecated.", []string{"a"}},
		{"Use `a`? `b` is deprecated.", []string{"a"}},
		{"Use `a`.\n\nUse `b` or `c`.", []string{"a", "b", "c"}},
		{"USE `a`.", []string{"a"}},
		{"Use `a` and `b`; `c` and `d` are not.", []string{"a", "b"}},
		{"Use `a`, `c` and `d`.", []string{"a", "c", "d"}},
		// A filler word, and an opening parenthesis, may stand between the cue
		// and the name; nothing else does.
		{"Use the `a`.", []string{"a"}},
		{"Use a `a`.", []string{"a"}},
		{"Use an `a`.", []string{"a"}},
		{"Use new `a`.", []string{"a"}},
		{"Use (`a`).", []string{"a"}},
		{"Use something like `a`.", nil},
		{"Use caution with `a`.", nil},
		{"Callers can use `a`.", nil},
	} {
		t.Run(tc.message, func(t *testing.T) {
			t.Parallel()
			doc, op, member := phraseDocument(tc.message)

			assert.Empty(t, replacement.Infer(inferring(), whole.New(doc)),
				"a name no phrase introduces is not reported")

			wantOps, wantMembers := entities(tc.want)
			assert.Equal(t, wantOps, op.Deprecation.ReplacedBy, "the operation")
			assert.Equal(t, wantMembers, member.Deprecation.ReplacedBy, "the member")
		})
	}
}

// TestMessage_ReadsEachCueAndNoNearMiss pins every cue phrase against a
// sentence it reads and the three near misses it does not: a negation before
// it in its sentence, the cue inside a longer word, and a cue with no name
// after it. A near miss draws no finding, since what it names no phrase
// introduces.
func TestMessage_ReadsEachCueAndNoNearMiss(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cue, read, negated, longer, nameless string
	}{
		{"use", "Use `a`.", "Never use `a`.", "Reuse `a`.", "Use caution."},
		{"use after please", "Please use `a`.", "Please do not use `a`.", "Please misuse `a`.", "Please use caution."},
		{"use after should", "You should use `a`.", "You should not use `a`.", "You should misuse `a`.", "You should use caution."},
		{"use after must", "You must use `a`.", "You must never use `a`.", "You must reuse `a`.", "You must use caution."},
		{"use after instead", "Instead use `a`.", "Instead, don't use `a`.", "Instead reuse `a`.", "Instead use caution."},
		{"renamed to", "This field was renamed to `a`.", "This field was not renamed to `a`.",
			"This field was unrenamed to `a`.", "This field was renamed to something else."},
		{"replaced by", "It is replaced by `a`.", "It is never replaced by `a`.",
			"It is misreplaced by `a`.", "It is replaced by nothing."},
		{"replaced with", "It was replaced with `a`.", "It cannot be replaced with `a`.",
			"It was unreplaced with `a`.", "It was replaced with care."},
		{"superseded by", "It is superseded by `a`.", "It isn't superseded by `a`.",
			"It is presuperseded by `a`.", "It is superseded by a newer release."},
		{"in favor of", "Deprecated in favor of `a`.", "Not deprecated in favor of `a`.",
			"Deprecated in favors of `a`.", "Deprecated in favor of caution."},
		{"in favour of", "Deprecated in favour of `a`.", "No longer deprecated in favour of `a`.",
			"Deprecated in favours of `a`.", "Deprecated in favour of the old ways."},
	} {
		t.Run(tc.cue, func(t *testing.T) {
			t.Parallel()
			for message, want := range map[string][]string{
				tc.read: {"a"}, tc.negated: nil, tc.longer: nil, tc.nameless: nil,
			} {
				doc, _, member := phraseDocument(message)
				assert.Empty(t, replacement.Infer(inferring(), whole.New(doc)), message)
				_, wantMembers := entities(want)
				assert.Equal(t, wantMembers, member.Deprecation.ReplacedBy, message)
			}
		})
	}
}

// TestMessage_CostsItsLength pins that reading a message costs its length: a
// message of n names, each introduced, draws allocations that grow as n at n,
// 2n and 4n, where a scan that began its sentence again at each name would
// grow as n squared. The measure is allocations, which the public face gives
// without a seam into the finder, and the document's own growth is linear.
func TestMessage_CostsItsLength(t *testing.T) {
	cost := func(n int) float64 {
		names := make([]string, n)
		props := make([]ir.Property, 0, n+1)
		for i := range names {
			names[i] = fmt.Sprintf("`m%d`", i)
			props = append(props, ir.Property{ID: ir.PropID(fmt.Sprintf("p/m%d", i)), WireName: fmt.Sprintf("m%d", i)})
		}
		message := "Use " + strings.Join(names, ", ") + "."
		return testing.AllocsPerRun(5, func() {
			own := append([]ir.Property{{ID: "p/old", WireName: "old", Deprecation: says(message)}}, props...)
			doc := routes()
			doc.Types = ir.TypeRegistry{"t/m": &ir.Model{ID: "t/m", Properties: own}}
			replacement.Infer(inferring(), whole.New(doc))
		})
	}
	small, double, quad := cost(64), cost(128), cost(256)
	assert.Less(t, double/small, 2.6, "twice the names costs about twice as much: %v then %v", small, double)
	assert.Less(t, quad/double, 2.6, "and twice again: %v then %v", double, quad)
}
