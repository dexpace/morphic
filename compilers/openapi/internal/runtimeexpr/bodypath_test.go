// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// TestBodyPath_WalksEveryContentToTheSameMembers holds BodyPath to what an
// expression reads of a body: the members its tokens name from the root of each
// media type, which must be one chain, and the reason it gives where they are
// not.
func TestBodyPath_WalksEveryContentToTheSameMembers(t *testing.T) {
	t.Parallel()
	json, xml := content("application/json", "t/page"), content("application/xml", "t/page")
	for name, tc := range map[string]struct {
		body   *ir.Payload
		tokens []string
		want   []ir.PropID
		why    string
	}{
		"a body the document does not describe": {
			body: nil, tokens: []string{"info"}, why: "reads a request body the document does not describe",
		},
		"a body that is an alias of the model": {
			body: body(content("application/json", "t/pagealias2")), tokens: []string{"info", "next"},
			want: []ir.PropID{"p/info", "p/next"},
		},
		"the same member through an alias in a later content": {
			body: body(json, content("application/xml", "t/pagealias")), tokens: []string{"info", "next"},
			want: []ir.PropID{"p/info", "p/next"},
		},
		"another declaration through an alias in a later content": {
			body: body(json, content("application/xml", "t/rivalalias")), tokens: []string{"info", "next"},
			why: "reads /info/next, which the request body's media types hold as different members",
		},
		"a body of no content": {
			body: body(), tokens: []string{"info"}, why: "reads a request body the document does not describe",
		},
		"the body whole": {body: body(json), tokens: nil},
		"a chain of members": {
			body: body(json), tokens: []string{"info", "next"}, want: []ir.PropID{"p/info", "p/next"},
		},
		"a chain the first content lacks": {
			body: body(json), tokens: []string{"info", "gone"},
			why: "reads /info/gone, which the request body's application/json content holds as no chain of members",
		},
		"the same member of two contents": {
			body: body(json, xml), tokens: []string{"info", "next"}, want: []ir.PropID{"p/info", "p/next"},
		},
		"a member a later content lacks": {
			body: body(json, content("application/xml", "t/text")), tokens: []string{"info"},
			why: "reads /info, which the request body's application/xml content holds as no chain of members",
		},
		"another declaration of the member in a later content": {
			body: body(json, content("application/xml", "t/rival")), tokens: []string{"info", "next"},
			why: "reads /info/next, which the request body's media types hold as different members",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, why, ok := runtimeexpr.BodyPath(whole.New(&ir.Document{Types: registry()}), tc.body, tc.tokens, "request")
			assert.Equal(t, tc.why == "", ok)
			assert.Equal(t, tc.why, why)
			assert.Equal(t, tc.want, got)
		})
	}
}
