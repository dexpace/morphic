// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/replacement"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
)

// TestMessage_UseOpensItsSentence pins that `use` is a cue only where nothing
// but words stands before it in its sentence, a comma after a lead word
// aside: a name or a mark before a lead word leaves the `use` after it no
// cue, as `use` is then no instruction the sentence opens with.
func TestMessage_UseOpensItsSentence(t *testing.T) {
	t.Parallel()
	for message, want := range map[string][]string{
		"`b`: please use `a`.":              nil,
		"For `b`, please use `a`.":          nil,
		"(Please use `a`.)":                 nil,
		"Deprecated. Please use `a`.":       {"a"},
		"Instead, please use `a`.":          {"a"},
		"Callers, instead, use `a`.":        nil,
		"Callers should, instead, use `a`.": {"a"},
	} {
		doc, _, member := phraseDocument(message)
		assert.Empty(t, replacement.Infer(inferring(), whole.New(doc)), message)
		_, wantMembers := entities(want)
		assert.Equal(t, wantMembers, member.Deprecation.ReplacedBy, message)
	}
}
