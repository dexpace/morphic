// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// The costs below are pinned as measured, so that a read of a document and a
// judgement of an asker are each held to a step of the work: an ask that
// charged nothing leaves the table cheaper by one, whatever else it spends.
const (
	// openCost is what a source with one $ref to a document that fails to read costs.
	openCost = 26
	// judgeCost is what a source and a document it reads, both named by URL,
	// cost when each asks for a third document, which the second judges.
	judgeCost = 70
)

// TestBind_ReadingADocumentIsAStepOfTheWork pins that the one ask of
// Config.Open that a $ref to another document makes is a step of the work: the
// table costs openCost, and a budget of one fewer stops it.
func TestBind_ReadingADocumentIsAStepOfTheWork(t *testing.T) {
	t.Parallel()
	const src = head + "components:\n  schemas:\n    A: {$ref: 'x.yaml'}\n"
	asks := 0
	cfg := self
	cfg.Open = func(string, string) (*yaml.Node, error) {
		asks++
		return nil, errors.New("no such document")
	}

	full := bound(t, src, cfg)

	require.Equal(t, 1, asks, "the reference asks for its document once")
	assert.Equal(t, openCost, full.Steps())
	assert.False(t, full.Exhausted())
	cfg.Limit = budget.AtMost(openCost - 1)
	assert.True(t, bound(t, src, cfg).Exhausted(), "one step short of it")
}

// TestBind_JudgingAnAskerIsAStepOfTheWork pins that judging a document
// another read first, for an asker read from the network, is a step of the
// work as a read is: the third ask below is that judgement, and the table
// costs judgeCost.
func TestBind_JudgingAnAskerIsAStepOfTheWork(t *testing.T) {
	t.Parallel()
	const base = "https://h.test/"
	const src = head + "components:\n  schemas:\n    A: {$ref: '" + base + "p.yaml#/P'}\n    B: {$ref: '" + base + "t.yaml#/T'}\n"
	var log askLog
	cfg := log.opening(t, self, map[string]string{
		base + "p.yaml": "P: {$ref: '" + base + "t.yaml#/T'}\n",
		base + "t.yaml": "T: {type: string}\n",
	})

	full := bound(t, src, cfg)

	assert.Equal(t, []ask{
		{base + "p.yaml", "spec.yaml"}, {base + "t.yaml", "spec.yaml"}, {base + "t.yaml", base + "p.yaml"},
	}, log.reads, "the document p reads is judged for it, having been read for the source")
	assert.Equal(t, judgeCost, full.Steps())
	cfg.Limit = budget.AtMost(judgeCost - 1)
	assert.True(t, bound(t, src, cfg).Exhausted(), "one step short of it")
}
