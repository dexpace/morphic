// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package report renders what a pipeline run produced: its findings as rated,
// for a person one per line with what it withheld counted, or for a program as
// one versioned JSON object, and what it produced at one source coordinate. It
// is the one renderer, so a driver shows a finding exactly as the CLI does;
// every stage below the engine emits typed ir.Diagnostic values and renders
// nothing.
package report
