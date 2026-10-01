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
// It carries no struct tags: nothing decodes into it. The parse over a tree
// names the two keys itself, in fieldFor.
type sniffProbe struct {
	OpenAPI string
	Swagger string
}

// Detect implements compilers.Compiler. It reports the dialect src declares by
// its version's major.minor prefix, naming swagger@2.0 too, which it does not
// serve, so the caller can say unsupported rather than unreadable. The path is
// not consulted.
//
// Recognition parses all of src, whatever its format, and hands the parse to
// Compile in Recognition.Parsed. The byte budget bounds that cost: a source
// past it is declined unread with Compile's own refusal.
//
// Unreadable bytes are declined silently unless they declare a version under a
// discriminating key (declaresProbeKey); then the parser's complaint is
// reported, since that source is this compiler's own and broken.
func (*Compiler) Detect(src compilers.Source, opts compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	// NoSource, not source 0: detection runs before any document exists, so
	// there is no source table for a provenance to index into.
	noSource := ir.Provenance{Source: ir.NoSource}
	if d, over := load.OverByteBudget(noSource, src.Data, detectionBudget(opts)); over {
		return compilers.Recognition{}, []ir.Diagnostic{d}, false
	}

	probe, parsed, err := sniff(src.Data)
	switch {
	case probe.OpenAPI != "":
		return recognized("openapi", probe.OpenAPI, parsed), nil, true
	case probe.Swagger != "":
		return recognized("swagger", probe.Swagger, parsed), nil, true
	case err != nil && declaresProbeKey(src.Data):
		return compilers.Recognition{}, []ir.Diagnostic{diag.Newf(
			ir.SeverityError, diag.UndecodableSource, noSource,
			"source declares an OpenAPI or Swagger version and cannot be read: %s", diag.OneLine(err))}, false
	default:
		return compilers.Recognition{}, nil, false
	}
}

// detectionBudget is the byte budget detection is held to: the one opts would
// compile with. Options of another compiler's type are not this one's to read,
// so they mean the defaults here — Compile is what reports them, and only if
// the source turns out to be this compiler's.
func detectionBudget(opts compilers.Options) int {
	o, err := optionsFrom(opts)
	if err != nil {
		o = Options{}.withDefaults()
	}
	return bounded(o.Limits.MaxSourceBytes)
}

// recognized builds the recognition for a source that declared version under
// name, carrying the parse that read it.
//
// parsed is never nil here. A probe names a key only when decodeYAML read it
// off a parsed root, so a recognition always has a parse to carry — which
// matters, since a nil *load.Parsed put into the interface would make one that
// is not nil, and a consumer's type assertion would accept it and read through.
func recognized(name, version string, parsed *load.Parsed) compilers.Recognition {
	return compilers.Recognition{
		Format: compilers.SourceFormat{Name: name, Version: majorMinor(version)},
		Parsed: parsed,
	}
}

// declaresProbeKey reports whether data declares a version under a
// discriminating key. Detect asks only once the probe cannot read the source,
// where "not YAML" alone fits a Protobuf or Smithy source too, and only within
// its byte budget.
//
// Neither reading may widen to "the name occurs before a colon": other formats
// nest such a key and must not get this compiler's parse error. The version
// makes the claim (GitHub #497), so a spec broken on its version line,
// `openapi: [3.1.0`, is unrecognized rather than unreadable, and a README
// quoting a spec in a code block is claimed like one.
func declaresProbeKey(data []byte) bool {
	data = trimBOM(data)
	return declaresBlockKey(data, "openapi") || declaresBlockKey(data, "swagger") ||
		declaresFlowKey(data)
}

// declaresBlockKey reports whether data writes key bare at the start of a line
// with a version beside it. Column 0 is where block style puts a top-level key;
// nested keys and block scalar content are indented.
//
// Only the bare spelling is read: a quoted key is flow style's, scoped by
// declaresFlowKey. A block document that quotes its top-level key is declined
// in silence, the safe direction to be wrong, since admitting quoted keys at
// column 0 would take in what JSON writes at every depth. The line is parsed
// alone, since the document around it did not parse.
func declaresBlockKey(data []byte, key string) bool {
	prefix := []byte(key + ":")
	read := 0
	for i := 0; i < len(data) && read < maxVersionLines; {
		line, next := nextLine(data, i)
		if bytes.HasPrefix(line, prefix) {
			if lineDeclaresVersion(line, key) {
				return true
			}
			read++
		}
		i = next
	}
	return false
}

// maxVersionLines bounds how many lines declaresBlockKey parses. A document
// declares its version once and a stream a few times over, so the bound is room
// to spare for anything written; without it, a crafted file of nothing but such
// lines, broken at the end, cost nearly three times the parse that failed on
// it, one small parse per line.
const maxVersionLines = 8

// lineDeclaresVersion reports whether line, parsed on its own, is a mapping of
// key to a scalar that reads as a version. An alias there names a node on
// another line, which the line alone cannot resolve, so it declares nothing.
func lineDeclaresVersion(line []byte, key string) bool {
	var doc yaml.Node
	if yaml.Unmarshal(line, &doc) != nil {
		return false
	}
	root := documentRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode || len(root.Content) != 2 || root.Content[0].Value != key {
		return false
	}
	version, err := probeVersion(root.Content[1])
	return err == nil && isVersion(version)
}

// declaresFlowKey reports whether data opens a flow mapping, the shape JSON
// writes, that names one of the discriminating keys among its own entries.
//
// Nesting depth is what makes the answer top-level: a plain search for
// `"openapi":` matches wherever it sits, and other formats nest one. A source
// that opens no mapping, a JSON array say, declares nothing.
//
// It is a lexer, not a parser, because it exists for bytes that will not parse:
// there is no tree to ask.
func declaresFlowKey(data []byte) bool {
	i := skipNodeProperties(data, skipSpaceAndComments(data, 0))
	if i == len(data) || data[i] != '{' {
		return false
	}

	for depth := 0; i < len(data); {
		switch data[i] {
		case '"':
			name, next := flowString(data, i)
			if depth == 1 && isProbeName(name) && versionAfterColon(data, next) {
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

// skipSpaceAndComments returns the index of the first byte at or after i that
// begins content: past whitespace, and past any line a `#` opens. A flow
// document may follow a comment line, and yaml.v3 reads it there.
func skipSpaceAndComments(data []byte, i int) int {
	for i = skipSpace(data, i); i < len(data) && data[i] == '#'; i = skipSpace(data, i) {
		_, i = nextLine(data, i)
	}
	return i
}

// maxNodeProperties is how many node properties a node may carry: one anchor
// and one tag, in either order. It bounds skipNodeProperties.
const maxNodeProperties = 2

// skipNodeProperties returns the index just past any node properties at i — an
// `&anchor` or a `!tag`, each a word ended by whitespace or a flow indicator,
// neither of which a name may contain — and the whitespace after them. YAML
// writes them in front of the node they annotate, so a document's root flow
// mapping may sit behind one or two of them: `&x {`, `!!map &x {`. A guard
// testing the first byte for the opener would see the property instead, and
// decline a broken document that is this compiler's own.
func skipNodeProperties(data []byte, i int) int {
	for range maxNodeProperties {
		if i == len(data) || (data[i] != '&' && data[i] != '!') {
			return i
		}
		for i < len(data) && !endsWord(data[i]) {
			i++
		}
		i = skipSpace(data, i)
	}
	return i
}

// nextLine returns the line beginning at i, without its terminator, and the
// index of the line after it.
func nextLine(data []byte, i int) (line []byte, next int) {
	line = data[i:]
	if j := bytes.IndexByte(line, '\n'); j >= 0 {
		return line[:j], i + j + 1
	}
	return line, len(data)
}

// endsWord reports whether b ends a bare word in flow context: whitespace, or a
// flow indicator, none of which an anchor, a tag or a bare version contains.
func endsWord(b byte) bool {
	switch b {
	case ',', '}', ']', '{', '[', ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

// versionAfterColon reports whether a colon follows i, making the name before it
// a key, and a version follows the colon: a quoted string or a bare word that
// reads as one. A collection there opens with a flow indicator, so it reads as
// an empty word and declares nothing.
func versionAfterColon(data []byte, i int) bool {
	i = skipSpace(data, i)
	if i == len(data) || data[i] != ':' {
		return false
	}
	i = skipSpace(data, i+1)
	if i < len(data) && data[i] == '"' {
		value, _ := flowString(data, i)
		return isVersion(string(value))
	}
	end := i
	for end < len(data) && !endsWord(data[end]) {
		end++
	}
	return isVersion(string(data[i:end]))
}

// sniff reads the discriminating keys out of data. For anything it cannot read
// it returns the zero probe and the parser's error; whether that error is
// worth reporting is Detect's question.
//
// The document is decoded whole and exactly, the only way to tell one that
// declares nothing from one that will not parse. The parse is the one the
// compile lowers, not a second reading beside it that would have to be kept in
// agreement with it (GitHub #486). The caller's byte budget, which Detect
// checks first, bounds it.
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

// isVersion reports whether value reads as a version rather than prose: one
// word, beginning with a digit. That admits every shape majorMinor is written
// for, and suffixed ones such as "3.1.0-rc1" on purpose: a document writing one
// is this compiler's own and wrong, and the precise complaint is load's to
// make, where declining here would give the engine's generic "unrecognized
// format". Prose has a space in it, and that is what the guard keeps out.
func isVersion(value string) bool {
	if value == "" || value[0] < '0' || value[0] > '9' {
		return false
	}
	return !strings.ContainsAny(value, " \t")
}

// bomUTF8 is the UTF-8 byte-order mark. YAML admits one at the start of a
// stream and JSON is its subset, so a spec written by an editor that emits one
// is a spec like any other: yaml.v3 reads straight through it and so does the
// loader. declaresProbeKey reads bytes rather than a parse and has to skip it
// itself, or a spec with one would be declined in silence when broken where a
// spec without one is reported.
const bomUTF8 = "\xef\xbb\xbf"

// trimBOM returns data without a leading byte-order mark. Comparing through a
// string conversion compiles to a comparison rather than a copy.
func trimBOM(data []byte) []byte {
	if len(data) >= len(bomUTF8) && string(data[:len(bomUTF8)]) == bomUTF8 {
		return data[len(bomUTF8):]
	}
	return data
}

// decodeYAML reads the probe keys from a complete YAML (or JSON, its subset)
// document.
//
// The document is parsed and its root mapping read, never decoded into
// sniffProbe: yaml.v3 compares every pair of a mapping's keys before reading
// any, so a mapping repeating one key n times raises n(n-1)/2 errors and then
// abandons the mapping, leaving the probe empty as well as expensive. Reading
// the keys off the tree is linear and answers the same for repeated keys,
// which the compile's own parse reports once each, sited.
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
		probe, err := probeFromMapping(root, maxMergeDepth, map[*yaml.Node]bool{})
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
// A key written directly wins over one merged in, per YAML. A key written twice
// takes its last spelling, as the compile's parser does, so detection and load
// never disagree.
//
// depth bounds the recursion. seen bounds the width depth does not: without it,
// one anchor merged k times walks its own k merges each time, a product rather
// than a sum (GitHub #487). Skipping a node already entered changes no answer:
// fillFrom keeps the first contribution.
func probeFromMapping(root *yaml.Node, depth int, seen map[*yaml.Node]bool) (sniffProbe, error) {
	probe, merges, err := probeFromEntries(root)
	if err != nil || depth <= 0 {
		return probe, err
	}

	for _, merge := range merges {
		merged, err := probeFromMerge(merge, depth-1, seen)
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
func probeFromMerge(merge *yaml.Node, depth int, seen map[*yaml.Node]bool) (sniffProbe, error) {
	if depth <= 0 || seen[merge] {
		return sniffProbe{}, nil
	}
	seen[merge] = true

	switch merge.Kind {
	case yaml.AliasNode:
		if merge.Alias == nil {
			return sniffProbe{}, nil
		}
		return probeFromMerge(merge.Alias, depth-1, seen)
	case yaml.MappingNode:
		return probeFromMapping(merge, depth-1, seen)
	case yaml.SequenceNode:
		// A sequence merges each of its entries, earlier ones winning over later,
		// which is the precedence YAML gives them.
		var probe sniffProbe
		for _, item := range merge.Content {
			merged, err := probeFromMerge(item, depth-1, seen)
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
