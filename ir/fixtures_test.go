// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPopulatedFixtures_LeaveNoFieldZero holds every populated* fixture to the
// "every field non-zero" its doc comment claims. The fixtures are the round-trip
// half of the JSON-contract tests, and a field a fixture leaves zero is a field
// those tests say nothing about: Encoding.Schema shipped with its codec
// unasserted because populatedEncoding was not extended with it, and retagging
// the field json:"-" left the package green.
//
// Sums and maps are deliberately out: populatedValue sets only the payload its
// Kind selects, and a map fixture has no fields to leave zero.
func TestPopulatedFixtures_LeaveNoFieldZero(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		fixture any
		// zeroOK names the fields a fixture leaves zero on purpose, against the
		// reason. An entry is a claim a reviewer has to agree with, which is the
		// point of spelling it here rather than dropping the fixture from the
		// table: the rest of its fields stay covered.
		zeroOK []zeroField
	}{
		{"Naming", populatedNaming(), nil},
		{"Docs", populatedDocs(), nil},
		{"Provenance", populatedProvenance(), nil},
		{"Deprecation", populatedDeprecation(), nil},
		{"Availability", populatedAvailability(), nil},
		{"Constraints", populatedConstraints(), nil},
		{"Encoding", populatedEncoding(), nil},
		{"XMLHints", populatedXMLHints(), nil},
		{"TypeRef", populatedTypeRef(), nil},
		{"TypeCommon", populatedTypeCommon("t/openapi/components/schemas/User"), nil},
		{"Header", populatedHeader(), nil},
		{"Property", populatedProperty(), []zeroField{eventPayloadLeftZero()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNoFieldZero(t, tc.fixture, tc.zeroOK...)
		})
	}
}

// stopNow is what recordingT.FailNow panics with, so that noFieldZeroReports
// recovers its own stop and no other panic.
type stopNow struct{}

// recordingT records what assertNoFieldZero reports instead of failing the
// test running it. FailNow stops the call as *testing.T's does, by a panic
// noFieldZeroReports recovers.
type recordingT struct {
	errors  []string
	stopped bool
}

func (*recordingT) Helper() {}

func (r *recordingT) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recordingT) FailNow() {
	r.stopped = true
	panic(stopNow{})
}

// noFieldZeroReports runs assertNoFieldZero over v and returns what it
// reported.
func noFieldZeroReports(v any, zeroOK ...zeroField) *recordingT {
	rec := &recordingT{}
	func() {
		defer func() {
			if p := recover(); p != nil {
				if _, ok := p.(stopNow); !ok {
					panic(p)
				}
			}
		}()
		assertNoFieldZero(rec, v, zeroOK...)
	}()
	return rec
}

// pair and embedsPair are the structs TestAssertNoFieldZero_ReportsWhatItHolds
// hands the walk: embedsPair promotes pair's fields, which it does not declare.
type (
	pair struct {
		A string
		B int
	}
	embedsPair struct {
		pair
		C bool
	}
)

// TestAssertNoFieldZero_ReportsWhatItHolds holds the walk every populated value
// goes through to what it reports, since a walk reporting nothing would leave
// each test it serves green whatever its value left zero.
func TestAssertNoFieldZero_ReportsWhatItHolds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		value   any
		zeroOK  []zeroField
		stopped bool
		errors  []string // a part of each message, in order
	}{
		{name: "every field set", value: pair{"a", 1}},
		{name: "through a pointer", value: &pair{"a", 1}},
		{name: "a field left zero", value: pair{A: "a"}, errors: []string{"pair.B is left zero"}},
		{name: "a field left zero on purpose", value: pair{A: "a"}, zeroOK: []zeroField{{"B", "why"}}},
		{
			name: "an exempt field set", value: pair{"a", 1}, zeroOK: []zeroField{{"B", "why"}},
			errors: []string{"pair.B is listed as deliberately zero (why)"},
		},
		{
			name: "an exempt name the struct lacks", value: pair{"a", 1}, zeroOK: []zeroField{{"Z", "why"}},
			stopped: true, errors: []string{"pair declares no field Z"},
		},
		{
			name: "an exempt name only promoted", value: embedsPair{pair{"a", 1}, true},
			zeroOK: []zeroField{{"A", "why"}}, stopped: true, errors: []string{"embedsPair declares no field A"},
		},
		{
			name: "a name listed twice", value: pair{A: "a"}, zeroOK: []zeroField{{"B", "x"}, {"B", "y"}},
			stopped: true, errors: []string{"each field is listed once"},
		},
		{name: "not a struct", value: 3, stopped: true, errors: []string{"must be a struct"}},
		{name: "no fields", value: struct{}{}, stopped: true, errors: []string{"witnesses nothing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := noFieldZeroReports(tc.value, tc.zeroOK...)
			assert.Equal(t, tc.stopped, got.stopped, "stopped")
			if assert.Len(t, got.errors, len(tc.errors), "%q", got.errors) {
				for i, want := range tc.errors {
					assert.Contains(t, got.errors[i], want)
				}
			}
		})
	}
}
