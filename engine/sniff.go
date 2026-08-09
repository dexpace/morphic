package engine

import (
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// sniffProbe holds the two discriminating keys read from the source bytes. YAML
// is a JSON superset, so a single yaml decode handles both JSON and YAML specs.
type sniffProbe struct {
	OpenAPI string `yaml:"openapi"`
	Swagger string `yaml:"swagger"`
}

// Sniff probe-decodes the source bytes and reports the spec format they declare.
// An `openapi: 3.X.Y` key yields the openapi compiler keyed by the major.minor
// prefix; `swagger: "2.0"` is recognized but not lowerable yet; anything else,
// undecodable bytes included, yields no format.
//
// ok reports whether a format was read, and diag says why not when it was not.
// There is no Go error return because there is nothing here for one to carry:
// every way a source can defeat Sniff is a problem with that source, and this
// pipeline reports those as diagnostics.
func Sniff(data []byte) (format compilers.SourceFormat, diag ir.Diagnostic, ok bool) {
	var probe sniffProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return compilers.SourceFormat{},
			specProblem(codeUndecodableSource, "decode source: %v", err), false
	}
	switch {
	case probe.OpenAPI != "":
		return compilers.SourceFormat{Name: "openapi", Version: majorMinor(probe.OpenAPI)},
			ir.Diagnostic{}, true
	case probe.Swagger != "":
		return compilers.SourceFormat{}, specProblem(codeUnsupportedFormat,
			"swagger 2.0 is not supported yet (planned: lift into the openapi compiler)"), false
	default:
		return compilers.SourceFormat{},
			specProblem(codeUnrecognizedFormat, "unrecognized spec format"), false
	}
}

// majorMinor returns the "major.minor" prefix of a dotted version string,
// e.g. "3.1.0" → "3.1". Strings with fewer than two dots — a bare major
// version, or a version already in major.minor form — are returned unchanged.
func majorMinor(version string) string {
	firstDot := -1
	for i := 0; i < len(version); i++ {
		if version[i] != '.' {
			continue
		}
		if firstDot < 0 {
			firstDot = i
			continue
		}
		return version[:i]
	}
	return version
}
