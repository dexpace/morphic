package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// maxSniffBytes bounds the prefix Detect parses on its fast path. Detection
// reads two top-level keys, and 64 KiB reaches them in any document a person
// wrote, so the cost of asking stays flat while spec size does not: a full parse
// of a 10 MB document costs hundreds of milliseconds before the compiler's own
// parse begins. It is a bound on the fast path, not on detection — a document
// whose prefix declares neither key while its bytes name one is read whole, per
// sniffWhole.
const maxSniffBytes = 64 << 10

// maxSniffEntries bounds the top-level entries read from a flow-style mapping.
// A document declares few top-level keys however large it grows, so a mapping
// that runs past this without naming either key is not one this compiler will
// take. The bound is on entries, not bytes: one of them may be megabytes long,
// which is the whole reason the byte cap alone does not answer the question.
const maxSniffEntries = 512

// maxMergeDepth bounds how far a root mapping's merge keys are followed. A `<<`
// value may be an alias to a mapping that merges another, and an anchor may name
// a mapping that reaches itself, so the chain is not bounded by the document.
// Detection reads two keys off the root, which a document that merges at all
// reaches in one step; eight leaves room for a written chain and none for a
// crafted one.
const maxMergeDepth = 8

// mergeTag is the tag YAML resolves `<<` to. The tag is read rather than the
// key's text, because a mapping may legitimately hold a key spelled "<<" that
// was quoted into a plain string and merges nothing.
const mergeTag = "!!merge"

// sniffProbe holds the two discriminating top-level keys. Which one is present
// is the whole of the format question: an OpenAPI 3.x document declares
// `openapi`, a Swagger 2.0 document declares `swagger`.
//
// It carries no struct tags: nothing decodes into it. Both readers — the flow
// one over a JSON token stream and the block one over a parsed tree — name the
// two keys themselves, in recordEntry and fieldFor.
type sniffProbe struct {
	OpenAPI string
	Swagger string
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
// Bytes the probe cannot read are declined silently unless they declare one of
// the discriminating keys, in which case the reader's complaint is reported: a
// source that says `openapi:` and will not read is this compiler's own and
// broken, which nothing else is in a position to say. That covers a document
// that does not parse and one that parses with a version key of the wrong shape
// alike — both are unreadable here, and neither is another format's. Bytes that
// declare neither key are, and a YAML parser's complaint about them describes
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
			"source declares an OpenAPI or Swagger key and cannot be read: %s", diag.OneLine(err))}, false
	default:
		return compilers.SourceFormat{}, nil, false
	}
}

// declaresProbeKey reports whether data names one of the discriminating keys as
// a top-level key. It is what separates a source of this compiler's own from one
// of another format that was never its business, and it is asked twice: before
// sniff parses a large document whole, and after a parse failed, where "not
// YAML" alone says only what a Protobuf or Smithy source would also say.
//
// The whole of data is read. A byte scan costs a fraction of the parse it stands
// in front of, and the key it looks for is exactly the one that can sit
// megabytes into a document — bounding this to the prefix would blind it in
// precisely the case it exists to catch.
//
// Top-level is the whole of the claim, and the two styles answer it by different
// structure: column 0 in block style, the root mapping's own entries in flow
// style. Neither reading may be widened to "the name occurs somewhere followed
// by a colon", because other formats nest a key of that name, and reporting
// their bytes under this compiler's parse error is the one thing detection must
// never do.
func declaresProbeKey(data []byte) bool {
	return declaresBlockKey(data, "openapi") || declaresBlockKey(data, "swagger") ||
		declaresFlowKey(data)
}

// declaresBlockKey reports whether data writes key bare at the start of a line,
// which in block style is where a top-level key goes and nowhere else: a key
// nested under another is indented past column 0, and a block scalar's content
// is indented past its own key.
//
// Only the bare spelling is read here, because the quoted one is how flow style
// writes every key and flow structure is what scopes it — declaresFlowKey has
// it. A block document that quotes its top-level key is therefore not seen, and
// is declined in silence rather than claimed; that is the direction to be wrong
// in, and the spelling is rare enough that widening column 0 to admit the shape
// JSON writes at every depth would cost far more than it buys.
//
// The colon that makes it a key is required. Without it, a document of another
// format that merely mentions the word — in a comment, or as a value — would be
// claimed as this compiler's and reported under its parse error.
func declaresBlockKey(data []byte, key string) bool {
	name := []byte(key + ":")
	return bytes.HasPrefix(data, name) || bytes.Contains(data, append([]byte("\n"), name...))
}

// declaresFlowKey reports whether data opens a flow mapping — the shape JSON
// writes — that names one of the discriminating keys among its own entries.
//
// Nesting depth is what makes the answer top-level, and it is the half a plain
// search for `"openapi":` gets wrong: a quoted name followed by a colon reads as
// a key wherever it sits, and other formats nest one. A source that opens no
// mapping at all — a JSON array, say — declares nothing here for the same
// reason: whatever it names, it does not name it as its own root key.
//
// The scan is a lexer, not a parser: it tracks quoted strings and nesting and
// reads nothing else. It has to answer on bytes that will not parse, which is
// the case it exists for — a document broken before the key that names it — so
// there is no tree to ask instead.
func declaresFlowKey(data []byte) bool {
	i := skipSpace(data, 0)
	if i == len(data) || data[i] != '{' {
		return false
	}

	for depth := 0; i < len(data); {
		switch data[i] {
		case '"':
			name, next := flowString(data, i)
			if depth == 1 && isProbeName(name) && startsWithColon(data, next) {
				return true
			}
			i = next
		case '{', '[':
			depth++
			i++
		case '}', ']':
			depth--
			i++
		default:
			i++
		}
	}
	return false
}

// flowString returns the bytes between the quotes of the string data[i] opens,
// and the index just past its closing quote. An unterminated string runs to the
// end of data: there is nothing past it left to read.
func flowString(data []byte, i int) ([]byte, int) {
	for j := i + 1; j < len(data); j++ {
		switch data[j] {
		case '\\':
			j++
		case '"':
			return data[i+1 : j], j + 1
		}
	}
	return nil, len(data)
}

// isProbeName reports whether name is one of the discriminating keys.
func isProbeName(name []byte) bool {
	return string(name) == "openapi" || string(name) == "swagger"
}

// skipSpace returns the index of the first byte at or after i that is not
// whitespace, or len(data) if there is none.
func skipSpace(data []byte, i int) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
		i++
	}
	return i
}

// startsWithColon reports whether the first non-whitespace byte at or after i is
// the colon that makes the name before it a key.
func startsWithColon(data []byte, i int) bool {
	i = skipSpace(data, i)
	return i < len(data) && data[i] == ':'
}

// sniff reads the discriminating keys out of data, and returns the zero probe
// and the parser's error for anything it cannot read. Whether that error is
// worth reporting is Detect's question, not this one's: here it is only the
// record of what happened.
//
// A document within the cap is decoded whole and exactly. A larger one is read
// from its prefix first, and only from all of itself when that prefix answered
// nothing and the bytes past it name a key this compiler serves.
func sniff(data []byte) (sniffProbe, error) {
	if len(data) <= maxSniffBytes {
		return decodeYAML(data)
	}

	probe, err := sniffPrefix(data[:maxSniffBytes])
	if probe.OpenAPI != "" || probe.Swagger != "" {
		return probe, nil
	}
	if declaresProbeKey(data) {
		return sniffWhole(data)
	}
	return probe, err
}

// sniffPrefix reads the probe keys from the first maxSniffBytes of a document
// too large to decode whole. The prefix cannot simply be cut: flow style — JSON
// is the common case — is one token stream with no line structure, so its
// entries are streamed instead, and block style is cut at its last complete
// line.
func sniffPrefix(prefix []byte) (sniffProbe, error) {
	// The cut breaks the token stream by construction, so the flow decoder's
	// error here describes the cut and not the document. What it managed to read
	// before the cut is the whole of what a prefix has to say.
	probe, flow, _ := decodeFlowEntries(prefix)
	if flow {
		return probe, nil
	}
	return decodeYAML(wholeLines(prefix))
}

// sniffWhole reads the probe keys from a whole document past the cap, for the
// one case that earns the parse: the prefix declared neither key, yet the bytes
// name one further in. Mapping key order carries no meaning, so a document that
// writes a multi-megabyte `components` before its `openapi` is as valid as one
// that writes them the other way round, and declining it would reject a valid
// document over nothing.
//
// Nothing another format wrote reaches here — declaresProbeKey guards the call —
// so the cost is paid only for bytes this compiler is about to parse in full
// anyway, and the answer for everyone else is still the fast path's silence.
//
// Unlike sniffPrefix this keeps the flow decoder's error, and returns the zero
// probe with it exactly as decodeYAML does. There is no cut here to explain a
// broken token stream away: a whole document that stops mid-stream is a document
// this compiler cannot read, and saying so is the answer the key it declares has
// earned. Dropping the error instead reports the source as another format's,
// which is what the document past the cap was found not to be.
func sniffWhole(data []byte) (sniffProbe, error) {
	probe, flow, err := decodeFlowEntries(data)
	if !flow {
		return decodeYAML(data)
	}
	if err != nil {
		return sniffProbe{}, err
	}
	return probe, nil
}

// decodeYAML reads the probe keys from a complete YAML (or JSON, its subset)
// document.
//
// The document is parsed and its root mapping read; it is never decoded into
// sniffProbe. That is the whole of the fix for a 32 KB source producing a 1.2 GB
// diagnostic: yaml.v3 compares every pair of a mapping's keys before it reads
// any of them, so a mapping repeating one key n times raises n(n-1)/2 errors —
// 21 million of them for the 6,553-line case — and then abandons the mapping, so
// the probe came back empty as well as expensive. Reading the two keys off the
// parsed tree is linear, and answers for a document whose keys repeat exactly as
// for one whose keys do not. The parser this compiler goes on to use reports
// those repeats itself, once each and sited, which is where a reader wants them.
func decodeYAML(data []byte) (sniffProbe, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return sniffProbe{}, err
	}

	root := documentRoot(&doc)
	switch {
	case root == nil:
		// A stream that carried no document declares no key, which is a decline
		// and not a failure: empty bytes are no more this compiler's than
		// anybody else's.
		return sniffProbe{}, nil
	case root.Kind != yaml.MappingNode:
		return sniffProbe{}, fmt.Errorf("document root is %s, not a mapping", root.ShortTag())
	default:
		return probeFromMapping(root, maxMergeDepth)
	}
}

// documentRoot returns the content node of a decoded stream's first document, or
// nil for a stream that carried none. Decoding into a yaml.Node yields the
// document node itself, and only the first: a multi-document stream is read to
// its first document here exactly as the compiler's own load reads it.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil
	}
	return doc.Content[0]
}

// probeFromMapping reads the probe keys off a root mapping, following its merge
// keys for a key the mapping does not write itself.
//
// A key written directly wins over one merged in, which is the precedence YAML
// gives a merge. A key written twice takes its last spelling, which is what the
// parser this compiler goes on to use takes: detection names the dialect that
// routes the source, load records the one it read, and a document must not get
// two answers. Neither rule could be had before, since the decoder this replaces
// refused any mapping that repeated a key at all.
//
// depth is the merge chain still allowed. It is the bound on this recursion,
// checked before every descent, and the recursion is otherwise over a parsed
// tree of finite size.
func probeFromMapping(root *yaml.Node, depth int) (sniffProbe, error) {
	probe, merges, err := probeFromEntries(root)
	if err != nil || depth <= 0 {
		return probe, err
	}

	for _, merge := range merges {
		merged, err := probeFromMerge(merge, depth-1)
		if err != nil {
			return sniffProbe{}, err
		}
		probe.fillFrom(merged)
	}
	return probe, nil
}

// probeFromEntries reads a mapping's own entries, and returns the values of its
// merge keys separately for the caller to follow. A mapping may write more than
// one `<<`, and their order is the order they are answered in.
func probeFromEntries(root *yaml.Node) (sniffProbe, []*yaml.Node, error) {
	var probe sniffProbe
	var merges []*yaml.Node

	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Tag == mergeTag {
			merges = append(merges, value)
			continue
		}
		field := probe.fieldFor(key)
		if field == nil {
			continue
		}
		version, err := probeVersion(value)
		if err != nil {
			return sniffProbe{}, nil, err
		}
		*field = version
	}
	return probe, merges, nil
}

// probeFromMerge reads the probe keys out of one `<<` value, which YAML admits
// as an alias to a mapping, a mapping written out, or a sequence of either.
// Anything else merges nothing, which is the source's problem to be reported by
// the parser that reads it and not a reason for detection to refuse.
func probeFromMerge(merge *yaml.Node, depth int) (sniffProbe, error) {
	if depth <= 0 {
		return sniffProbe{}, nil
	}

	switch merge.Kind {
	case yaml.AliasNode:
		if merge.Alias == nil {
			return sniffProbe{}, nil
		}
		return probeFromMerge(merge.Alias, depth-1)
	case yaml.MappingNode:
		return probeFromMapping(merge, depth-1)
	case yaml.SequenceNode:
		// A sequence merges each of its entries, earlier ones winning over later,
		// which is the precedence YAML gives them.
		var probe sniffProbe
		for _, item := range merge.Content {
			merged, err := probeFromMerge(item, depth-1)
			if err != nil {
				return sniffProbe{}, err
			}
			probe.fillFrom(merged)
		}
		return probe, nil
	default:
		return sniffProbe{}, nil
	}
}

// probeVersion returns the version string a probe key's value declares, and an
// error for a value that is not a scalar at all.
//
// The scalar's text is taken as written rather than decoded, because the two
// disagree only for tags no version carries — a version key is not !!binary —
// and because decoding is what must not happen here: a mapping handed back to
// the decoder is the quadratic path decodeYAML exists to avoid, and a probe
// key's own value is the last place one could still be handed to it.
func probeVersion(value *yaml.Node) (string, error) {
	if value.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("version key is %s, not a scalar", value.ShortTag())
	}
	return value.Value, nil
}

// fieldFor returns the probe field that key names, or nil for a key that names
// neither. Only a scalar names one: a mapping or sequence used as a key is legal
// YAML and is not one of the two spellings this looks for.
func (p *sniffProbe) fieldFor(key *yaml.Node) *string {
	if key.Kind != yaml.ScalarNode {
		return nil
	}
	switch key.Value {
	case "openapi":
		return &p.OpenAPI
	case "swagger":
		return &p.Swagger
	default:
		return nil
	}
}

// fillFrom takes from other only what p does not already declare, which is what
// makes a merged key lose to a written one.
func (p *sniffProbe) fillFrom(other sniffProbe) {
	if p.OpenAPI == "" {
		p.OpenAPI = other.OpenAPI
	}
	if p.Swagger == "" {
		p.Swagger = other.Swagger
	}
}

// opensFlowMapping reports whether the stream's first token opens a mapping.
// A stream that does not is not flow style, which is an answer rather than a
// failure: the decoder's complaint there says only that these bytes are not
// JSON, and block YAML is not JSON either.
func opensFlowMapping(dec *json.Decoder) bool {
	tok, err := dec.Token()
	return err == nil && tok == json.Delim('{')
}

// decodeFlowEntries reads the top-level entries of data, which may be a whole
// document or a prefix of one. It reports whether data opened a flow mapping,
// and the error that ended the walk early. The JSON decoder is used because it
// streams: a prefix cut mid-document still yields every entry it completed,
// where decoding those same bytes whole reports only that they end early.
//
// A nil error means the walk ended on the mapping's own closing delimiter or on
// the entry cap — the two ways of stopping that say nothing about the bytes.
// Whether a non-nil one describes the document or only the cut that produced
// data is the caller's question, since only the caller knows which it passed.
func decodeFlowEntries(data []byte) (sniffProbe, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if !opensFlowMapping(dec) {
		return sniffProbe{}, false, nil
	}

	var probe sniffProbe
	for range maxSniffEntries {
		key, err := dec.Token()
		if err != nil {
			return probe, true, err
		}
		if key == json.Delim('}') {
			return probe, true, nil
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return probe, true, err
		}
		recordEntry(&probe, key, value)
	}
	return probe, true, nil
}

// recordEntry stores value under probe's field for key. key is compared as read
// rather than asserted to a string: a json.Token holds whichever kind the stream
// produced, and only the two names the switch spells are of any interest here.
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
