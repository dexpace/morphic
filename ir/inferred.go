// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import "strings"

// inferredSeparator joins the names in an inferred-marker list. A heuristic's
// name holds none.
const inferredSeparator = ","

// Inferred lists the heuristics that produced a fact (ir-design §12): each
// heuristic's name once, comma-separated, in the order first written, none
// empty. The empty list marks a declared fact. Names compare exactly, and the IR
// declares no set of them.
//
// [JoinInferred] writes the form and [Inferred.IsCanonical] reads it. A
// conversion from a string skips both, and so does a decoded document, so
// irverify holds every stored list to the form.
type Inferred string

// JoinInferred joins inferred-marker lists into one in the form [Inferred]
// states.
//
// Each argument is itself such a list, so one heuristic's name is a list of one
// and two stored lists merge in one call. A node reached by two references is
// annotated twice, and a name that repeated per reference would make the field
// depend on the document's reference count rather than on which heuristics ran.
// A name is kept through a set, so a long list costs its length, not its square.
func JoinInferred(lists ...Inferred) Inferred {
	names := make([]string, 0, len(lists))
	listed := make(map[string]struct{})
	for _, list := range lists {
		for name := range strings.SplitSeq(string(list), inferredSeparator) {
			if _, repeat := listed[name]; repeat || name == "" {
				continue
			}
			listed[name] = struct{}{}
			names = append(names, name)
		}
	}
	return Inferred(strings.Join(names, inferredSeparator))
}

// IsCanonical reports whether l is in the form [Inferred] states, so that
// JoinInferred(l) == l: empty, or names that are each non-empty and listed once.
//
// irverify asks it of every list a document stores, decoded ones included, so it
// reads l once through a set. The set is a local map, which keeps its first few
// entries on the stack, so the lists a compiler writes cost no allocation.
func (l Inferred) IsCanonical() bool {
	if l == "" {
		return true
	}
	listed := make(map[string]struct{})
	for name := range strings.SplitSeq(string(l), inferredSeparator) {
		if _, repeat := listed[name]; repeat || name == "" {
			return false
		}
		listed[name] = struct{}{}
	}
	return true
}
