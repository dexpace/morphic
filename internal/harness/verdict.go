// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"
)

// Oracle names one of the checks Check applies to a spec.
type Oracle string

// The oracles, in the order Check applies them. Each finds one Outcome, which
// Oracle.finds names.
const (
	OracleNoPanic           Oracle = "no-panic"
	OracleInvariants        Oracle = "invariants"
	OracleNoErrorDiagnostic Oracle = "no-error-diagnostic"
	OracleRoundTrip         Oracle = "round-trip"
	OracleDeterminism       Oracle = "determinism"
	OracleIdempotence       Oracle = "idempotence"
	OracleOrder             Oracle = "order-invariance"
	OracleSequenceOrder     Oracle = "sequence-order-invariance"
)

// The case oracles, in the order CheckCase applies them, each comparing a
// case with a neighbour (Document says which).
const (
	// OracleSpelling holds a case to the partner spelling it must equal.
	OracleSpelling Oracle = "spelling"
	// OraclePresence holds a case to the case leaving its construct out and
	// to the case writing its other value.
	OraclePresence Oracle = "presence"
	// OracleInstance holds each valid instance a case writes as a default to
	// the type it compiles to.
	OracleInstance Oracle = "instance"
)

// Oracles returns every oracle in the order Check applies them.
func Oracles() []Oracle {
	return []Oracle{
		OracleNoPanic, OracleInvariants, OracleNoErrorDiagnostic, OracleRoundTrip,
		OracleDeterminism, OracleIdempotence, OracleOrder, OracleSequenceOrder,
	}
}

// CaseOracles returns every case oracle in the order CheckCase applies them.
func CaseOracles() []Oracle {
	return []Oracle{OracleSpelling, OraclePresence, OracleInstance}
}

// finds returns the outcome o reports when it fires, and false for a name that
// is no oracle.
func (o Oracle) finds() (Outcome, bool) {
	switch o {
	case OracleNoPanic:
		return OutcomePanic, true
	case OracleInvariants:
		return OutcomeViolations, true
	case OracleNoErrorDiagnostic:
		return OutcomeErrorDiag, true
	case OracleRoundTrip:
		return OutcomeRoundtrip, true
	case OracleDeterminism:
		return OutcomeNondeterministic, true
	case OracleIdempotence:
		return OutcomeNonIdempotent, true
	case OracleOrder:
		return OutcomeOrderDependent, true
	case OracleSequenceOrder:
		return OutcomeSequenceOrderDependent, true
	case OracleSpelling:
		return OutcomeSpellingDependent, true
	case OraclePresence:
		return OutcomeNotCarried, true
	case OracleInstance:
		return OutcomeInstanceExcluded, true
	default:
		return "", false
	}
}

// idle returns why o, a case oracle, asks a case nothing when the case's
// document gives it nothing to compare, and "" for an oracle that is none. A
// case oracle CheckCase holds no entry for declines for it.
func idle(o Oracle) Decline {
	switch o {
	case OracleSpelling:
		return DeclineNoPartner
	case OraclePresence:
		return DeclineNoConstruct
	case OracleInstance:
		return DeclineNoInstance
	default:
		return ""
	}
}

// records reports whether o's record lists a decline for the reason d, as it
// lists a finding: a case gave o something to ask, and o could not ask it. The
// instance oracle's alone, every group the test suite holds being one to ask,
// so a group that starts to decline shows.
func (o Oracle) records(d Decline) bool {
	return o == OracleInstance && (d == DeclineErrorDiagnostic || d == DeclineNotJudged)
}

// notReached is why an oracle a fault ended the run before declines.
func notReached(Oracle) Decline { return DeclineNotReached }

// Decline says why an oracle asked a spec nothing. A sweep counts each
// (Summary), so an oracle that could not ask is never read as one that passed.
type Decline string

// Why an oracle declines. The reasons after DeclineNoDocument are the two
// order oracles' alone: what a permutation cannot rewrite faithfully, or
// leaves nothing to compare.
const (
	// DeclineNotReached: the run ended before the oracle, as Result.Outcome
	// says: a panic, a Go error, or a source no registered compiler takes.
	DeclineNotReached Decline = "not reached"
	// DeclineNoDocument: the compile refused the source, so there is no
	// document to hold to the oracle.
	DeclineNoDocument Decline = "no document"
	// DeclineNotYAML: the source does not parse as YAML, so it cannot be
	// permuted.
	DeclineNotYAML Decline = "not YAML"
	// DeclineDuplicateKey: a mapping declares one key twice, so reversing it
	// changes which declaration wins.
	DeclineDuplicateKey Decline = "duplicate key"
	// DeclineAnchorRedefined: two nodes carry one anchor name, so moving them
	// can rebind an alias to the other.
	DeclineAnchorRedefined Decline = "anchor redefined"
	// DeclineTooDeep: the source nests deeper than any document the compiler
	// admits, or what the sequence-order oracle compares nests past its own
	// bound.
	DeclineTooDeep Decline = "too deep"
	// DeclineTooManyDocuments: the source is a stream of more documents than
	// the rewrite reads.
	DeclineTooManyDocuments Decline = "too many documents"
	// DeclineUnencodable: the parsed source does not encode again.
	DeclineUnencodable Decline = "does not re-encode"
	// DeclinePermutationUnparsable: the permuted source does not parse again,
	// which is the rewrite's own fault.
	DeclinePermutationUnparsable Decline = "permutation does not parse"
	// DeclineNothingToPermute: the permutation changes nothing, so both orders
	// are one: no mapping holds two entries, or no allOf or required sequence
	// of two or more is the sequence-order oracle's to reverse.
	DeclineNothingToPermute Decline = "nothing to permute"
	// DeclineReencodingNoSpec: the source re-encoded reads as no spec any
	// registered compiler takes, so there is no baseline to compare with.
	DeclineReencodingNoSpec Decline = "re-encoding is no spec"
	// DeclineReencodingRefused: the source compiles to a document and its
	// re-encoding to none, so the two orders would compare a refusal the
	// rewrite made rather than what the source compiles to.
	DeclineReencodingRefused Decline = "re-encoding refused"
	// DeclineReadsAnotherFile: the source's compile reads another file, which
	// can name an element of a sequence by its index. The sequence-order
	// oracle reverses the source alone and reads no other file's references,
	// so reversing would change what such a reference names.
	DeclineReadsAnotherFile Decline = "reads another file"
	// DeclineReferenceCycle: nodes beneath the sequences the sequence-order
	// oracle reverses name each other in a cycle of two or more. What tells
	// such nodes apart would follow which of them the comparison met first,
	// not what they say.
	DeclineReferenceCycle Decline = "reference cycle"
)

// Why a case oracle declines.
const (
	// DeclineNoPartner: the case names no spelling it must equal.
	DeclineNoPartner Decline = "no partner"
	// DeclineRelocationCollides: two names of one object read alike once
	// relocated to the partner's coordinates, so the comparison would lose one.
	DeclineRelocationCollides Decline = "relocation collides"
	// DeclineNoConstruct: the case names no case leaving its construct out,
	// as for the case writing no construct and for a construct's other value.
	DeclineNoConstruct Decline = "no construct"
	// DeclineNoInstance: the case writes no instance.
	DeclineNoInstance Decline = "no instance"
	// DeclineNotJudged: the compiled document does not hold an instance where
	// the case wrote it, so nothing judged it.
	DeclineNotJudged Decline = "not judged"
	// DeclineErrorDiagnostic: a document the oracle compares draws an error
	// diagnostic, so it compares a refusal rather than what the case says.
	DeclineErrorDiagnostic Decline = "error diagnostic"
)

// Verdict is one oracle's answer for one spec: it asked and found nothing
// (Outcome is OutcomeOK), asked and found something (Outcome is what the
// oracle finds, described by Detail), or declined (Declined says why, and
// Outcome is empty).
type Verdict struct {
	// Oracle is the oracle answering.
	Oracle Oracle
	// Outcome is OutcomeOK or the oracle's finding; empty exactly when it
	// declined.
	Outcome Outcome
	// Declined is why the oracle asked nothing; empty exactly when it asked.
	Declined Decline
	// Detail describes a finding; empty otherwise.
	Detail string
}

// Asked reports whether the oracle asked its question of the spec.
func (v Verdict) Asked() bool { return v.Declined == "" }

// Found reports whether the oracle asked and found something.
func (v Verdict) Found() bool { return v.Asked() && v.Outcome != OutcomeOK }

// passed is o's verdict on a spec it asked and found nothing in.
func passed(o Oracle) Verdict { return Verdict{Oracle: o, Outcome: OutcomeOK} }

// found is o's verdict on a spec it found what detail describes in. A detail
// is required: a finding nobody can read is no finding.
func found(o Oracle, detail string) Verdict {
	outcome, ok := o.finds()
	if !ok || detail == "" {
		panic(fmt.Sprintf("harness: finding %q by %q", detail, o))
	}
	return Verdict{Oracle: o, Outcome: outcome, Detail: detail}
}

// declined is o's verdict on a spec it asked nothing of, for the reason why.
func declined(o Oracle, why Decline) Verdict {
	if why == "" {
		panic(fmt.Sprintf("harness: %q declined for no reason", o))
	}
	return Verdict{Oracle: o, Declined: why}
}

// judge is o's verdict from an oracle function reporting ok, or what it found.
func (o Oracle) judge(detail string, ok bool) Verdict {
	if ok {
		return passed(o)
	}
	return found(o, detail)
}

// tally is what Summary counts for one oracle.
type tally struct {
	asked, found int
	declined     map[Decline]int
}

// Summary renders, for each oracle in Check's order, how many of the results
// it asked its question of, how many of those it found something in, and how
// many it declined, by reason. A result missing an oracle's verdict, which no
// Check returns, counts as not reached.
func Summary(results []Result) string { return summary(results, Oracles()) }

// CaseSummary renders what Summary does of CheckCases' results, for each case
// oracle in CheckCase's order.
func CaseSummary(results []Result) string { return summary(results, CaseOracles()) }

// summary renders, for each of oracles in order, what Summary says of it.
func summary(results []Result, oracles []Oracle) string {
	tallies := make(map[Oracle]*tally, len(oracles))
	for _, o := range oracles {
		tallies[o] = &tally{declined: map[Decline]int{}}
	}
	for _, r := range results {
		answered := map[Oracle]Verdict{}
		for _, v := range r.Verdicts {
			answered[v.Oracle] = v
		}
		for _, o := range oracles {
			count(tallies[o], answered[o])
		}
	}

	width := 0
	for _, o := range oracles {
		width = max(width, utf8.RuneCountInString(string(o)))
	}
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%-*s %6s %6s %8s\n", width, "oracle", "asked", "found", "declined")
	for _, o := range oracles {
		t := tallies[o]
		line := fmt.Sprintf("%-*s %6d %6d %8d", width, o, t.asked, t.found, total(t.declined))
		if reasons := reasonsOf(t.declined); reasons != "" {
			line += "  " + reasons
		}
		_, _ = fmt.Fprintln(&b, line)
	}
	return b.String()
}

// count adds v to t; a zero Verdict is an oracle the result has none from.
func count(t *tally, v Verdict) {
	switch {
	case v.Oracle == "":
		t.declined[DeclineNotReached]++
	case !v.Asked():
		t.declined[v.Declined]++
	default:
		t.asked++
		if v.Found() {
			t.found++
		}
	}
}

// total is how many declines byReason holds.
func total(byReason map[Decline]int) int {
	n := 0
	for _, c := range byReason {
		n += c
	}
	return n
}

// reasonsOf renders byReason as "reason count" pairs, most frequent first and
// alphabetical among equals, so a summary reads the same on every run.
func reasonsOf(byReason map[Decline]int) string {
	reasons := slices.SortedFunc(maps.Keys(byReason), func(a, b Decline) int {
		return cmp.Or(cmp.Compare(byReason[b], byReason[a]), cmp.Compare(a, b))
	})
	parts := make([]string, len(reasons))
	for i, r := range reasons {
		parts[i] = fmt.Sprintf("%s %d", r, byReason[r])
	}
	return strings.Join(parts, ", ")
}
