// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"encoding/json/v2"
	"fmt"
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
// when written and again when its value changes; one nothing carries changes
// nothing, and Reason says why. Documents compare less their diagnostics, which
// a dropped construct can still draw, and their source table, whose hash
// differs with any text. A construct replacing a member its target writes has
// no case leaving it out, so its two values are compared alone.
func present(ctx context.Context, s *shard, at []int, doc CaseDocument) Verdict {
	if doc.Absent == nil {
		return declined(OraclePresence, DeclineNoConstruct)
	}
	if !doc.Carried && doc.Reason == "" {
		panic(fmt.Sprintf("harness: case %v is carried by nothing and says not why", at))
	}
	if doc.Replaces && (!doc.Carried || doc.Other == nil) {
		panic(fmt.Sprintf("harness: case %v replaces a member of its target and names no other value", at))
	}

	neighbours := [][]int{at}
	if !doc.Replaces {
		neighbours = append(neighbours, doc.Absent)
	}
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
	return judgePresence(s.axes, doc, texts)
}

// judgePresence is the verdict of a case whose documents compiled to texts:
// the case's, then the case leaving its construct out unless doc replaces a
// member, then the case writing its other value, when it names one.
func judgePresence(axes []Axis, doc CaseDocument, texts []string) Verdict {
	if doc.Replaces {
		if texts[0] == texts[1] {
			return found(OraclePresence, whatDiscarded+"\nequal to "+caseName(axes, doc.Other))
		}
		return passed(OraclePresence)
	}
	absent := caseName(axes, doc.Absent)
	switch {
	case !doc.Carried && texts[0] != texts[1]:
		return found(OraclePresence, whatCarried+"\n"+doc.Reason+"\ndiffers from "+absent)
	case !doc.Carried:
		return passed(OraclePresence)
	case texts[0] == texts[1]:
		return found(OraclePresence, whatChangesNothing+"\nequal to "+absent)
	case len(texts) == 3 && texts[0] == texts[2]:
		return found(OraclePresence, whatDiscarded+"\nequal to "+caseName(axes, doc.Other))
	}
	return passed(OraclePresence)
}

// encoded returns what the case at compiled to as the presence oracle
// compares it, and why it cannot: the case did not compile (uncompiled), so
// it would compare a refusal. A case the cases write nothing for, as the
// neighbour of a document that names it, and a compile's Go error are the
// harness's or the product's mistake, and a panic, as in the other oracles.
func (s *shard) encoded(ctx context.Context, at []int) (string, Decline) {
	got, ok := s.compile(ctx, at)
	if !ok {
		panic(fmt.Sprintf("harness: case %v is named as a neighbour and writes nothing", at))
	}
	if got.err != nil {
		panic(fmt.Sprintf("harness: compile case %v: %v", at, got.err))
	}
	if got.uncompiled() {
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
