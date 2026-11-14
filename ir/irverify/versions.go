// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var availabilityType = reflect.TypeFor[ir.Availability]()

// checkVersions asserts every entry of Document.Versions names a version and
// none repeats an earlier one, which gives each label the one place in the
// order the entity-level rules read it at. A repeat is reported at its later
// entry, naming the earlier. It builds a map only for a document that declares
// versions.
func checkVersions(doc *ir.Document) []Violation {
	var vs []Violation
	var first map[string]int
	for i, label := range doc.Versions {
		path := "doc.Versions[" + strconv.Itoa(i) + "]"
		if isBlankName(label) {
			vs = append(vs, Violation{
				Code: "ir/version-label-blank", Message: "version label is blank, so no order places it", Path: path,
			})
			continue
		}
		if earlier, repeated := first[label]; repeated {
			vs = append(vs, Violation{
				Code: "ir/version-label-repeated",
				Message: "version label " + strconv.Quote(label) + " is listed here and at index " +
					strconv.Itoa(earlier),
				Path: path,
			})
			continue
		}
		if first == nil {
			first = make(map[string]int, len(doc.Versions))
		}
		first[label] = i
	}
	return vs
}

// versionPositions maps each label of doc.Versions to its first position. A blank
// label is left out, so none is declared; it is nil for a document that
// declares none.
func versionPositions(doc *ir.Document) map[string]int {
	var positions map[string]int
	for i, label := range doc.Versions {
		if _, listed := positions[label]; listed || isBlankName(label) {
			continue
		}
		if positions == nil {
			positions = make(map[string]int, len(doc.Versions))
		}
		positions[label] = i
	}
	return positions
}

// labelSite is where an Availability writes a version label, as the walk spells
// it: the field, the entry's index in it, or -1 for a field holding one label,
// and the member of the entry the label is.
type labelSite struct {
	field string
	index int
	label string
	sub   string
}

// path spells the site under the Availability at owner.
func (s labelSite) path(owner string) string {
	path := owner + s.field
	if s.index >= 0 {
		path += "[" + strconv.Itoa(s.index) + "]"
	}
	return path + s.sub
}

// labelSites returns every version label a, written by Availability, names, in
// field order.
func labelSites(a *ir.Availability) []labelSite {
	sites := make([]labelSite, 0, len(a.Added)+len(a.Removed)+1+len(a.RenamedFrom)+len(a.TypeChangedFrom)+
		len(a.RequiredChanged))
	for i, label := range a.Added {
		sites = append(sites, labelSite{".Added", i, label, ""})
	}
	for i, label := range a.Removed {
		sites = append(sites, labelSite{".Removed", i, label, ""})
	}
	if a.Deprecated != "" {
		sites = append(sites, labelSite{".Deprecated", -1, a.Deprecated, ""})
	}
	for i, n := range a.RenamedFrom {
		sites = append(sites, labelSite{".RenamedFrom", i, n.Version, ".Version"})
	}
	for i, t := range a.TypeChangedFrom {
		sites = append(sites, labelSite{".TypeChangedFrom", i, t.Version, ".Version"})
	}
	for i, b := range a.RequiredChanged {
		sites = append(sites, labelSite{".RequiredChanged", i, b.Version, ".Version"})
	}
	return sites
}

// appendAvailabilityViolations reports each version label of the Availability
// at owner that Document.Versions does not declare, and, where every label of
// Added and Removed is declared, the first entry breaking the order they
// follow: each ascends in Versions order, and merged they alternate. Either
// may come first, so a Removed first says the entity is there from the first
// version.
func appendAvailabilityViolations(vs []Violation, a ir.Availability, owner ir.WalkPath,
	positions map[string]int,
) []Violation {
	at := owner.String()
	undeclared := false
	for _, site := range labelSites(&a) {
		if _, declared := positions[site.label]; declared {
			continue
		}
		undeclared = true
		vs = append(vs, Violation{
			Code:    "ir/version-label-undeclared",
			Message: "version label " + strconv.Quote(site.label) + " is not one Document.Versions declares",
			Path:    site.path(at),
		})
	}
	if undeclared {
		return vs
	}
	if broken, ok := firstOrderBreak(a, positions); ok {
		vs = append(vs, Violation{
			Code: "ir/availability-out-of-order",
			Message: strconv.Quote(broken.label) + " does not follow " + strconv.Quote(broken.prev) +
				": Added and Removed alternate in Document.Versions order",
			Path: broken.path(at),
		})
	}
	return vs
}

// orderBreak is the entry of Added or Removed that breaks their order, and the
// label it was read after.
type orderBreak struct {
	labelSite
	prev string
}

// firstOrderBreak returns the first entry of a that breaks the order Added and
// Removed follow, given the position of every label in Versions: an entry that
// does not come after the one before it in its own list, else one that does not
// alternate in the two lists merged by position.
func firstOrderBreak(a ir.Availability, positions map[string]int) (orderBreak, bool) {
	if broken, ok := ascendingBreak(".Added", a.Added, positions); ok {
		return broken, true
	}
	if broken, ok := ascendingBreak(".Removed", a.Removed, positions); ok {
		return broken, true
	}
	return alternationBreak(a, positions)
}

// ascendingBreak returns the first entry of labels, the entries of field, at
// or before the position of the one preceding it.
func ascendingBreak(field string, labels []string, positions map[string]int) (orderBreak, bool) {
	for i := 1; i < len(labels); i++ {
		if positions[labels[i]] <= positions[labels[i-1]] {
			return orderBreak{labelSite{field, i, labels[i], ""}, labels[i-1]}, true
		}
	}
	return orderBreak{}, false
}

// alternationBreak merges Added and Removed by position, an Added placed before
// a Removed of its version, and returns the first entry that is of the same
// list as the one before it or at its position.
func alternationBreak(a ir.Availability, positions map[string]int) (orderBreak, bool) {
	var prev labelSite
	added, removed := 0, 0
	for added < len(a.Added) || removed < len(a.Removed) {
		var next labelSite
		takeAdded := removed == len(a.Removed) ||
			(added < len(a.Added) && positions[a.Added[added]] <= positions[a.Removed[removed]])
		if takeAdded {
			next = labelSite{".Added", added, a.Added[added], ""}
			added++
		} else {
			next = labelSite{".Removed", removed, a.Removed[removed], ""}
			removed++
		}
		if prev.label != "" && (next.field == prev.field || positions[next.label] == positions[prev.label]) {
			return orderBreak{next, prev.label}, true
		}
		prev = next
	}
	return orderBreak{}, false
}
