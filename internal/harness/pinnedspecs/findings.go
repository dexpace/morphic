// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pinnedspecs

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// Findings returns the lines a record of result, a spec's Check, lists: each
// diagnostic of the spec's compile, named by its code and place (place), what
// was found being its severity, and each oracle that found something or
// declined, named "oracle" and the oracle, what being its detail's first line
// or why it declined. Diagnostics of one code at one place are one finding,
// listing each one's severity. A result a fault ended is refused: its oracles
// asked nothing, and what it holds is no account of the spec.
func Findings(result harness.Result) ([]harness.Finding, error) {
	if result.Spec == "" {
		return nil, errors.New("pinnedspecs: a result naming no spec")
	}
	if result.Faulted() {
		return nil, fmt.Errorf("pinnedspecs: %s: the run ended in a fault: %s: %s", result.Spec, result.Outcome, result.Detail)
	}

	severities := map[string][]string{}
	for _, diagnostic := range result.Diagnostics {
		name := diagnostic.Code + " " + place(diagnostic.Provenance)
		severities[name] = append(severities[name], string(diagnostic.Severity))
	}
	findings := make([]harness.Finding, 0, len(severities)+len(result.Verdicts))
	for _, name := range slices.Sorted(maps.Keys(severities)) {
		slices.Sort(severities[name])
		findings = append(findings, harness.Finding{Name: name, What: strings.Join(severities[name], ", ")})
	}
	for _, verdict := range result.Verdicts {
		name := "oracle " + string(verdict.Oracle)
		switch {
		case !verdict.Asked():
			findings = append(findings, harness.Finding{Name: name, What: "declined: " + string(verdict.Declined)})
		case verdict.Found():
			first, _, _ := strings.Cut(verdict.Detail, "\n")
			findings = append(findings, harness.Finding{Name: name, What: recordable(first)})
		}
	}
	return findings, nil
}

// place is where a diagnostic at provenance sits, as a record names it: its
// pointer, failing that its line and column, failing both the document, each
// after its source's index where that source is not the spec's own; or, with
// no pointer but an IR node, that node alone, because an IR ID names one node
// whichever source it came from.
func place(provenance ir.Provenance) string {
	if provenance.Pointer == "" && provenance.Node != "" {
		return "node " + recordable(provenance.Node)
	}
	at := "document"
	switch {
	case provenance.Pointer != "":
		at = recordable(string(provenance.Pointer))
	case provenance.Position != ir.Position{}:
		at = fmt.Sprintf("line %d column %d", provenance.Position.Line, provenance.Position.Column)
	}
	if provenance.Source != 0 {
		return fmt.Sprintf("source %d %s", provenance.Source, at)
	}
	return at
}

// recordable is text a record can hold on one line between tabs: text as it
// is, or quoted where it holds a tab or a line break. Neither a pointer nor an
// IR ID starts with a quote, so a quoted one reads as no other.
func recordable(text string) string {
	if strings.ContainsAny(text, "\t\n\r") {
		return strconv.Quote(text)
	}
	return text
}
