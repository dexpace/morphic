// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import "strings"

// withFormats returns long with a paragraph after its first naming the formats
// this build compiles. Which compiler reads a spec is the engine's to decide,
// so the help names what its compilers serve rather than a format written into
// the text, which a compiler registered later would make wrong.
func withFormats(long string) string {
	first, rest, _ := strings.Cut(long, "\n\n")
	return first + "\n\n" + servedFormats() + "\n\n" + rest
}

// servedFormats is the sentence naming every format the engine a run would use
// compiles, or saying why it cannot be built, which every run would then report.
func servedFormats() string {
	eng, err := newEngine()
	if err != nil {
		return "The formats this build compiles cannot be listed: " + err.Error() + "."
	}
	formats := eng.Formats()
	names := make([]string, 0, len(formats))
	for _, f := range formats {
		names = append(names, f.String())
	}
	return "This build compiles " + strings.Join(names, ", ") + "."
}
