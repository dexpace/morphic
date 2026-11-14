// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestBind_AReadBarredToOneAskerIsNoAnswerOfTheDocument pins that a read
// refused to the document that asked alone leaves the document readable by
// another, whichever asks first, and that a document another read first is
// judged for each asker read over the network: the source's reference to the
// private document resolves, the fetched document's does not, with the
// refusal as its reason, and the refusal is asked once per asker.
func TestBind_AReadBarredToOneAskerIsNoAnswerOfTheDocument(t *testing.T) {
	t.Parallel()
	const (
		evil   = "https://pub.example/evil.yaml"
		secret = "http://10.0.0.5/secret.yaml"
		why    = "external document " + secret + " refused: it is at a private address"
	)
	docs := map[string]string{evil: "X: {$ref: '" + secret + "#/S'}\n", secret: "S: {type: string}\n"}
	for _, schemas := range []string{
		"    A: {$ref: '" + evil + "#/X'}\n    B: {$ref: '" + secret + "#/S'}\n",
		"    B: {$ref: '" + secret + "#/S'}\n    A: {$ref: '" + evil + "#/X'}\n",
	} {
		var opens, judged []ask
		cfg := self
		cfg.Open = func(name, from string) (*yaml.Node, error) {
			opens = append(opens, ask{name, from})
			if name == secret && from == evil {
				return nil, &bind.BarredError{Why: why}
			}
			return openapitest.YAMLDocument(t, docs[name]), nil
		}
		cfg.Admits = func(name, from string) error {
			judged = append(judged, ask{name, from})
			if name == secret && from == evil {
				return &bind.BarredError{Why: why}
			}
			return nil
		}
		table := bound(t, head+"components:\n  schemas:\n"+schemas, cfg)

		assert.Equal(t, bind.Resolved, entryAt(t, table, "/components/schemas/B").Chain.Status)
		e, ok := table.Entry(bind.Position{Doc: docNamed(t, table, evil), Pointer: "/X"})
		require.True(t, ok)
		assert.Equal(t, bind.Unresolved, e.Hop.Status)
		assert.Equal(t, why, e.Hop.Detail)
		assert.Equal(t, why, (&bind.BarredError{Why: why}).Error())
		count := 0
		for _, a := range slices.Concat(opens, judged) {
			if a == (ask{secret, evil}) {
				count++
			}
		}
		assert.Equal(t, 1, count, "the asker is refused once, whatever asks first: %v %v", opens, judged)
	}
}
