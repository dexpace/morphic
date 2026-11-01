// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

// CompareGoldenOn is compareGolden, for the external tests.
var CompareGoldenOn = compareGolden

// EncodedOn is encoded, for the external tests.
var EncodedOn = encoded

// FillOn is fill, for the external tests.
var FillOn = fill

// ReadModuleFilesOn is readModuleFiles, for the external tests.
var ReadModuleFilesOn = readModuleFiles

// RequireOpensOn is requireOpens for a module rooted at /m, whose child writes
// its log to /log, for the external tests.
func RequireOpensOn(t testingT, self, test string, inChild bool,
	run func() ([]byte, error), read func(string) ([]byte, error),
) {
	t.Helper()
	requireOpens(t, opensCheck{
		self: self, test: test, root: "/m", logPath: "/log", inChild: inChild, run: run, read: read,
	})
}

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
