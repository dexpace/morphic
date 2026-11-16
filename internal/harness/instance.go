// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// codeOutsideType is the validate pass's finding on a default its carrier's
// type does not admit (docs/ir/values.md), which judges each instance.
const codeOutsideType = "pass/default-outside-type"

// admitted is the instance oracle: each valid instance the case at writes as a
// default must draw no codeOutsideType at its carrier. The validate pass is the
// judge; nothing here reads a value. It declines a case writing no valid
// instance, one whose compile draws an error, and one whose document holds an
// instance at no carrier, which the judge never met. A Go error compiling the
// case is the writer's or the harness's fault, not the source's, so it ends
// the case as a panic does.
func admitted(ctx context.Context, s *shard, at []int, doc Document) Verdict {
	var valid []Instance
	for _, in := range doc.Instances {
		if in.Valid {
			valid = append(valid, in)
		}
	}
	if len(valid) == 0 {
		return declined(OracleInstance, DeclineNoInstance)
	}

	got, _ := s.compile(ctx, at) // the case wrote doc, so it writes it again
	if got.err != nil {
		panic(fmt.Errorf("harness: compiling case %v: %w", at, got.err))
	}
	if _, failed := ir.FirstError(got.diags); failed {
		return declined(OracleInstance, DeclineErrorDiagnostic)
	}
	held := carriers(got.doc)
	for _, in := range valid {
		if !held[in.Node] {
			return declined(OracleInstance, DeclineNotJudged)
		}
	}
	return excluded(valid, got.diags)
}

// carriers returns the ID of each property, header included, and each
// parameter of doc that holds a default as a value: the places the validate
// pass judges one at. A nil doc holds none.
func carriers(doc *ir.Document) map[string]bool {
	held := map[string]bool{}
	if doc == nil {
		return held
	}
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		if p, ok := reflect.TypeAssert[ir.Property](v); ok && p.Default != nil {
			held[string(p.ID)] = true
		}
		if p, ok := reflect.TypeAssert[ir.Parameter](v); ok && p.Default != nil {
			held[string(p.ID)] = true
		}
		return true
	}))
	return held
}

// excluded is the verdict on instances, each held at its carrier, given what
// the run reported: found, naming each instance a codeOutsideType at its
// carrier excludes, and the message, or passed where none is.
func excluded(instances []Instance, diags []ir.Diagnostic) Verdict {
	var names, lines []string
	for _, in := range instances {
		for _, d := range diags {
			if d.Code == codeOutsideType && d.Provenance.Node == in.Node {
				names = append(names, strconv.Quote(in.Name))
				lines = append(lines, strconv.Quote(in.Name)+": "+d.Message)
				break
			}
		}
	}
	if len(names) == 0 {
		return passed(OracleInstance)
	}
	return found(OracleInstance, strings.Join(names, ", ")+"\n"+strings.Join(lines, "\n"))
}
