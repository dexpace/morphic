package openapi

import (
	"bytes"
	"encoding/json"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
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
//
// Bytes that do not parse are declined silently unless they declare one of the
// discriminating keys, in which case the parse error is reported: a source that
// says `openapi:` and will not parse is this compiler's own and broken, which
// nothing else is in a position to say. Bytes that declare neither key are
// another format's business, and a YAML parser's complaint about them describes
// only the parser that was wrong to be asked.
func (*Compiler) Detect(src compilers.Source) (compilers.SourceFormat, []ir.Diagnostic, bool) {
	probe, err := sniff(src.Data)
	switch {
	case probe.OpenAPI != "":
		return compilers.SourceFormat{Name: "openapi", Version: majorMinor(probe.OpenAPI)}, nil, true
	case probe.Swagger != "":
		return compilers.SourceFormat{Name: "swagger", Version: majorMinor(probe.Swagger)}, nil, true
	case err != nil && declaresProbeKey(src.Data):
		// NoSource, not source 0: detection runs before any document exists, so
		// there is no source table for a provenance to index into.
		return compilers.SourceFormat{}, []ir.Diagnostic{diag.Newf(
			ir.SeverityError, diag.UndecodableSource, ir.Provenance{Source: ir.NoSource},
			"source declares an OpenAPI or Swagger key and does not parse: %v", err)}, false
	default:
		return compilers.SourceFormat{}, nil, false
	}
}

// declaresProbeKey reports whether data names one of the discriminating keys as
// a top-level key. It is what separates a source of this compiler's own that
// will not parse from one of another format that was never its business: a
// parse failure alone says only "not YAML", which a Protobuf or Smithy source
// is not either.
//
// Only the bounded prefix is read, for the reason sniff bounds its own reads.
func declaresProbeKey(data []byte) bool {
	if len(data) > maxSniffBytes {
		data = data[:maxSniffBytes]
	}
	return declaresKey(data, "openapi") || declaresKey(data, "swagger")
}

// declaresKey reports whether data names key at the top level, in either style:
// unquoted at the start of a line for block style, or quoted for flow style,
// which is how JSON writes every key.
//
// Both spellings require the colon that makes it a key. Without it, a document
// of another format that merely mentions the word — in a comment, or as a value
// — would be claimed as this compiler's and reported under its parse error.
func declaresKey(data []byte, key string) bool {
	block := []byte(key + ":")
	if bytes.HasPrefix(data, block) || bytes.Contains(data, []byte("\n"+key+":")) {
		return true
	}
	return followedByColon(data, []byte(`"`+key+`"`))
}

// followedByColon reports whether name occurs in data followed by a colon,
// ignoring the whitespace a flow mapping may put between them.
func followedByColon(data, name []byte) bool {
	for i := 0; ; {
		j := bytes.Index(data[i:], name)
		if j < 0 {
			return false
		}
		rest := bytes.TrimLeft(data[i+j+len(name):], " \t\r\n")
		if len(rest) > 0 && rest[0] == ':' {
			return true
		}
		i += j + len(name)
	}
}

// sniff reads the discriminating keys out of at most maxSniffBytes bytes, and
// returns the zero probe and the parser's error for anything it cannot read.
// Whether that error is worth reporting is Detect's question, not this one's:
// here it is only the record of what happened.
//
// A document within the cap is decoded whole and exactly. A larger one is
// decoded from a prefix, which cannot simply be cut: flow style — JSON is the
// common case — is one token stream with no line structure, so its entries are
// streamed instead, and block style is cut at its last complete line.
func sniff(data []byte) (sniffProbe, error) {
	if len(data) <= maxSniffBytes {
		return decodeYAML(data)
	}
	prefix := data[:maxSniffBytes]
	if probe, ok := decodeFlowPrefix(prefix); ok {
		return probe, nil
	}
	return decodeYAML(wholeLines(prefix))
}

// decodeYAML reads the probe keys from a complete YAML (or JSON, its subset)
// document.
func decodeYAML(data []byte) (sniffProbe, error) {
	var probe sniffProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return sniffProbe{}, err
	}
	return probe, nil
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
