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
type Recorded map[string]recordLine

// recordLine is one line of a Recorded.
type recordLine struct{ what, reason string }

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
		r[fields[0]] = recordLine{what: fields[1], reason: fields[2]}
	}
	return r, nil
}

// Judge holds findings, every finding an oracle made at the core cases, to r,
// whose reasons must be keys of reasons. It returns, each sorted, a finding r
// does not list, one it lists with another What, a line naming no finding,
// and a line whose reason reasons does not name, such as "unclassified".
func (r Recorded) Judge(findings []Finding, reasons map[string]string) (unlisted, changed, stale, unreasoned []string) {
	seen := make(map[string]bool, len(findings))
	for _, f := range findings {
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

// Explains reports whether r lists f at as, with f's What: as is f's case's
// own name, or the name of the core case it projects to (Result.Like).
func (r Recorded) Explains(f Finding, as string) bool {
	e, ok := r[as]
	return ok && e.what == f.What
}

// Write renders findings as a record ReadRecorded reads back: header as comment
// lines, then a line per finding, by name, keeping the reason r gives its
// name, and "unclassified" for a name r does not list. A What that is empty
// or holds a tab writes a line ReadRecorded refuses, so an oracle's signature
// line is neither.
func (r Recorded) Write(header string, findings []Finding) []byte {
	var b strings.Builder
	for line := range strings.Lines(header) {
		b.WriteString(strings.TrimRight("# "+strings.TrimSuffix(line, "\n"), " ") + "\n")
	}
	sorted := slices.SortedFunc(slices.Values(findings), func(a, b Finding) int { return cmp.Compare(a.Name, b.Name) })
	for _, f := range sorted {
		reason := unclassified
		if e, ok := r[f.Name]; ok {
			reason = e.reason
		}
		b.WriteString(f.Name + "\t" + f.What + "\t" + reason + "\n")
	}
	return []byte(b.String())
}

// Findings returns what o found in each of results, in their order, and each
// decline its record lists as it lists a finding (listed), whose What is
// "declined: <reason>".
func Findings(results []Result, o Oracle) []Finding {
	var out []Finding
	for _, r := range results {
		for _, v := range r.Verdicts {
			if v.Oracle == o && listed(v) {
				out = append(out, findingOf(r, v))
			}
		}
	}
	return out
}

// listed reports whether a record lists v: a finding, or one of the two
// declines no case should draw. A case is written to compile and each group to
// be judged, so a document that draws an error and an instance nothing judged
// show as a new finding does. A spelling the oracle cannot compare, for a
// relocation that collides or a document nested too deep, is listed by none.
func listed(v Verdict) bool {
	return v.Found() || v.Declined == DeclineErrorDiagnostic || v.Declined == DeclineNotJudged
}

// findingOf is the Finding v, a verdict of r a record lists, makes.
func findingOf(r Result, v Verdict) Finding {
	if !v.Asked() {
		return Finding{Name: r.Spec, What: declinedWhat(v.Declined)}
	}
	what, _, _ := strings.Cut(v.Detail, "\n")
	return Finding{Name: r.Spec, What: what}
}

// declinedWhat is the What of a decline for the reason d.
func declinedWhat(d Decline) string { return "declined: " + string(d) }

// Unexplained returns, of results, those records does not explain: a result a
// fault ended (Result.Faulted), whole, and one with a finding (explained) its
// oracle's record does not explain, which holds the verdicts of such findings
// alone, the first its outcome. A decline the record lists as a finding and
// does not list here is held as a finding of OutcomeDeclined, so whatever reads
// findings reads it.
func Unexplained(results []Result, records map[Oracle]Recorded) []Result {
	run := byName(results)
	var out []Result
	for _, r := range results {
		if r.Faulted() {
			out = append(out, r)
			continue
		}
		var left []Verdict
		for _, v := range r.Verdicts {
			if !listed(v) || explained(records[v.Oracle], run, r, v) {
				continue
			}
			if !v.Asked() {
				v = Verdict{Oracle: v.Oracle, Outcome: OutcomeDeclined, Detail: declinedWhat(v.Declined)}
			}
			left = append(left, v)
		}
		if len(left) > 0 {
			out = append(out, Result{Spec: r.Spec, Like: r.Like, Parts: r.Parts, Outcome: left[0].Outcome,
				Detail: left[0].Detail, Verdicts: left})
		}
	}
	return out
}

// byName indexes the results naming parts (Result.Parts) by name, and nil
// where none does.
func byName(results []Result) map[string]Result {
	if !slices.ContainsFunc(results, func(r Result) bool { return len(r.Parts) > 0 }) {
		return nil
	}
	out := make(map[string]Result, len(results))
	for _, r := range results {
		out[r.Spec] = r
	}
	return out
}

// explained reports whether rec, or run, the results of the same run by name,
// explains v, a verdict of r its record lists: rec lists what v found at r's
// own name or at the core case r projects to; or v is a spelling of a case
// writing two keywords whose every place it differs at, a part of its What, is
// a place a spelling of either part differs at in run. Each part is held to
// the records itself, so a place forgiven here is one already recorded or
// reported where it stands alone.
func explained(rec Recorded, run map[string]Result, r Result, v Verdict) bool {
	f := findingOf(r, v)
	if rec.Explains(f, r.Spec) || rec.Explains(f, r.Like) {
		return true
	}
	if v.Oracle != OracleSpelling || len(r.Parts) == 0 {
		return false
	}
	places := map[string]bool{}
	for _, name := range r.Parts {
		part, ok := run[name]
		if !ok {
			continue
		}
		for _, w := range part.Verdicts {
			if w.Oracle == v.Oracle && listed(w) {
				for place := range strings.SplitSeq(findingOf(part, w).What, ",") {
					places[place] = true
				}
			}
		}
	}
	for place := range strings.SplitSeq(f.What, ",") {
		if !places[place] {
			return false
		}
	}
	return true
}
