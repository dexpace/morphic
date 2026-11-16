// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"encoding/json/v2"
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// The What of each finding the presence oracle makes, which a record lists.
const (
	whatChangesNothing = "changes nothing"
	whatDiscarded      = "value discarded"
	whatCarried        = "carried"
)

// present is the presence oracle: it holds a case writing a construct to the
// case leaving it out and, for a construct the format carries, to the case
// writing its other value. A carried construct changes the compiled document
// when written and again when its value changes, which tells it from one
// announced and dropped, or noticed and its value discarded; one nothing
// carries changes nothing, and Reason says why. Documents compare less their
// diagnostics, which a dropped construct can still draw, and their source
// table, whose hash differs with any text.
func present(ctx context.Context, s *shard, at []int, doc Document) Verdict {
	if doc.Absent == nil {
		return declined(OraclePresence, DeclineNoConstruct)
	}
	if !doc.Carried && doc.Reason == "" {
		panic(fmt.Sprintf("harness: case %v is carried by nothing and says not why", at))
	}

	neighbours := [][]int{at, doc.Absent}
	if doc.Carried && doc.Other != nil {
		neighbours = append(neighbours, doc.Other)
	}
	texts := make([]string, len(neighbours))
	for i, n := range neighbours {
		text, why := s.encoded(ctx, n)
		if why != "" {
			return declined(OraclePresence, why)
		}
		texts[i] = text
	}

	against := "equal to " + caseName(s.axes, doc.Absent)
	switch {
	case !doc.Carried && texts[0] != texts[1]:
		return found(OraclePresence, whatCarried+"\n"+doc.Reason+"\ndiffers from "+caseName(s.axes, doc.Absent))
	case !doc.Carried:
		return passed(OraclePresence)
	case texts[0] == texts[1]:
		return found(OraclePresence, whatChangesNothing+"\n"+against)
	case len(texts) == 3 && texts[0] == texts[2]:
		return found(OraclePresence, whatDiscarded+"\nequal to "+caseName(s.axes, doc.Other))
	}
	return passed(OraclePresence)
}

// encoded returns what the case at compiled to as the presence oracle
// compares it, and why it cannot: the compile failed, or drew an error
// diagnostic, so it would compare a refusal. A case the cases write nothing
// for is an oracle's mistake, as the neighbour of a document that names it.
func (s *shard) encoded(ctx context.Context, at []int) (string, Decline) {
	got, ok := s.compile(ctx, at)
	if !ok {
		panic(fmt.Sprintf("harness: case %v is named as a neighbour and writes nothing", at))
	}
	if got.err != nil {
		return "", DeclineNoDocument
	}
	if _, failed := ir.FirstError(got.diags); failed {
		return "", DeclineErrorDiagnostic
	}
	own := *got.doc // a copy: the shard keeps the document for the other oracles
	own.Diagnostics = nil
	own.Sources = nil
	out, err := json.Marshal(&own)
	if err != nil {
		panic(fmt.Sprintf("harness: case %v does not encode: %v", at, err))
	}
	return string(out), ""
}
