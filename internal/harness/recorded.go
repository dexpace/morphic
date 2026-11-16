// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// unclassified is the reason Write gives a line it adds. No reason map names
// it, so Judge holds the line unreasoned until someone gives it one.
const unclassified = "unclassified"

// Finding is what an oracle found at one case: the case's name, and What, the
// first line of the verdict's Detail, its signature.
type Finding struct{ Name, What string }

// Recorded is an oracle's known findings at the core cases, by name, each
// with what was found and the reason it stands: a file of lines
// "name<TAB>what<TAB>reason", a line starting "#" a comment.
type Recorded map[string]entry

// entry is one line of a Recorded.
type entry struct{ what, reason string }

// ReadRecorded reads a record. A line that is not a comment, blank, or three
// non-empty fields between tabs, and a name listed twice, are errors.
func ReadRecorded(data []byte) (Recorded, error) {
	r := Recorded{}
	n := 0
	for line := range strings.Lines(string(data)) {
		n++
		line = strings.TrimSuffix(line, "\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || slices.Contains(fields, "") {
			return nil, fmt.Errorf("harness: record line %d: want name, what and reason between tabs, got %q", n, line)
		}
		if _, dup := r[fields[0]]; dup {
			return nil, fmt.Errorf("harness: record line %d: %q is listed twice", n, fields[0])
		}
		r[fields[0]] = entry{what: fields[1], reason: fields[2]}
	}
	return r, nil
}

// Judge holds found, every finding an oracle made at the core cases, to r,
// whose reasons must be keys of reasons. It returns, each sorted, a finding r
// does not list, one it lists with another What, a line naming no finding,
// and a line whose reason reasons does not name, such as "unclassified".
func (r Recorded) Judge(found []Finding, reasons map[string]string) (unlisted, changed, stale, unreasoned []string) {
	seen := make(map[string]bool, len(found))
	for _, f := range found {
		seen[f.Name] = true
		e, ok := r[f.Name]
		switch {
		case !ok:
			unlisted = append(unlisted, f.Name+"\t"+f.What)
		case e.what != f.What:
			changed = append(changed, fmt.Sprintf("%s: listed %q, found %q", f.Name, e.what, f.What))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(r)) {
		if !seen[name] {
			stale = append(stale, name+"\t"+r[name].what)
		}
		if _, ok := reasons[r[name].reason]; !ok {
			unreasoned = append(unreasoned, name+"\t"+r[name].reason)
		}
	}
	slices.Sort(unlisted)
	slices.Sort(changed)
	return unlisted, changed, stale, unreasoned
}

// Explains reports whether r lists f at as, the name of the core case f's case
// projects to (Result.Like), with f's What.
func (r Recorded) Explains(f Finding, as string) bool {
	e, ok := r[as]
	return ok && e.what == f.What
}

// Write renders found as a record ReadRecorded reads back: header as comment
// lines, then a line per finding, by name, keeping the reason r gives its
// name, and "unclassified" for a name r does not list.
func (r Recorded) Write(header string, found []Finding) []byte {
	var b strings.Builder
	for line := range strings.Lines(header) {
		b.WriteString(strings.TrimRight("# "+strings.TrimSuffix(line, "\n"), " ") + "\n")
	}
	sorted := slices.SortedFunc(slices.Values(found), func(a, b Finding) int { return cmp.Compare(a.Name, b.Name) })
	for _, f := range sorted {
		reason := unclassified
		if e, ok := r[f.Name]; ok {
			reason = e.reason
		}
		b.WriteString(f.Name + "\t" + f.What + "\t" + reason + "\n")
	}
	return []byte(b.String())
}

// Findings returns what o found in each of results, in their order.
func Findings(results []Result, o Oracle) []Finding {
	var found []Finding
	for _, r := range results {
		for _, v := range r.Verdicts {
			if v.Oracle == o && v.Found() {
				found = append(found, findingOf(r, v))
			}
		}
	}
	return found
}

// findingOf is the Finding v, a verdict of r that found something, makes.
func findingOf(r Result, v Verdict) Finding {
	what, _, _ := strings.Cut(v.Detail, "\n")
	return Finding{Name: r.Spec, What: what}
}

// Unexplained returns, of results, those records does not explain: a result a
// fault ended (Result.Faulted), whole, and one with a finding its oracle's
// record does not list at the core case the result projects to (Result.Like),
// which holds the verdicts of such findings alone, the first its outcome.
func Unexplained(results []Result, records map[Oracle]Recorded) []Result {
	var out []Result
	for _, r := range results {
		if r.Faulted() {
			out = append(out, r)
			continue
		}
		var left []Verdict
		for _, v := range r.Verdicts {
			if v.Found() && !records[v.Oracle].Explains(findingOf(r, v), r.Like) {
				left = append(left, v)
			}
		}
		if len(left) > 0 {
			out = append(out, Result{Spec: r.Spec, Like: r.Like, Outcome: left[0].Outcome, Detail: left[0].Detail, Verdicts: left})
		}
	}
	return out
}
