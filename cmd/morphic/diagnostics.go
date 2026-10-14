package main

import (
	"io"
	"strconv"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// renderDiagnostics writes each diagnostic to w, one per line, as
// "<severity> <code> <location>: <message>". This is the sole place in the
// pipeline where diagnostics are rendered for a human.
func renderDiagnostics(w io.Writer, res *engine.Result) {
	for _, d := range res.Diagnostics {
		emitf(w, "%s %s%s: %s\n",
			d.Severity, d.Code, location(res.Sources, d.Provenance), d.Message)
	}
}

// location renders the "where" of a diagnostic line, leading space included:
// " <path>#<pointer>" for a pointer, " <path>:<line>:<column>" for a position
// (the form editors and terminals turn into a link), both as
// " <path>:<line>:<column> (#<pointer>)", " <path>" for the source as a whole,
// and the bare node for an IR-space node, the first of these that applies.
//
// sources is the run's table, which a refused compile has too. A locator whose
// index names no entry in it is spelled without a path, and a finding naming
// neither a source nor a node prints nothing rather than a guess.
func location(sources []ir.SourceInfo, prov ir.Provenance) string {
	path := sourcePath(sources, prov.Source)
	switch {
	case prov.Pointer != "" && prov.Position.Line > 0:
		return " " + onPath(path, ":", positionText(prov.Position)) + " (#" + string(prov.Pointer) + ")"
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
