// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestVerify_AKeyAndIDThatDifferDrawOneCode holds each registry a document keys
// by an ID to one finding for a node filed under a key other than its own ID:
// the mismatch, at the entry. The node's own ID is its declaration and no
// reference, so no dangling-reference code names it too. Each case is paired
// with the same node keyed by its own ID, which draws neither.
func TestVerify_AKeyAndIDThatDifferDrawOneCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, prefix, mismatch, dangling, path string
		keyed                                  func(key, id string) *ir.Document
	}{
		{"type", "t", "ir/type-id-mismatch", "ir/dangling-type-ref", "types[t/x/A]", func(key, id string) *ir.Document {
			return &ir.Document{Types: ir.TypeRegistry{ir.TypeID(key): &ir.Model{ID: ir.TypeID(id), Name: named("m")}}}
		}},
		{"channel", "chan", "ir/channel-id-mismatch", "ir/dangling-channel-ref", "channels[chan/x/A]", func(key, id string) *ir.Document {
			return &ir.Document{Channels: map[ir.ChannelID]ir.Channel{ir.ChannelID(key): {ID: ir.ChannelID(id)}}}
		}},
		{"message", "msg", "ir/message-id-mismatch", "ir/dangling-message-ref", "messages[msg/x/A]", func(key, id string) *ir.Document {
			return &ir.Document{Messages: map[ir.MessageID]ir.Message{ir.MessageID(key): {ID: ir.MessageID(id)}}}
		}},
		{"auth", "auth", "ir/auth-id-mismatch", "ir/dangling-auth-ref", "auth[auth/x/A]", func(key, id string) *ir.Document {
			return &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{
				ir.AuthID(key): {ID: ir.AuthID(id), Name: named("a"), Kind: ir.AuthKindAPIKey},
			}}
		}},
		{"server", "server", "ir/server-id-mismatch", "ir/dangling-server-ref", "servers[server/x/A]", func(key, id string) *ir.Document {
			return &ir.Document{Servers: map[ir.ServerID]ir.Server{
				ir.ServerID(key): {ID: ir.ServerID(id), Name: named("a"), URLTemplate: "https://a.example"},
			}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			key, other := tc.prefix+"/x/A", tc.prefix+"/x/B"

			got := verifyDeclared(tc.keyed(key, other))
			assert.Equal(t, []string{tc.path}, violationPaths(got, tc.mismatch))
			assert.Empty(t, violationPaths(got, tc.dangling), "a node's own ID is no reference")

			twin := verifyDeclared(tc.keyed(key, key))
			assert.Empty(t, violationPaths(twin, tc.mismatch))
			assert.Empty(t, violationPaths(twin, tc.dangling))
		})
	}
}

// TestVerify_AReferenceInsideAMismatchedNodeStillDangles is the witness that
// the rule above skips a node's own ID alone: a model filed under a key other
// than its ID, whose Base names a type nothing declares, draws the mismatch and
// that one dangling reference, at the reference.
func TestVerify_AReferenceInsideAMismatchedNodeStillDangles(t *testing.T) {
	t.Parallel()
	build := func(base ir.TypeID) *ir.Document {
		return &ir.Document{Types: ir.TypeRegistry{
			"t/x/A": &ir.Model{ID: "t/x/B", Name: named("b"), Base: &ir.TypeRef{Target: base}},
			"t/x/D": &ir.Model{ID: "t/x/D", Name: named("d")},
		}}
	}

	got := verifyDeclared(build("t/x/C"))
	require.Equal(t, []string{"types[t/x/A]"}, violationPaths(got, "ir/type-id-mismatch"))
	assert.Equal(t, []string{"doc.Types[t/x/A].Base.Target"}, violationPaths(got, "ir/dangling-type-ref"))

	twin := verifyDeclared(build("t/x/D"))
	assert.Equal(t, []string{"types[t/x/A]"}, violationPaths(twin, "ir/type-id-mismatch"))
	assert.Empty(t, violationPaths(twin, "ir/dangling-type-ref"), "a Base naming a declared type draws none")
}
