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
	recorded := Recorded{}
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
		if _, dup := recorded[fields[0]]; dup {
			return nil, fmt.Errorf("harness: record line %d: %q is listed twice", n, fields[0])
		}
		recorded[fields[0]] = recordLine{what: fields[1], reason: fields[2]}
	}
	return recorded, nil
}

// Judge holds findings, every finding an oracle made at the core cases, to r,
// whose reasons must be keys of reasons. It returns, each sorted, a finding r
// does not list, one it lists with another What, a line naming no finding,
// and a line whose reason reasons does not name, such as "unclassified".
func (r Recorded) Judge(findings []Finding, reasons map[string]string) (unlisted, changed, stale, unreasoned []string) {
	seen := make(map[string]bool, len(findings))
	for _, finding := range findings {
		seen[finding.Name] = true
		recordLine, ok := r[finding.Name]
		switch {
		case !ok:
			unlisted = append(unlisted, finding.Name+"\t"+finding.What)
		case recordLine.what != finding.What:
			changed = append(changed, fmt.Sprintf("%s: listed %q, found %q", finding.Name, recordLine.what, finding.What))
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

// Explains reports whether r lists finding at as, with finding's What: as is finding's case's
// own name, or the name of the core case it projects to (Result.Like).
func (r Recorded) Explains(finding Finding, as string) bool {
	recordLine, ok := r[as]
	return ok && recordLine.what == finding.What
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
	for _, finding := range sorted {
		reason := unclassified
		if recordLine, ok := r[finding.Name]; ok {
			reason = recordLine.reason
		}
		b.WriteString(finding.Name + "\t" + finding.What + "\t" + reason + "\n")
	}
	return []byte(b.String())
}

// Findings returns what oracle found in each of results, in their order, and each
// decline its record lists as it lists a finding (listed), whose What is
// "declined: <reason>".
func Findings(results []Result, oracle Oracle) []Finding {
	var out []Finding
	for _, result := range results {
		for _, verdict := range result.Verdicts {
			if verdict.Oracle == oracle && listed(verdict) {
				out = append(out, findingOf(result, verdict))
			}
		}
	}
	return out
}

// listed reports whether a record lists verdict: a finding, or one of the two
// declines no case should draw. A case is written to compile and each group to
// be judged, so a document that draws an error and an instance nothing judged
// show as a new finding does. A spelling the oracle cannot compare, for a
// relocation that collides or a document nested too deep, is listed by none.
func listed(verdict Verdict) bool {
	return verdict.Found() || verdict.Declined == DeclineErrorDiagnostic || verdict.Declined == DeclineNotJudged
}

// findingOf is the Finding verdict, a verdict of result a record lists, makes.
func findingOf(result Result, verdict Verdict) Finding {
	if !verdict.Asked() {
		return Finding{Name: result.Spec, What: declinedWhat(verdict.Declined)}
	}
	what, _, _ := strings.Cut(verdict.Detail, "\n")
	return Finding{Name: result.Spec, What: what}
}

// declinedWhat is the What of a decline for the reason decline.
func declinedWhat(decline Decline) string { return "declined: " + string(decline) }

// Unexplained returns, of results, those records does not explain: a result a
// fault ended (Result.Faulted), whole, and one with a finding (explained) its
// oracle's record does not explain, which holds the verdicts of such findings
// alone, the first its outcome. A decline the record lists as a finding and
// does not list here is held as a finding of OutcomeDeclined, so whatever reads
// findings reads it.
func Unexplained(results []Result, records map[Oracle]Recorded) []Result {
	run := byName(results)
	var out []Result
	for _, result := range results {
		if result.Faulted() {
			out = append(out, result)
			continue
		}
		var left []Verdict
		for _, verdict := range result.Verdicts {
			if !listed(verdict) || explained(records[verdict.Oracle], run, result, verdict) {
				continue
			}
			if !verdict.Asked() {
				verdict = Verdict{Oracle: verdict.Oracle, Outcome: OutcomeDeclined, Detail: declinedWhat(verdict.Declined)}
			}
			left = append(left, verdict)
		}
		if len(left) > 0 {
			out = append(out, Result{Spec: result.Spec, Like: result.Like, Parts: result.Parts, Outcome: left[0].Outcome,
				Detail: left[0].Detail, Verdicts: left})
		}
	}
	return out
}

// byName indexes the results naming parts (Result.Parts) by name, and nil
// where none does.
func byName(results []Result) map[string]Result {
	if !slices.ContainsFunc(results, func(result Result) bool { return len(result.Parts) > 0 }) {
		return nil
	}
	out := make(map[string]Result, len(results))
	for _, result := range results {
		out[result.Spec] = result
	}
	return out
}

// explained reports whether rec, or run, the results of the same run by name,
// explains verdict, a verdict of result its record lists: rec lists what verdict found at result's
// own name or at the core case result projects to; or verdict is a spelling of a case
// writing two keywords whose every place it differs at, a part of its What, is
// a place a spelling of either part differs at in run. Each part is held to
// the records itself, so a place forgiven here is one already recorded or
// reported where it stands alone.
func explained(rec Recorded, run map[string]Result, result Result, verdict Verdict) bool {
	finding := findingOf(result, verdict)
	if rec.Explains(finding, result.Spec) || rec.Explains(finding, result.Like) {
		return true
	}
	if verdict.Oracle != OracleSpelling || len(result.Parts) == 0 {
		return false
	}
	places := map[string]bool{}
	for _, name := range result.Parts {
		part, ok := run[name]
		if !ok {
			continue
		}
		for _, verdictValue := range part.Verdicts {
			if verdictValue.Oracle == verdict.Oracle && listed(verdictValue) {
				for place := range strings.SplitSeq(findingOf(part, verdictValue).What, ",") {
					places[place] = true
				}
			}
		}
	}
	for place := range strings.SplitSeq(finding.What, ",") {
		if !places[place] {
			return false
		}
	}
	return true
}
