// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// messagesAt returns the message of every diagnostic carrying code, in the order
// they were reported, so a caller can pin which names were reported and not only
// how many.
func messagesAt(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			out = append(out, diagnostic.Message)
		}
	}
	return out
}

// messagesAtPointer returns the message of every diagnostic whose provenance
// names pointer, in report order. It filters on the pointer alone, so a report
// arriving at the right place under the wrong code is still returned.
func messagesAtPointer(diags []ir.Diagnostic, pointer string) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, diagnostic.Message)
		}
	}
	return out
}

// codesAtPointer returns the code of every diagnostic sited at pointer,
// sorted, so a caller pins which stage reported there, and how often: the
// loader's findings carry the pointer of the node they are about as well.
func codesAtPointer(diags []ir.Diagnostic, pointer string) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, diagnostic.Code)
		}
	}
	slices.Sort(out)
	return out
}

// sortedPointersAt returns every provenance pointer carried by a diagnostic
// with code, sorted so a caller pins the set rather than the walk's order.
func sortedPointersAt(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			out = append(out, string(diagnostic.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}

// unmodeledKeys returns unmodeled's keys sorted, so a caller pins the whole set
// rather than probing for the ones it happened to think of.
func unmodeledKeys(unmodeled ir.Unmodeled) []string {
	return slices.Sorted(maps.Keys(unmodeled))
}
