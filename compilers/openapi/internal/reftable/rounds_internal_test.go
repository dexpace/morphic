// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	"testing"

	"github.com/stretchr/testify/assert"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// TestGoOn_ARoundThatChangesNothingEndsOnTheBudget pins that each round of a
// pass costs a step: a held name that no drain ever clears, as a slip in the
// drain would leave it, makes every round change nothing, and the work's
// limit still ends the rounds rather than leaving the pass to run unbounded.
func TestGoOn_ARoundThatChangesNothingEndsOnTheBudget(t *testing.T) {
	t.Parallel()
	var src yaml.Node
	if err := yaml.Unmarshal([]byte("openapi: 3.1.0\n"), &src); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Self: "https://x.test/spec.yaml", Limit: budget.AtMost(100)}
	workValue := &work{ctx: t.Context(), steps: budget.New(cfg.Limit)}
	docs := newDocs(&src, cfg, workValue)
	b := newBinder(cfg, docs, workValue, newIdentifiers(resources{docs}.document(Source)), newReads())
	rounds := 0
	for ; rounds < 1_000 && !b.work.over(); rounds++ {
		b.reads.hold("https://x.test/held.yaml", Source)
		goOn(b)
	}
	assert.True(t, b.work.over(), "the budget ends rounds that change nothing")
	assert.LessOrEqual(t, rounds, 101, "one step a round")
}
