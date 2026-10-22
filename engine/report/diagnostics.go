// Package report renders what a pipeline run produced for a person to read:
// its diagnostics, one per line, and what it produced at one source
// coordinate. It is the one renderer, so a driver shows a finding exactly as
// the CLI does; every stage below the engine emits typed ir.Diagnostic values
// and renders nothing.
package report

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// errNoWriter reports a renderer handed no writer to render to.
var errNoWriter = errors.New("report: no writer to write to")

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

// WriteDiagnostics writes each of diags to w, one per line, as
// "<severity> <code><location>: <message>". sources is the table each
// Provenance.Source indexes, a run's engine.Result.Sources. It stops at the
// first write that fails and returns that write's error, and refuses a nil w
// whether or not there is a line to write.
func WriteDiagnostics(w io.Writer, diags []ir.Diagnostic, sources []ir.SourceInfo) error {
	if w == nil {
		return errNoWriter
	}
	lw := &lineWriter{w: w}
	for _, d := range diags {
		lw.printf("%s %s%s: %s\n", d.Severity, d.Code, location(sources, d.Provenance), d.Message)
	}
	return lw.err
}

// location renders the "where" of a diagnostic line, leading space included,
// as the first of these that applies: " <path>:<line>:<column> (#<pointer>)"
// for a position and a pointer, the position first so editors link it;
// " <path>#<pointer>" for a pointer; " <path>:<line>:<column>" for a position;
// " <path>" for the source as a whole; and the bare node for an IR-space node.
//
// A locator whose index names no entry of sources is spelled without a path,
// and a finding naming neither a source nor a node prints nothing rather than
// a guess.
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
