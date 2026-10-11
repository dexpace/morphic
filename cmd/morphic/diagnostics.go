package main

import (
	"io"
	"os"
	"strconv"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// renderDiagnostics writes each diagnostic to w, one per line, as
// "<severity> <code> <location>: <message>". This is the sole place in the
// pipeline where diagnostics are rendered for a human.
//
// On a terminal the severity is coloured and the location dimmed. Anywhere else
// the styling is removed before it reaches w, so a log, a pipe or a problem
// matcher reads the plain line it always did.
func renderDiagnostics(w io.Writer, res *engine.Result) {
	writeDiagnostics(colorprofile.NewWriter(w, os.Environ()), res)
}

// writeDiagnostics writes the styled lines of renderDiagnostics to w. Whether
// the styling survives is w's to decide: a colorprofile.Writer downsamples it to
// the terminal's palette, or strips it.
func writeDiagnostics(w io.Writer, res *engine.Result) {
	for _, d := range res.Diagnostics {
		emitf(w, "%s %s%s: %s\n",
			severityStyle(d.Severity).Render(string(d.Severity)), d.Code,
			styledLocation(location(res.Sources, d.Provenance)), d.Message)
	}
}

// severityStyle picks how a severity is drawn. It uses the terminal's own
// sixteen colours, so it reads on a light or a dark theme without asking the
// terminal which it is, and bold keeps the two that fail a gate apart from the
// one that does not. A severity it does not know is left plain.
func severityStyle(s ir.Severity) lipgloss.Style {
	style := lipgloss.NewStyle()
	switch s {
	case ir.SeverityError:
		return style.Foreground(lipgloss.Red).Bold(true)
	case ir.SeverityWarning:
		return style.Foreground(lipgloss.Yellow).Bold(true)
	case ir.SeverityInfo:
		return style.Foreground(lipgloss.Cyan)
	default:
		return style
	}
}

// styledLocation dims loc, which is empty or has a leading space. Dimming
// rather than a colour keeps it legible on any theme: bright black is the
// background colour on some. An empty location stays empty, so a line with none
// carries no stray escape.
func styledLocation(loc string) string {
	if loc == "" {
		return ""
	}
	return lipgloss.NewStyle().Faint(true).Render(loc)
}

// location renders the "where" of a diagnostic line, leading space included:
// " <path>#<pointer>" for a pointer, " <path>:<line>:<column>" for a position
// (the form editors and terminals turn into a link), " <path>" for the source
// as a whole, and the bare node for an IR-space node. A finding with several
// locators is spelled by the first of them in that order.
//
// sources is the run's table, which a refused compile has too. A locator whose
// index names no entry in it is spelled without a path, and a finding naming
// neither a source nor a node prints nothing rather than a guess.
func location(sources []ir.SourceInfo, prov ir.Provenance) string {
	path := sourcePath(sources, prov.Source)
	switch {
	case prov.Pointer != "":
		return " " + onPath(path, "#", string(prov.Pointer))
	case prov.Position.Line > 0:
		return " " + onPath(path, ":", positionText(prov.Position))
	case path != "":
		return " " + path
	case prov.Node != "":
		return " " + prov.Node
	default:
		return ""
	}
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
