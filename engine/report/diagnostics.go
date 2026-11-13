// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package report

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/ir"
)

// errNoWriter reports a renderer handed no writer to render to.
var errNoWriter = errors.New("report: no writer to write to")

// Report is what a run returned that the renderers read, taken from an
// engine.Result, beside what the driver adds: WriteText renders Findings,
// UnusedRules and Sources, and WriteJSON every field.
type Report struct {
	// Failed is the run's verdict, engine.Result.Failed.
	Failed bool `json:"failed"`
	// Sources is the table each finding's Provenance.Source indexes,
	// engine.Result.Sources.
	Sources []ir.SourceInfo `json:"sources"`
	// Findings is every finding as rated, engine.Result.Findings.
	Findings []triage.Finding `json:"findings"`
	// UnusedRules is engine.Result.UnusedRules: the rules no finding met.
	UnusedRules []triage.Rule `json:"unusedRules"`
	// Warnings is what kept the run from working as asked without failing it,
	// such as a cache it could not use, one line each, and never a finding.
	Warnings []string `json:"warnings"`
}

// TextOptions says how many of a run's findings WriteText shows one per line.
// Its zero value shows each finding no rule accepted.
type TextOptions struct {
	// Least is the least severity shown one per line: a finding rated below it
	// is counted. Empty shows every severity.
	Least ir.Severity
	// PerCode is how many findings of one code and rating are shown one per
	// line, the first in reported order, the rest counted. Zero shows each.
	PerCode int
}

// lineWriter writes formatted lines to w until one write fails, and keeps that
// failure, so a renderer writing many lines reports the first and stops.
type lineWriter struct {
	w   io.Writer
	err error
}

// printf writes one formatted line unless an earlier write failed.
func (lw *lineWriter) printf(format string, args ...any) {
	if lw.err != nil {
		return
	}
	_, lw.err = fmt.Fprintf(lw.w, format, args...)
}

// groupKey is a code and the severity its findings are rated at.
type groupKey struct {
	severity ir.Severity
	code     string
}

// group is the findings of one code at one rating: how many are shown one per
// line and how many only counted.
type group struct {
	groupKey
	shown    int
	withheld int
}

// WriteText writes r's findings to w for a person, one per line as
// "<severity> <code><location>: <message>" at the rated severity, in reported
// order, as many as opts admits. Then, indented so no line parser reads it as
// a finding, a line for each code and rating it withheld findings of, one per
// unused rule, and, once it withheld any or a rule accepted any, a tally.
// It escapes every string a spec can write (printable).
//
// It refuses a nil w and options naming no severity or a negative count, and
// stops at the first write that fails, returning its error.
func WriteText(w io.Writer, r Report, opts TextOptions) error {
	if w == nil {
		return errNoWriter
	}
	least, err := opts.least()
	if err != nil {
		return err
	}
	lw := &lineWriter{w: w}
	var groups []*group
	byKey := map[groupKey]*group{}
	accepted := 0
	for _, f := range r.Findings {
		if f.Accepted {
			accepted++
			continue
		}
		at := groupKey{severity: f.Severity, code: f.Diagnostic.Code}
		g, seen := byKey[at]
		if !seen {
			g = &group{groupKey: at}
			byKey[at], groups = g, append(groups, g)
		}
		rank, ranked := triage.Rank(f.Severity)
		if (ranked && rank < least) || (opts.PerCode > 0 && g.shown == opts.PerCode) {
			g.withheld++
			continue
		}
		g.shown++
		lw.printf("%s\n", line(f, r.Sources))
	}
	writeFooter(lw, r, groups, accepted)
	return lw.err
}

// least returns the rank of the least severity o shows, or why o names none.
func (o TextOptions) least() (int, error) {
	if o.PerCode < 0 {
		return 0, fmt.Errorf("report: %d findings of a code is no count to show", o.PerCode)
	}
	if o.Least == "" {
		return 0, nil
	}
	rank, ranked := triage.Rank(o.Least)
	if !ranked {
		return 0, fmt.Errorf("report: %q is no severity to show from", string(o.Least))
	}
	return rank, nil
}

// writeFooter writes what follows the findings WriteText shows: what it
// withheld of each group, the unused rules, and the tally once anything was
// withheld or accepted.
func writeFooter(lw *lineWriter, r Report, groups []*group, accepted int) {
	withheld := accepted
	for _, g := range groups {
		withheld += g.withheld
		switch {
		case g.withheld == 0:
		case g.shown > 0:
			lw.printf("  %d more %s %s not shown\n", g.withheld, printable(string(g.severity)), printable(g.code))
		default:
			lw.printf("  %d %s %s not shown\n", g.withheld, printable(string(g.severity)), printable(g.code))
		}
	}
	for _, rule := range r.UnusedRules {
		lw.printf("  triage rule for %s%s matched no finding\n", printable(rule.Code), rulePlace(rule))
	}
	if withheld == 0 {
		return
	}
	counts := map[ir.Severity]int{}
	for _, f := range r.Findings {
		if !f.Accepted {
			counts[f.Severity]++
		}
	}
	var tally strings.Builder
	for _, sev := range slices.Backward(triage.Severities()) {
		fmt.Fprintf(&tally, "%d %s, ", counts[sev], sev)
	}
	lw.printf("  %d of %d findings shown: %s%d accepted\n", len(r.Findings)-withheld, len(r.Findings), tally.String(), accepted)
}

// rulePlace spells where rule rules, leading space included: " at <pointer>",
// " at node <node>", or nothing for a rule naming its code alone.
func rulePlace(rule triage.Rule) string {
	switch {
	case rule.Pointer != "":
		return " at " + printable(string(rule.Pointer))
	case rule.Node != "":
		return " at node " + printable(rule.Node)
	default:
		return ""
	}
}

// line renders f as WriteText writes it, newline aside.
func line(f triage.Finding, sources []ir.SourceInfo) string {
	d := f.Diagnostic
	return printable(string(f.Severity)) + " " + printable(d.Code) + location(sources, d.Provenance) +
		writer(sources, d.Provenance) + ": " + printable(d.Message)
}

// location renders the "where" of a diagnostic line, leading space included,
// as the first of these that applies: " <path>:<line>:<column> (#<pointer>)"
// for a position and a pointer, the position first so editors link it;
// " <path>#<pointer>" for a pointer; " <path>:<line>:<column>" for a position;
// " <path>" for the source as a whole; and the bare node for an IR-space node.
//
// A locator whose index names no entry of sources is spelled without a path,
// and a finding naming neither a source nor a node prints nothing rather than
// a guess. Each part is escaped (printable).
func location(sources []ir.SourceInfo, prov ir.Provenance) string {
	path := printable(sourcePath(sources, prov.Source))
	pointer := printable(string(prov.Pointer))
	switch {
	case pointer != "" && prov.Position.Line > 0:
		return " " + onPath(path, ":", positionText(prov.Position)) + " (#" + pointer + ")"
	case pointer != "":
		return " " + onPath(path, "#", pointer)
	case prov.Position.Line > 0:
		return " " + onPath(path, ":", positionText(prov.Position))
	case path != "":
		return " " + path
	case prov.Node != "":
		return " " + printable(prov.Node)
	default:
		return ""
	}
}

// writer renders who wrote what prov locates, leading space included, as
// " (written by <path>)" when an input other than its Source did, and nothing
// otherwise, nor for a writer index naming no entry of sources.
func writer(sources []ir.SourceInfo, prov ir.Provenance) string {
	if prov.WrittenBy == 0 {
		return ""
	}
	path := sourcePath(sources, prov.WrittenBy)
	if path == "" {
		return ""
	}
	return " (written by " + printable(path) + ")"
}

// onPath spells locator against path, joined by sep, or alone when no path
// resolved.
func onPath(path, sep, locator string) string {
	if path == "" {
		return locator
	}
	return path + sep + locator
}

// positionText spells a position as <line>:<column>, or <line> alone when the
// producer knows no column.
func positionText(p ir.Position) string {
	if p.Column > 0 {
		return strconv.Itoa(p.Line) + ":" + strconv.Itoa(p.Column)
	}
	return strconv.Itoa(p.Line)
}

// sourcePath resolves a diagnostic's source index to its file path, returning
// "" when the index addresses no entry of sources.
func sourcePath(sources []ir.SourceInfo, source int) string {
	if source < 0 || source >= len(sources) {
		return ""
	}
	return sources[source].Path
}
