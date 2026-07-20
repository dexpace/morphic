package engine

import (
	"fmt"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/frontend"
)

// sniffProbe holds the two discriminating keys read from the source bytes. YAML
// is a JSON superset, so a single yaml decode handles both JSON and YAML specs.
type sniffProbe struct {
	OpenAPI string `yaml:"openapi"`
	Swagger string `yaml:"swagger"`
}

// Sniff probe-decodes the source bytes and reports the spec format they declare.
// An `openapi: 3.X.Y` key yields the openapi frontend keyed by the major.minor
// prefix; `swagger: "2.0"` is recognized but unsupported; anything else is an
// error. Undecodable bytes yield a wrapped decode error.
func Sniff(data []byte) (frontend.SourceFormat, error) {
	var probe sniffProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return frontend.SourceFormat{}, fmt.Errorf("sniff: decode source: %w", err)
	}
	switch {
	case probe.OpenAPI != "":
		return frontend.SourceFormat{Name: "openapi", Version: majorMinor(probe.OpenAPI)}, nil
	case probe.Swagger != "":
		return frontend.SourceFormat{}, fmt.Errorf(
			"swagger 2.0 is not supported yet (planned: lift into the openapi frontend)")
	default:
		return frontend.SourceFormat{}, fmt.Errorf("unrecognized spec format")
	}
}

// majorMinor returns the "major.minor" prefix of a dotted version string,
// e.g. "3.1.0" → "3.1". Strings with fewer than two dot-separated components are
// returned unchanged.
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
