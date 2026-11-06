// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

import "time"

// CompareGoldenOn is compareGolden, for the external tests.
var CompareGoldenOn = compareGolden

// EncodedOn is encoded, for the external tests.
var EncodedOn = encoded

// FillOn is fill, for the external tests.
var FillOn = fill

// ReadModuleFilesOn is readModuleFiles, for the external tests.
var ReadModuleFilesOn = readModuleFiles

// RequireOpensOn is requireOpens for a child that must open paths and writes
// its log to /log, for the external tests.
func RequireOpensOn(t testingT, self, test string, paths []string, inChild bool, remaining time.Duration,
	bounded bool, run func(args []string) ([]byte, error), read func(string) ([]byte, error),
) {
	t.Helper()
	requireOpens(t, opensCheck{
		self: self, test: test, paths: paths, logPath: "/log", inChild: inChild,
		remaining: remaining, bounded: bounded, run: run, read: read,
	})
}

// ChildArgs is childArgs, for the external tests.
var ChildArgs = childArgs

// UnopenedIn is unopened, for the external tests.
var UnopenedIn = unopened

// OpensChildEnv is opensChildEnv, for the external tests.
const OpensChildEnv = opensChildEnv

// RootFrom is rootFrom, for the external tests.
var RootFrom = rootFrom

// UpdateFlag is update, for the external tests.
var UpdateFlag = update

// VisitMeasurements is visitMeasurements, for the external tests.
const VisitMeasurements = visitMeasurements
