// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/engine"
)

// keyInputs returns a set of key inputs every row of a test starts from.
func keyInputs() engine.KeyInputs {
	return engine.KeyInputs{
		Build: "build", IRVersion: "1.0.0", Dir: "/dir", Path: "spec.yaml", SpecHash: "hash",
		Passes: []string{"validate"}, Settings: map[string]string{"grouping": "tags"},
	}
}

// TestDeriveKey_CoversEveryInput changes each input in turn, which is the only
// place one a run cannot vary, such as the IR version, can be seen. Each field of
// the inputs needs a row, so one added there and left out of the key fails here.
func TestDeriveKey_CoversEveryInput(t *testing.T) {
	t.Parallel()
	rows := map[string]func(*engine.KeyInputs){
		"Build":     func(in *engine.KeyInputs) { in.Build = "other" },
		"IRVersion": func(in *engine.KeyInputs) { in.IRVersion = "1.0.1" },
		"Dir":       func(in *engine.KeyInputs) { in.Dir = "/other" },
		"Path":      func(in *engine.KeyInputs) { in.Path = "other.yaml" },
		"SpecHash":  func(in *engine.KeyInputs) { in.SpecHash = "other" },
		"Passes":    func(in *engine.KeyInputs) { in.Passes = nil },
		"Settings":  func(in *engine.KeyInputs) { in.Settings = map[string]string{"grouping": "path-prefix"} },
	}
	want := engine.DeriveKey(keyInputs())
	for field, change := range rows {
		in := keyInputs()
		change(&in)
		assert.NotEqual(t, want, engine.DeriveKey(in), "%s changed and the key did not", field)
	}
	renamed := keyInputs()
	renamed.Settings = map[string]string{"other": "tags"}
	assert.NotEqual(t, want, engine.DeriveKey(renamed), "a setting's name changed and the key did not")
	assert.Empty(t, cmp.Diff(slices.Sorted(maps.Keys(rows)), declaredFields(t, "keyInputs")),
		"each input needs a row (-rows +declared)")
}

// TestDeriveKey_FramesEachInput moves a value across a boundary between inputs,
// which a key built by joining them would not tell apart: the passes run out
// where their count says, a setting's name ends before its value, and each pass
// reads what the one before it returned, so their order is an input too.
func TestDeriveKey_FramesEachInput(t *testing.T) {
	t.Parallel()
	with := func(passes []string, settings map[string]string) engine.KeyInputs {
		in := keyInputs()
		in.Passes, in.Settings = passes, settings
		return in
	}
	for name, pair := range map[string][2]engine.KeyInputs{
		"a pass taken for a setting": {with([]string{"a", "k", "0"}, nil), with([]string{"a"}, map[string]string{"k": "0"})},
		"a name taken for a value":   {with(nil, map[string]string{"ab": "c"}), with(nil, map[string]string{"a": "bc"})},
		"two passes swapped":         {with([]string{"a", "b"}, nil), with([]string{"b", "a"}, nil)},
	} {
		assert.NotEqual(t, engine.DeriveKey(pair[0]), engine.DeriveKey(pair[1]), name)
	}
}

// TestDeriveKey_DependsOnNoMapOrder derives one key many times over settings
// enough that a derivation reading them in map order, which Go draws afresh on
// each range, would give two keys.
func TestDeriveKey_DependsOnNoMapOrder(t *testing.T) {
	t.Parallel()
	in := keyInputs()
	in.Settings = map[string]string{}
	for n := range 10 {
		in.Settings[string(rune('a'+n))] = strconv.Itoa(n)
	}
	want := engine.DeriveKey(in)
	for range 20 {
		assert.Equal(t, want, engine.DeriveKey(in))
	}
}
