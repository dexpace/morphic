// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// codeOutsideType is the validate pass's finding on a default its carrier's
// type does not admit (docs/ir/values.md), which judges each instance.
const codeOutsideType = "pass/default-outside-type"

// kindInference names the heuristic that reads a kind off a schema naming
// none (docs/compilers/openapi.md#policies), as a node's Provenance.Inferred
// lists it.
const kindInference = "kind-inference"

// admitted is the instance oracle: each valid instance the case at writes as a
// default must draw no codeOutsideType at its carrier. The validate pass is the
// judge; nothing here reads a value but its kind. It declines a case writing no
// valid instance, one whose compile draws an error, and one whose document
// holds an instance at no carrier, which the judge never met. A Go error
// compiling the case is the writer's or the harness's fault, not the source's,
// so it ends the case as a panic does.
func admitted(ctx context.Context, shardValue *shard, at []int, doc CaseDocument) Verdict {
	var valid []Instance
	for _, in := range doc.Instances {
		if in.Valid {
			valid = append(valid, in)
		}
	}
	if len(valid) == 0 {
		return declined(OracleInstance, DeclineNoInstance)
	}

	got, _ := shardValue.compile(ctx, at) // the case wrote doc, so it writes it again
	if got.err != nil {
		panic(fmt.Errorf("harness: compiling case %v: %w", at, got.err))
	}
	if got.uncompiled() {
		return declined(OracleInstance, DeclineErrorDiagnostic)
	}
	held := carriers(got.doc)
	for _, in := range valid {
		if _, ok := held[in.Node]; !ok {
			return declined(OracleInstance, DeclineNotJudged)
		}
	}
	return excluded(valid, got.diags, held, got.doc)
}

// carrier is a place that holds a default as a value: the type it declares
// and the value.
type carrier struct {
	typ ir.TypeRef
	def *ir.Value
}

// carriers returns, by ID, each property, header included, and each parameter
// of doc that holds a default as a value: the places the validate pass judges
// one at.
func carriers(doc *ir.Document) map[string]carrier {
	held := map[string]carrier{}
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		if property, ok := reflect.TypeAssert[ir.Property](v); ok && property.Default != nil {
			held[string(property.ID)] = carrier{typ: property.Type, def: property.Default}
		}
		if parameter, ok := reflect.TypeAssert[ir.Parameter](v); ok && parameter.Default != nil {
			held[string(parameter.ID)] = carrier{typ: parameter.Type, def: parameter.Default}
		}
		return true
	}))
	return held
}

// excused reports whether a finding on carrierValue is the kind inference's
// known cost: the node carrierValue is checked against, past any alias of it
// (aliased), names kind-inference in its Provenance.Inferred, which narrows a
// schema admitting every kind to one, and the default is of another kind. A
// finding of what the default holds, a bound or a member, is the node's own and
// is never excused, nor is one on a node that carries no such mark or against a
// default of the node's kind.
func excused(doc *ir.Document, carrierValue carrier) bool {
	td := aliased(doc, carrierValue.typ.Target)
	var kind ir.ValueKind
	switch td.(type) {
	case *ir.Model, *ir.MapT:
		kind = ir.ValueObject
	case *ir.List, *ir.Tuple:
		kind = ir.ValueList
	default:
		return false // a node of no one kind is not narrowed to a kind
	}
	inferred := strings.Split(string(td.Common().Provenance.Inferred), ",")
	return slices.Contains(inferred, kindInference) && carrierValue.def.Kind != kind
}

// aliased returns the node target names, past each scalar whose base is
// another node, as the validate pass judges a default against what an alias
// names: a use of an inferred node beside a bound or a description is such an
// alias. The chain holds at most one scalar per node of doc.
func aliased(doc *ir.Document, target ir.TypeID) ir.TypeDef {
	td := doc.Types[target]
	for range len(doc.Types) {
		scalar, ok := td.(*ir.Scalar)
		if !ok || scalar.Base == nil {
			break
		}
		td = doc.Types[scalar.Base.Target]
	}
	return td
}

// excluded is the verdict on instances, each held at its carrier, given what
// the run reported: found, naming each instance a codeOutsideType at its
// carrier excludes, and the message, or passed where none is. A finding excused
// is none.
func excluded(instances []Instance, diags []ir.Diagnostic, held map[string]carrier, doc *ir.Document) Verdict {
	var names, lines []string
	for _, in := range instances {
		if excused(doc, held[in.Node]) {
			continue
		}
		for _, diagnostic := range diags {
			if diagnostic.Code == codeOutsideType && diagnostic.Provenance.Node == in.Node {
				names = append(names, strconv.Quote(in.Name))
				lines = append(lines, strconv.Quote(in.Name)+": "+diagnostic.Message)
				break
			}
		}
	}
	if len(names) == 0 {
		return passed(OracleInstance)
	}
	return found(OracleInstance, strings.Join(names, ", ")+"\n"+strings.Join(lines, "\n"))
}
