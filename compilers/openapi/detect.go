package openapi

import (
	"bytes"
	"fmt"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/ir"
)

// maxDetectBytes is the largest source detection will read. It is the default
// source budget, because a document past that is one the compiler refuses to
// lower anyway, and reading it to name a format nothing will compile is work
// spent on an answer nobody uses.
//
// It is a ceiling rather than a budget: nothing raises it. A caller who lifts
// Limits.MaxSourceBytes past it lifts what the compile will lower, not what
// detection will read, so a source in that range is declined here — with a
// diagnostic when it is recognizably this compiler's own, since "no compiler
// took it" would be the wrong thing to say about a 100 MB OpenAPI document.
//
// Detection used to read past this by scanning bytes for the two keys instead
// of parsing, which meant two readers of one document and a table holding them
// to one answer. Parsing is the only reading now, so the bound is on what is
// read at all.
const maxDetectBytes = DefaultMaxSourceBytes

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
// It carries no struct tags: nothing decodes into it. Both readers — the scan
// over bytes and the parse over a tree — name the two keys themselves, in
// setVersion and fieldFor.
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
// alike — a mapping or a sequence where a version goes — since both are
// unreadable here, and neither is another format's. Bytes that declare neither
// key are, and a YAML parser's complaint about them describes only the parser
// that was wrong to be asked. So is a key with prose beside it rather than a
// version (declaredVersions): Markdown writes `openapi:` at column 0 too, and
// what tells its line from a declaration is the one word after the colon.
//
// The parse a recognition carries is the one Compile lowers. Recognizing a
// source means reading what it declares, which means parsing it, and the
// compile that follows would otherwise parse the same bytes again. A source
// past maxDetectBytes is read no further than its length, so it declares
// nothing here and carries nothing forward.
func (*Compiler) Detect(src compilers.Source) (compilers.Recognition, []ir.Diagnostic, bool) {
	if len(src.Data) > maxDetectBytes {
		return compilers.Recognition{}, unreadable(src.Data,
			fmt.Errorf("source is %d bytes, past the %d-byte ceiling detection reads; "+
				"raising the source budget lifts what a compile lowers, not what detection reads",
				len(src.Data), maxDetectBytes)), false
	}
	probe, parsed, err := sniff(src.Data)
	switch {
	case probe.OpenAPI != "":
		return recognized("openapi", probe.OpenAPI, parsed), nil, true
	case probe.Swagger != "":
		return recognized("swagger", probe.Swagger, parsed), nil, true
	case err != nil:
		return compilers.Recognition{}, unreadable(src.Data, err), false
	default:
		return compilers.Recognition{}, nil, false
	}
}

// unreadable reports a source this compiler could not read, and says nothing
// at all unless the source is recognizably its own.
//
// Bytes of another format are ordinary input, so declining them is silent; a
// document that writes `openapi:` and will not read is this compiler's and
// broken, which nothing else is in a position to say. The provenance is
// NoSource rather than source 0: detection runs before any document exists, so
// there is no source table for a provenance to index into.
func unreadable(data []byte, err error) []ir.Diagnostic {
	if !declaresProbeKey(data) {
		return nil
	}
	return []ir.Diagnostic{diag.Newf(
		ir.SeverityError, diag.UndecodableSource, ir.Provenance{Source: ir.NoSource},
		"source declares an OpenAPI or Swagger key and cannot be read: %s", diag.OneLine(err))}
}

// recognized builds the recognition for a source that declared version under
// name, carrying the parse that read it.
//
// A nil parse is left out rather than stored: a nil *load.Parsed put into an
// interface makes an interface that is not nil, which a consumer's type
// assertion accepts and then reads through. Past the sniff cap nothing is
// parsed, so this is the ordinary case and not an edge one.
func recognized(name, version string, parsed *load.Parsed) compilers.Recognition {
	rec := compilers.Recognition{Format: compilers.SourceFormat{Name: name, Version: majorMinor(version)}}
	if parsed != nil {
		rec.Parsed = parsed
	}
	return rec
}

// declaresProbeKey reports whether data names one of the discriminating keys as
// a top-level key. It is what separates a source of this compiler's own from one
// of another format that was never its business, and it is asked once: after a
// parse failed, where "not YAML" alone says only what a Protobuf or Smithy
// source would also say. A parse only runs at or below the cap — past it the
// scan answers and cannot fail — so the bytes reaching here are never large.
//
// Top-level is the whole of the claim, and the two styles answer it by different
// structure: column 0 in block style, the root mapping's own entries in flow
// style. Neither reading may be widened to "the name occurs somewhere followed
// by a colon", because other formats nest a key of that name, and reporting
// their bytes under this compiler's parse error is the one thing detection must
// never do.
func declaresProbeKey(data []byte) bool {
	data = trimBOM(data)
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
// claimed as this compiler's and reported under its parse error. What follows
// the colon is not: this guard is asked only after a reading has failed, so
// there is no version left to read, and nothing else will claim a file this
// compiler has already called broken. scanProbe names a format and routes the
// source, so its block reading requires the separated colon YAML does; the
// looseness here is deliberate, not inherited.
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
	return walkFlowRoot(data, func(name []byte, next int) (int, bool) {
		return next, isProbeName(name) && startsWithColon(data, next)
	})
}

// walkFlowRoot lexes the flow mapping data opens and calls visit at each quoted
// string that is one of the root mapping's own entries — a name at depth 1 —
// with the index just past its closing quote. visit returns where to resume
// and whether to stop; walkFlowRoot reports whether it was stopped. Data that
// opens no flow mapping is walked past nothing.
//
// The two flow readers share this walk so they cannot drift: one lexer, one
// notion of depth, and the visitor is the whole of what differs between
// "is the key there" and "what does it say". A name is handed over only at
// depth 1, which is also what keeps the walk cheap — a nested string is stepped
// over by flowString alone, and the value after a root name is read only when
// the visitor asks for it.
func walkFlowRoot(data []byte, visit func(name []byte, next int) (resume int, stop bool)) bool {
	i := skipNodeProperties(data, skipSpaceAndComments(data, 0))
	if i == len(data) || data[i] != '{' {
		return false
	}

	for depth := 0; i < len(data); {
		switch data[i] {
		case '"':
			name, next := flowString(data, i)
			if depth != 1 {
				i = next
				continue
			}
			resume, stop := visit(name, next)
			if stop {
				return true
			}
			i = resume
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

// skipSpaceAndComments returns the index of the first byte at or after i that
// begins content: past whitespace, and past any line a `#` opens. A flow
// document may follow a comment line, and yaml.v3 reads it there.
func skipSpaceAndComments(data []byte, i int) int {
	for i = skipSpace(data, i); i < len(data) && data[i] == '#'; i = skipSpace(data, i) {
		if nl := bytes.IndexByte(data[i:], '\n'); nl >= 0 {
			i += nl + 1
			continue
		}
		return len(data)
	}
	return i
}

// maxNodeProperties is how many node properties a node may carry: one anchor
// and one tag, in either order. It bounds skipNodeProperties.
const maxNodeProperties = 2

// skipNodeProperties returns the index just past any node properties at i — an
// `&anchor` or a `!tag`, each a word ended by whitespace or a flow indicator,
// neither of which a name may contain — and the whitespace after them. YAML writes them in front of the node they annotate, so what
// opens a construct may sit behind one or two of them: `a: &x {`, `!!map &x {`,
// `&x "…"`. A reader testing the first byte after the colon for an opener
// sees the property instead and opens nothing, which is the claiming direction
// for every line the construct then continues.
func skipNodeProperties(data []byte, i int) int {
	for range maxNodeProperties {
		if i == len(data) || (data[i] != '&' && data[i] != '!') {
			return i
		}
		for i < len(data) && !isFlowScalarEnd(data[i]) {
			i++
		}
		i = skipSpace(data, i)
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
// A document within the cap is decoded whole and exactly, which is the only way
// to tell one that declares nothing from one that will not parse. A larger one
// is scanned instead: the answer detection owes is which of two keys a document
// declares, and a scan reads that in one linear pass, where a parse builds a
// tree of everything between them before the compiler's size and node budgets
// have agreed to pay for one.
func sniff(data []byte) (sniffProbe, *load.Parsed, error) {
	probe, parsed, err := decodeYAML(data)
	return declaredVersions(probe), parsed, err
}

// declaredVersions drops any value that does not read as a version. A key alone
// does not declare a format: another format's document may write the word — at
// column 0 in Markdown prose, or as a field of its own — and what separates that
// from a declaration is the version beside it. Claiming it instead reports this
// compiler's complaint over a file that was never its own.
func declaredVersions(probe sniffProbe) sniffProbe {
	if !isVersion(probe.OpenAPI) {
		probe.OpenAPI = ""
	}
	if !isVersion(probe.Swagger) {
		probe.Swagger = ""
	}
	return probe
}

// isVersion reports whether value reads as a version rather than as prose: one
// word, beginning with a digit. That admits the three shapes majorMinor is
// written for — "3.1.0", "3.1", a bare "4" — and the ones load goes on to
// refuse by name, "3.1.0-rc1" or "3x", and nothing that a sentence is.
//
// The suffixes are admitted on purpose. A document writing one is this
// compiler's own and wrong, and the precise complaint — which version, and why
// it is not served — is load's and the validator's to make; declining here
// hands the same file to the engine's generic "unrecognized format" instead.
// What the guard exists to keep out is another format's prose beside the word,
// and prose has a space in it.
func isVersion(value string) bool {
	if value == "" || value[0] < '0' || value[0] > '9' {
		return false
	}
	return !strings.ContainsAny(value, " \t")
}

// bomUTF8 is the UTF-8 byte-order mark. YAML admits one at the start of a
// stream and JSON is its subset, so a spec written by an editor that emits one
// is a spec like any other: yaml.v3 reads straight through it and so does the
// loader. The byte scans have to skip it themselves, or the same document would
// name a format at the cap and none one byte past it.
const bomUTF8 = "\xef\xbb\xbf"

// trimBOM returns data without a leading byte-order mark. Comparing through a
// string conversion compiles to a comparison rather than a copy, so the scan
// stays allocation-free.
func trimBOM(data []byte) []byte {
	if len(data) >= len(bomUTF8) && string(data[:len(bomUTF8)]) == bomUTF8 {
		return data[len(bomUTF8):]
	}
	return data
}

// isFlowScalarEnd reports whether b ends a bare scalar in flow style: the
// separators and closers of flow syntax, and whitespace, which a version never
// contains. The openers are here too, so a value that begins one reads as an
// empty scalar and flowValue declines it.
func isFlowScalarEnd(b byte) bool {
	switch b {
	case ',', '}', ']', '{', '[', ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
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
func decodeYAML(data []byte) (sniffProbe, *load.Parsed, error) {
	parsed, err := load.Decode(data)
	if err != nil {
		return sniffProbe{}, nil, err
	}

	root := documentRoot(parsed.Root())
	switch {
	case root == nil:
		// A stream that carried no document declares no key, which is a decline
		// and not a failure: empty bytes are no more this compiler's than
		// anybody else's.
		return sniffProbe{}, nil, nil
	case root.Kind != yaml.MappingNode:
		return sniffProbe{}, nil, fmt.Errorf("document root is %s, not a mapping", root.ShortTag())
	default:
		probe, err := probeFromMapping(root, maxMergeDepth)
		return probe, parsed, err
	}
}

// documentRoot returns the content node of the document load handed back, or
// nil for a stream that carried none. That document is the first in the stream
// that holds content — the one the compile lowers, read by the same function,
// so a source routed here on what that document declares is the source lowered
// here (GitHub #481). The documents after it are reported as dropped by load
// (GitHub #387); detection routes the source and says nothing about its shape.
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
