// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package triage rates a run's findings by the rules a caller writes. A rule
// names one diagnostic code, exactly, and either rates its findings at another
// severity, everywhere or at one place, or accepts the findings at one place,
// so they fail no run. It decides no verdict: the engine judges the rated
// findings at its threshold, and engine/report renders them (architecture §4).
package triage
