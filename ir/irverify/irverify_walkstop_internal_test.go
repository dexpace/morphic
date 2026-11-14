// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// referringDoc returns a sound document whose model holds a property typed by a
// second model, so a walk that read no declarations would find the reference
// dangling.
func referringDoc() *ir.Document {
	target := &ir.Model{ID: "t/x/Target", Name: ir.Naming{Source: "Target", Canonical: "target"}}
	holder := &ir.Model{
		ID: "t/x/Holder", Name: ir.Naming{Source: "Holder", Canonical: "holder"},
		Properties: []ir.Property{{
			ID: "p/x/Holder/a", WireName: "a", Name: ir.Naming{Source: "a", Canonical: "a"},
			Type: ir.TypeRef{Target: target.ID},
		}},
	}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{target.ID: target, holder.ID: holder}}
}

// truncations returns the ir/walk-truncated violations of vs, and the codes of
// every dangling one.
func truncations(vs []Violation) (truncated []Violation, dangling []string) {
	for _, v := range vs {
		switch {
		case v.Code == "ir/walk-truncated":
			truncated = append(truncated, v)
		case strings.HasPrefix(v.Code, "ir/dangling-"):
			dangling = append(dangling, v.Code)
		}
	}
	return truncated, dangling
}

// TestVerify_AStoppedWalkIsOneFindingAtTheRoot holds verify to turning the
// result of its walk into one finding, whatever the walk was cut by, and to
// claiming no reference dangling for want of declarations a stopped walk did
// not read.
func TestVerify_AStoppedWalkIsOneFindingAtTheRoot(t *testing.T) {
	t.Parallel()
	walking := func(root any, path string, vs ...ir.Visitor) bool {
		ir.WalkValues(root, path, vs...)
		return true
	}
	stopped := func(any, string, ...ir.Visitor) bool { return true }

	for name, walk := range map[string]walkFunc{"a walk that read it all": walking, "a walk that read nothing": stopped} {
		truncated, dangling := truncations(verify(referringDoc(), walk))

		if assert.Len(t, truncated, 1, name) {
			assert.Equal(t, ir.DocumentPath, truncated[0].Path, name)
			assert.Equal(t, "document nests deeper or unfolds larger than the bounded walk reads; part of it went unchecked",
				truncated[0].Message, name)
		}
		assert.Empty(t, dangling, "%s: no reference is dangling for what the walk did not read", name)
	}
}
