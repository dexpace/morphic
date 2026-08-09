package openapi

import (
	"bytes"
	"encoding/json"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
)

// maxSniffBytes bounds the bytes Detect parses. Detection reads two top-level
// keys, and 64 KiB reaches them in any document a person wrote, so the cost of
// asking stays flat while spec size does not: a full parse of a 10 MB document
// costs hundreds of milliseconds before the compiler's own parse begins.
const maxSniffBytes = 64 << 10

// maxSniffEntries bounds the top-level entries read from a flow-style prefix.
// The keys being looked for are declared among a document's first few, and a
// prefix full of nothing else is not one this compiler will take.
const maxSniffEntries = 512

// sniffProbe holds the two discriminating top-level keys. Which one is present
// is the whole of the format question: an OpenAPI 3.x document declares
// `openapi`, a Swagger 2.0 document declares `swagger`.
type sniffProbe struct {
	OpenAPI string `yaml:"openapi"`
	Swagger string `yaml:"swagger"`
}

// Detect implements compilers.Compiler. It reports the dialect src declares,
// keyed by the major.minor prefix of the version string.
//
// It names swagger@2.0 as well, which this compiler does not serve: a Swagger
// document is recognizably an API spec, and reporting it as one lets the caller
// say the format is unsupported rather than that the file is unreadable. The
// path is not consulted — an OpenAPI document is what it declares itself to be,
// under any extension.
func (*Compiler) Detect(src compilers.Source) (compilers.SourceFormat, bool) {
	probe := sniff(src.Data)
	switch {
	case probe.OpenAPI != "":
		return compilers.SourceFormat{Name: "openapi", Version: majorMinor(probe.OpenAPI)}, true
	case probe.Swagger != "":
		return compilers.SourceFormat{Name: "swagger", Version: majorMinor(probe.Swagger)}, true
	default:
		return compilers.SourceFormat{}, false
	}
}

// sniff reads the discriminating keys out of at most maxSniffBytes bytes, and
// reports the zero probe for anything it cannot read — bytes of another format
// are ordinary input here, and a parser's complaint about them answers no
// question a caller asked.
//
// A document within the cap is decoded whole and exactly. A larger one is
// decoded from a prefix, which cannot simply be cut: flow style — JSON is the
// common case — is one token stream with no line structure, so its entries are
// streamed instead, and block style is cut at its last complete line.
func sniff(data []byte) sniffProbe {
	if len(data) <= maxSniffBytes {
		return decodeYAML(data)
	}
	prefix := data[:maxSniffBytes]
	if probe, ok := decodeFlowPrefix(prefix); ok {
		return probe
	}
	return decodeYAML(wholeLines(prefix))
}

// decodeYAML reads the probe keys from a complete YAML (or JSON, its subset)
// document.
func decodeYAML(data []byte) sniffProbe {
	var probe sniffProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return sniffProbe{}
	}
	return probe
}

// decodeFlowPrefix reads the top-level entries of a prefix that opens a flow
// mapping, and reports whether it was one. The JSON decoder is used because it
// streams: a prefix cut mid-document still yields every entry it completed,
// where decoding those same bytes whole reports only that they end early.
func decodeFlowPrefix(prefix []byte) (sniffProbe, bool) {
	dec := json.NewDecoder(bytes.NewReader(prefix))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return sniffProbe{}, false
	}

	var probe sniffProbe
	for range maxSniffEntries {
		key, err := dec.Token()
		if err != nil {
			break
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			break
		}
		recordEntry(&probe, key, value)
	}
	return probe, true
}

// recordEntry stores value under probe's field for key. key is compared as read
// rather than asserted to a string: the closing delimiter of the mapping
// arrives here too, and it matches neither name.
func recordEntry(probe *sniffProbe, key json.Token, value json.RawMessage) {
	switch key {
	case "openapi":
		probe.OpenAPI = jsonString(value)
	case "swagger":
		probe.Swagger = jsonString(value)
	}
}

// jsonString returns value as a string, or "" for any other shape. A version
// that is not a string does not declare a dialect.
func jsonString(value json.RawMessage) string {
	var out string
	if err := json.Unmarshal(value, &out); err != nil {
		return ""
	}
	return out
}

// wholeLines returns prefix up to and including its last newline, so a block
// document is cut between entries rather than inside one. A prefix with no
// newline in it is returned as it is; there is no better cut to make.
func wholeLines(prefix []byte) []byte {
	if i := bytes.LastIndexByte(prefix, '\n'); i >= 0 {
		return prefix[:i+1]
	}
	return prefix
}

// majorMinor returns the "major.minor" prefix of a dotted version string,
// e.g. "3.1.0" → "3.1". Strings with fewer than two dots — a bare major
// version, or a version already in major.minor form — are returned unchanged.
func majorMinor(version string) string {
	firstDot := -1
	for i := range len(version) {
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
