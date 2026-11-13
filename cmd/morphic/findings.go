// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"context"
	"io"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/engine/report"
	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/ir"
)

// defaultDiagLimit is how many findings of one code and severity the text
// format writes when --diag-limit is not given: enough to show what the code
// reports and where, few enough that a spec drawing thousands of findings
// fits on a screen, its counts beside them.
const defaultDiagLimit = 3

// readTriage reads the triage file path names, or none for an empty path.
func readTriage(ctx context.Context, path string) ([]triage.Rule, error) {
	if path == "" {
		return nil, nil
	}
	return engine.ReadTriage(ctx, path)
}

// writeFindings writes res's findings to stderr in the format opts names: one
// JSON report, the verdict, held and any warning about the run's cache in it,
// or text, as many as --show and --diag-limit admit, then that warning.
func writeFindings(stderr io.Writer, opts specOptions, res *engine.Result, held []error) error {
	r := report.Report{Failed: res.Failed, Sources: res.Sources, Findings: res.Findings, UnusedRules: res.UnusedRules}
	if opts.format == formatJSON {
		if res.CacheErr != nil {
			held = append(held, res.CacheErr)
		}
		for _, w := range held {
			r.Warnings = append(r.Warnings, warningText(w))
		}
		return report.WriteJSON(stderr, r)
	}
	textOpts := report.TextOptions{Least: ir.Severity(opts.show), PerCode: int(opts.limit)}
	if err := report.WriteText(stderr, r, textOpts); err != nil {
		return err
	}
	if res.CacheErr != nil {
		warnCache(stderr, res.CacheErr)
	}
	return nil
}
