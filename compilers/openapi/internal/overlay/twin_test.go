// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// sharer writes a source whose value shares nodes: anchors, then a tree of
// mappings and sequences naming them by alias and by `<<` merge, one merge
// list among them, under keys few enough that a merge often brings a key the
// mapping writes too.
type sharer struct {
	r *rand.Rand
	// mappings names the anchors written so far that hold a mapping, which
	// alone a merge may name; anchors names every one.
	anchors, mappings []string
}

// value writes a value depth levels from the bottom.
func (s *sharer) value(depth int) string {
	switch k := s.r.IntN(6); {
	case depth == 0 || k == 0:
		return []string{"1", "2", "s"}[s.r.IntN(3)]
	case k == 1 && len(s.anchors) > 0:
		return "*" + s.anchors[s.r.IntN(len(s.anchors))]
	case k == 2:
		return "[" + s.value(depth-1) + ", " + s.value(depth-1) + "]"
	default:
		return s.mapping(depth)
	}
}

// mapping writes a flow mapping of up to three members, perhaps merging one or
// two of the mappings anchored so far.
func (s *sharer) mapping(depth int) string {
	var members []string
	if n := len(s.mappings); n > 0 && s.r.IntN(2) == 0 {
		merged := "*" + s.mappings[s.r.IntN(n)]
		if s.r.IntN(3) == 0 {
			merged = "[" + merged + ", *" + s.mappings[s.r.IntN(n)] + "]"
		}
		members = append(members, "<<: "+merged)
	}
	for _, key := range []string{"p", "q", "r"} {
		if s.r.IntN(2) == 0 {
			members = append(members, key+": "+s.value(depth-1))
		}
	}
	return "{" + strings.Join(members, ", ") + "}"
}

// source writes a source holding four anchors under x-a, each perhaps naming
// the ones before it, and a tree naming them under x-g.
func (s *sharer) source() string {
	var b strings.Builder
	b.WriteString(head + "x-a:\n")
	for i := range 4 {
		name := fmt.Sprintf("a%d", i)
		v := s.mapping(3)
		if other := s.value(3); s.r.IntN(4) == 0 && !strings.HasPrefix(other, "*") {
			v = other // YAML anchors no alias
		}
		fmt.Fprintf(&b, "  %s: &%s %s\n", name, name, v)
		s.anchors = append(s.anchors, name)
		if strings.HasPrefix(v, "{") {
			s.mappings = append(s.mappings, name)
		}
	}
	b.WriteString("x-g:\n")
	for i := range 3 {
		fmt.Fprintf(&b, "  k%d: %s\n", i, s.value(3))
	}
	return b.String()
}

// expansion writes the value src stands for with no node shared: every alias
// and merge written out, as decoding the source and encoding it again does.
func expansion(t *testing.T, src string) string {
	t.Helper()
	out, err := yaml.Marshal(valueOf(t, decoded(t, src)))
	require.NoError(t, err)
	return string(out)
}

// twinActions are the actions each generated source is overlaid with: removes
// and updates, a primitive replaced by an update among them, through
// descendant and wildcard selectors and filters, each selecting a node and
// its descendants or positions an alias or a merge shares.
var twinActions = []string{
	"  - target: $['x-g']..*\n    remove: true\n",
	"  - target: $..*\n    remove: true\n",
	"  - target: $['x-g'].*\n    remove: true\n",
	"  - target: $..p\n    remove: true\n",
	"  - target: $..[?@.p]\n    remove: true\n",
	"  - target: $..[?@ == 1]\n    remove: true\n",
	"  - target: $..[?@.p]\n    update: {u: 1}\n",
	"  - target: $['x-g'].*.*\n    update: {u: 1}\n",
	"  - target: $..[?@ == 1]\n    update: 9\n",
	"  - target: $..[?@.q]\n    update: {q: 7, u: [1]}\n",
	"  - target: $..[?@.p]\n    remove: true\n  - target: $..[?@ == 1]\n    update: 9\n",
}

// withoutInfo returns codes but the replaced-value infos, which turn on
// whether the source writes a value where it stands, and nil for none.
func withoutInfo(codes []string) []string {
	kept := slices.DeleteFunc(codes, func(c string) bool { return c == "info "+diag.OverlayReplacedValue })
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// TestApply_GivesEveryGeneratedSharedValueTheTreeOfItsExpansion holds an
// overlay over a source sharing nodes to what it leaves over the same value
// written with nothing shared: the same tree, applied or refused alike, with
// the same findings but the infos of replaced values. The sources are
// generated, so the sharing meets every action at positions no hand-written
// case chose.
func TestApply_GivesEveryGeneratedSharedValueTheTreeOfItsExpansion(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(1834, 9535))
	for i := range 150 {
		src := (&sharer{r: r}).source()
		twin := expansion(t, src)
		for j, action := range twinActions {
			ov := v11 + action
			origin, shared, sharedDiags := applied(t, src, ov, true)
			twinOrigin, written, writtenDiags := applied(t, twin, ov, true)

			at := fmt.Sprintf("source %d, action %d:\n%s\n%s", i, j, src, action)
			require.Equal(t, twinOrigin.Applied(), origin.Applied(), "%s\n%+v\n%+v", at, sharedDiags, writtenDiags)
			if d := cmp.Diff(withoutInfo(codes(writtenDiags)), withoutInfo(codes(sharedDiags))); d != "" {
				t.Fatalf("%s\nanother report (-twin +shared):\n%s", at, d)
			}
			if d := cmp.Diff(valueOf(t, written), valueOf(t, shared)); d != "" {
				t.Fatalf("%s\nanother tree (-twin +shared):\n%s", at, d)
			}
		}
	}
}

// TestApply_RemovesANodeAndItsDescendantsAtAnAnchorAsItsExpansionDoes pins the
// remove that selects a shared node and nodes beneath it in one selection,
// parent first, as a descendant segment orders them: every schema goes, as
// it does where nothing is shared.
func TestApply_RemovesANodeAndItsDescendantsAtAnAnchorAsItsExpansionDoes(t *testing.T) {
	t.Parallel()
	ov := v11 + "  - target: $.components.schemas..*\n    remove: true\n"

	origin, shared, diags := applied(t, aliasedSource, ov, false)

	require.True(t, origin.Applied(), "%+v", diags)
	sameCodes(t, nil, diags)
	sameValue(t, head+"components:\n  schemas: {}\n", shared)
}
