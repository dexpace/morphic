// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"
)

// TestRefConjunction_ABareRefHoistsNoNode holds that a $ref to a string or an
// object writing nothing beside it is a use of the target at every position:
// no anonymous node stands there or names the target as its base, and a
// component over a string is the alias that owns it.
func TestRefConjunction_ABareRefHoistsNoNode(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, target := range []string{"Obj", "Str"} {
			for _, site := range conjSites {
				if site.name == "component" && target == "Obj" {
					continue // a component over an object is a model, which a bare $ref makes only from now on
				}
				t.Run(fmt.Sprintf("%s/%s/%s", version, target, site.name), func(t *testing.T) {
					t.Parallel()
					requireNothingHoisted(t, version, target, site, "{"+refTo(target)+"}")
				})
			}
		}
	}
}

// TestRefConjunction_TwoUnionsOnEntriesLowerTheOneOfWhicheverEntryWritesIt holds
// that a oneOf and an anyOf on two entries beside a body, in either order of the
// entries, distribute the oneOf and keep the anyOf whole on each variant.
func TestRefConjunction_TwoUnionsOnEntriesLowerTheOneOfWhicheverEntryWritesIt(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, name := range []string{"Entries", "EntriesRev"} {
			t.Run(version+"/"+name, func(t *testing.T) {
				t.Parallel()
				doc, _ := compileUnrefused(t, admissionSpec(version, "", "Use: "+twoUnionShapes[name]))
				requireOneOfDistributed(t, doc)
			})
		}
	}
}
