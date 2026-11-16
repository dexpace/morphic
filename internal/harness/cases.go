// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// maxCases bounds a product: CaseCount refuses one of more cases, so an index
// fits an int and a run over every case ends.
const maxCases = 1 << 22

// shardAxes is how many leading axes a shard shares (CheckCases): a case and
// every neighbour an oracle compares it with differ past them alone.
const shardAxes = 2

// Cases is one format's product of schema cases.
type Cases interface {
	// Axes names each axis and its values. An index counts the last axis fastest.
	Axes() []Axis
	// Write returns the document at one value index per axis, and false where the
	// combination writes nothing: a keyword its version lacks, a value its target does not take.
	Write(at []int) (Document, bool)
}

// Axis is one dimension of a product: its name and its values, in order.
type Axis struct {
	Name   string
	Values []Value
}

// Value is one value of an axis. Like is empty for a value the gate runs, and otherwise names
// the core value of the same axis a finding at it is recorded under.
type Value struct{ Name, Like string }

// Document is what one case writes, and the neighbours each case oracle
// compares it with. A neighbour is named by its value index per axis.
type Document struct {
	// Sources are the root first, and the rest what its references name.
	Sources []compilers.Source
	// Options are the compiler options the case needs.
	Options map[string]string
	// Equal is the case this one must equal up to decorators (spelling).
	Equal *Equal
	// Absent is the case leaving the construct out (presence).
	Absent []int
	// Other is the case writing the construct's other value, nil for a
	// control (presence).
	Other []int
	// Carried is false for a construct the format never carries (presence).
	Carried bool
	// Reason says why not, required when Carried is false and Absent is set
	// (presence).
	Reason string
	// Instances are each default the document writes (instances).
	Instances []Instance
}

// Equal is the partner a spelling must equal, and what makes the two one.
type Equal struct {
	// To is the partner case.
	To []int
	// Rule is "docs/compilers/openapi.md#<key>", the rule making the spellings
	// an equivalence.
	Rule string
	// Relocate maps a string naming a place in this spelling to its name in
	// the partner's coordinates.
	Relocate func(string) string
	// Stated are the reports this spelling draws and its partner does not.
	Stated []Stated
}

// Stated is one report a spelling states it causes.
type Stated struct {
	Severity ir.Severity
	Code     string
	Pointer  jsontext.Pointer
}

// Instance is one default a document writes: the ID of the node carrying it,
// whether the test suite holds it valid, and its name there.
type Instance struct {
	Node  string
	Valid bool
	Name  string
}

// CaseCount returns how many cases c's product holds, the product of its axes'
// lengths. It refuses a product of no axis or of more than maxCases cases, an
// axis with no value, two axes or two values of one axis of one name, a name
// the record or a case name cannot hold, and a Like naming no core value of its
// axis.
func CaseCount(c Cases) (int, error) {
	_, n, err := product(c)
	return n, err
}

// product returns c's axes and how many cases they make, reading them once.
func product(c Cases) ([]Axis, int, error) {
	if c == nil {
		return nil, 0, errors.New("harness: no cases")
	}
	axes := c.Axes()
	n, err := countCases(axes)
	return axes, n, err
}

// countCases is CaseCount over axes.
func countCases(axes []Axis) (int, error) {
	if len(axes) == 0 {
		return 0, errors.New("harness: a product of no axis")
	}
	n := 1
	seen := make(map[string]bool, len(axes))
	for _, a := range axes {
		if seen[a.Name] {
			return 0, fmt.Errorf("harness: two axes named %q", a.Name)
		}
		seen[a.Name] = true
		if err := checkAxis(a); err != nil {
			return 0, err
		}
		if len(a.Values) > maxCases/n {
			return 0, fmt.Errorf("harness: axis %q takes the product past %d cases", a.Name, maxCases)
		}
		n *= len(a.Values)
	}
	return n, nil
}

// checkAxis refuses an axis CaseCount refuses for itself.
func checkAxis(a Axis) error {
	if err := checkName("an axis", a.Name); err != nil {
		return err
	}
	if len(a.Values) == 0 {
		return fmt.Errorf("harness: axis %q has no value", a.Name)
	}
	core := make(map[string]bool, len(a.Values))
	for _, v := range a.Values {
		if err := checkName("a value of axis "+a.Name, v.Name); err != nil {
			return err
		}
		if _, dup := core[v.Name]; dup {
			return fmt.Errorf("harness: axis %q has two values named %q", a.Name, v.Name)
		}
		core[v.Name] = v.Like == ""
	}
	for _, v := range a.Values {
		if v.Like != "" && !core[v.Like] {
			return fmt.Errorf("harness: value %q of axis %q is like %q, no core value of it", v.Name, a.Name, v.Like)
		}
	}
	return nil
}

// checkName refuses a name a case name cannot hold: empty, or holding the "/"
// that joins a case's value names, or a tab or a line break, which a record's
// line cannot hold. what says whose name it is.
func checkName(what, name string) error {
	if name == "" {
		return fmt.Errorf("harness: %s has no name", what)
	}
	if strings.ContainsAny(name, "/\t\n\r") {
		return fmt.Errorf("harness: %s is named %q, holding a slash, a tab or a line break", what, name)
	}
	return nil
}

// coordinates returns the value index per axis of the case at index i, the
// last axis counting fastest.
func coordinates(axes []Axis, i int) []int {
	at := make([]int, len(axes))
	for k := len(axes) - 1; k >= 0; k-- {
		at[k] = i % len(axes[k].Values)
		i /= len(axes[k].Values)
	}
	return at
}

// indexOf returns the index of the case at, coordinates' inverse.
func indexOf(axes []Axis, at []int) int {
	i := 0
	for k, a := range axes {
		i = i*len(a.Values) + at[k]
	}
	return i
}

// inProduct reports whether at names a case of axes: one index per axis, each
// within its axis.
func inProduct(axes []Axis, at []int) bool {
	if len(at) != len(axes) {
		return false
	}
	for k, a := range axes {
		if at[k] < 0 || at[k] >= len(a.Values) {
			return false
		}
	}
	return true
}

// caseName returns the name of the case at: its value names joined by "/".
func caseName(axes []Axis, at []int) string {
	names := make([]string, len(axes))
	for k, a := range axes {
		names[k] = a.Values[at[k]].Name
	}
	return strings.Join(names, "/")
}

// project returns at with each value replaced by the core value its Like
// names, the case a finding at at is recorded under.
func project(axes []Axis, at []int) []int {
	core := slices.Clone(at)
	for k, a := range axes {
		like := a.Values[at[k]].Like
		if like == "" {
			continue
		}
		core[k] = slices.IndexFunc(a.Values, func(v Value) bool { return v.Name == like })
	}
	return core
}

// Core returns the pick admitting the cases the gate runs: those each of whose
// values has an empty Like. Of cases CaseCount refuses it admits nothing.
func Core(c Cases) func(at []int) bool {
	axes, _, err := product(c)
	if err != nil {
		return func([]int) bool { return false }
	}
	return func(at []int) bool {
		if !inProduct(axes, at) {
			return false
		}
		for k, a := range axes {
			if a.Values[at[k]].Like != "" {
				return false
			}
		}
		return true
	}
}

// Coordinates returns the value index per axis of c's case at index i.
func Coordinates(c Cases, i int) ([]int, error) {
	axes, n, err := product(c)
	if err != nil {
		return nil, err
	}
	if i < 0 || i >= n {
		return nil, fmt.Errorf("harness: case %d is outside the %d cases", i, n)
	}
	return coordinates(axes, i), nil
}

// CheckCase writes c's case at and applies every case oracle to it, as
// CheckCases does, and returns false, with the case's name alone, where the
// case writes nothing. A caller mistake (nil ctx, cases CaseCount refuses, at
// outside the product) is OutcomeError with a harness-prefixed Detail.
func CheckCase(ctx context.Context, c Cases, at []int) (Result, bool) {
	axes, _, err := product(c)
	if err != nil {
		return faulted("", OutcomeError, err.Error()), true
	}
	if !inProduct(axes, at) {
		return faulted("", OutcomeError, fmt.Sprintf("harness: case %v is outside the product", at)), true
	}
	if ctx == nil {
		return faulted(caseName(axes, at), OutcomeError, "harness: nil context"), true
	}
	return newShard(c, axes, at).check(ctx, at)
}

// faulted is the Result of a case a fault ended, outcome and detail saying
// which: every case oracle declines as not reached.
func faulted(name string, outcome Outcome, detail string) Result {
	res := judgeAmong(name, CaseOracles(), nil, notReached)
	res.Outcome, res.Detail = outcome, detail
	return res
}

// CheckCases applies every case oracle to each of c's cases, in index order,
// whose value indices pick admits and which Write writes: a Result each, its
// Spec the case's name. A document an oracle compiles is kept for the shard of
// cases sharing the case's first two values, so a neighbour compiled once
// serves every case comparing with it, and memory holds one shard's.
func CheckCases(ctx context.Context, c Cases, pick func(at []int) bool) ([]Result, error) {
	if ctx == nil {
		return nil, errors.New("harness: nil context")
	}
	if pick == nil {
		return nil, errors.New("harness: no pick")
	}
	axes, n, err := product(c)
	if err != nil {
		return nil, err
	}
	var results []Result
	var s *shard
	for i := range n {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("harness: case %d: %w", i, err)
		}
		at := coordinates(axes, i)
		if !pick(slices.Clone(at)) {
			continue
		}
		if s == nil || !s.holds(at) {
			s = newShard(c, axes, at)
		}
		if r, ok := s.check(ctx, at); ok {
			results = append(results, r)
		}
	}
	return results, nil
}

// asked holds the case oracles CheckCase applies, in CaseOracles' order, each
// answering for the case at, which writes doc, and compiling through s what it
// compares. A case oracle with no entry declines as having nothing to ask.
var asked = []func(ctx context.Context, s *shard, at []int, doc Document) Verdict{
	present,
}

// shard is the run of cases sharing their first shardAxes values, and what
// each compiled to in it, by index.
type shard struct {
	cases Cases
	axes  []Axis
	key   []int
	kept  map[int]compiled
}

// compiled is what a case's document compiled to.
type compiled struct {
	doc   *ir.Document
	diags []ir.Diagnostic
	err   error
}

// newShard returns the empty shard holding the case at.
func newShard(c Cases, axes []Axis, at []int) *shard {
	return &shard{cases: c, axes: axes, key: slices.Clone(at[:min(shardAxes, len(at))]), kept: map[int]compiled{}}
}

// holds reports whether the case at belongs to s.
func (s *shard) holds(at []int) bool { return slices.Equal(s.key, at[:len(s.key)]) }

// compile returns what the case at compiles to, compiling it the first time s
// is asked, and false where the case writes nothing. A case outside the
// product compiles to an error.
func (s *shard) compile(ctx context.Context, at []int) (compiled, bool) {
	if !inProduct(s.axes, at) {
		return compiled{err: fmt.Errorf("harness: case %v is outside the product", at)}, true
	}
	i := indexOf(s.axes, at)
	if got, ok := s.kept[i]; ok {
		return got, true
	}
	doc, ok := s.cases.Write(slices.Clone(at))
	if !ok {
		return compiled{}, false
	}
	var got compiled
	got.doc, got.diags, got.err = compileSources(ctx, doc.Sources, doc.Options, passes())
	s.kept[i] = got
	return got, true
}

// check writes the case at and asks it every case oracle, behind barrier.Catch
// as Check is, and returns false, with the case's name alone, where the case
// writes nothing. A panic is OutcomePanic, and the oracles not yet asked
// decline as not reached.
func (s *shard) check(ctx context.Context, at []int) (Result, bool) {
	name := caseName(s.axes, at)
	written := true
	var verdicts []Verdict
	err := barrier.Catch(func() {
		var doc Document
		if doc, written = s.cases.Write(slices.Clone(at)); !written {
			return
		}
		for _, ask := range asked {
			verdicts = append(verdicts, ask(ctx, s, at, doc))
		}
	})
	if !written {
		return Result{Spec: name}, false
	}

	missing := idle
	if err != nil {
		missing = notReached
	}
	res := judgeAmong(name, CaseOracles(), verdicts, missing)
	if err != nil {
		res.Outcome, res.Detail = OutcomePanic, panicDetail(err)
	}
	res.Like = caseName(s.axes, project(s.axes, at))
	return res, true
}
