// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

// TitleNames is the title-names policy: whether a schema's title names the
// anonymous node built from it, in place of the hint composed from its
// position (docs/compilers/openapi.md#naming).
//
// It is a policy because JSON Schema declares a title a label for display, so
// preferring it over the position is a reading (architecture principle 6): a
// sentence written as a title names a node by every word, and editing one
// renames the node. It is unmarked in Provenance.Inferred, as a Hint with no
// Source is the suggestion already.
type TitleNames struct {
	// Disabled turns the reading off: every anonymous node is named by its
	// position, its title kept as its summary alone.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that a title names its node.
func (p TitleNames) Honours() bool { return !p.Disabled }
