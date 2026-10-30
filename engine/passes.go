// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/pass"
)

// ValidatePass names the validate pass, for a caller that does not import pass.
const ValidatePass = pass.ValidateName

// ErrUnknownPass reports a RunOptions.DisablePasses name that none of the
// engine's passes carries. It is a Go error rather than a diagnostic because it
// names no defect in the spec: the run was misconfigured.
var ErrUnknownPass = errors.New("unknown pass")

// Passes returns the default passes in the order a run takes them, in a list
// of the caller's own: the list New runs, and the one an embedder composes its
// own from (WithPasses). No other code holds that order. An analysis that
// judges the document, validate among them, goes after every transform, so it
// checks what the run returns, and a pass acting on what another returns goes
// after that one, so a second run changes nothing; architecture.md §2.2 gives
// the reasons.
func Passes() []pass.Pass {
	return []pass.Pass{pass.Validator{}}
}

// namedPass is a pass with the name it answered to when the engine was built.
// The engine asks for a name once, behind a barrier, and keeps it: a run turns
// passes off by name and reports a failure by it, and a name asked for again
// could panic outside New or differ from the one a caller disabled it by.
type namedPass struct {
	name string
	pass pass.Pass
}

// bindPasses names ps and refuses a list a run could not address by name: a nil
// pass, one with no name or whose Name panics, and two sharing a name, which
// turning one off would turn off together. Each is a Go error naming the
// position, as a refused compiler is.
func bindPasses(ps []pass.Pass) ([]namedPass, error) {
	bound := make([]namedPass, 0, len(ps))
	at := make(map[string]int, len(ps))
	for i, p := range ps {
		if isNilPass(p) {
			return nil, fmt.Errorf("engine: pass %d is nil", i)
		}
		name, err := nameOf(p)
		if err != nil {
			return nil, fmt.Errorf("engine: pass %d: %w", i, err)
		}
		if name == "" {
			return nil, fmt.Errorf("engine: pass %d has no name, which a run turns it off by", i)
		}
		if first, taken := at[name]; taken {
			return nil, fmt.Errorf("engine: passes %d and %d are both named %q; turning one off would turn off both",
				first, i, name)
		}
		at[name] = i
		bound = append(bound, namedPass{name: name, pass: p})
	}
	return bound, nil
}

// nameOf asks p for its name, behind a barrier.
func nameOf(p pass.Pass) (string, error) {
	var name string
	err := caught("Name", func() { name = p.Name() })
	return name, err
}

// isNilPass reports whether p is unsafe to call: an untyped nil interface or a
// typed nil pointer, which a method with a value receiver dereferences.
func isNilPass(p pass.Pass) bool {
	if p == nil {
		return true
	}
	rv := reflect.ValueOf(p)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// enabledPasses returns, in run order, those of e's passes that disabled does
// not name, and refuses a name none of them carries, for the reason
// RunOptions.DisablePasses gives.
func (e *Engine) enabledPasses(disabled []string) ([]namedPass, error) {
	names := make([]string, 0, len(e.passes))
	for _, p := range e.passes {
		names = append(names, p.name)
	}
	for _, name := range disabled {
		if !slices.Contains(names, name) {
			return nil, fmt.Errorf("engine: disable %q (known: %s): %w",
				name, strings.Join(names, ", "), ErrUnknownPass)
		}
	}

	return slices.DeleteFunc(slices.Clone(e.passes), func(p namedPass) bool {
		return slices.Contains(disabled, p.name)
	}), nil
}
