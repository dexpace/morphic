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

// Detect implements compilers.Compiler. It reports the dialect src declares,
// keyed by the major.minor prefix of the version string.
//
// It names swagger@2.0 as well, which this compiler does not serve: a Swagger
// document is recognizably an API spec, and reporting it as one lets the caller
// say the format is unsupported rather than that the file is unreadable. The
// path is not consulted — an OpenAPI document is what it declares itself to be,
// under any extension.
//
// Bytes the probe cannot read are declined silently unless they declare a
// version under one of the discriminating keys, in which case the reader's
// complaint is reported: a source that says `openapi: 3.1.0` and will not read
// is this compiler's own and broken, which nothing else is in a position to
// say. Bytes that declare no version are another format's, and a YAML parser's
// complaint about them describes only the parser that was wrong to be asked.
// That holds whether they parse or not: a key with prose beside it is what
// Markdown writes at column 0 (declaredVersions for a document that parses,
// declaresProbeKey for one that does not), and a mapping under it is another
// tool's configuration section. What tells either from a declaration is the one
// word after the colon.
//
// The parse a recognition carries is the one Compile lowers. Recognizing a
// source means reading what it declares, which means parsing it, and the
// compile that follows would otherwise parse the same bytes again. Every
// source within the byte budget is parsed whole, one this compiler goes on to
// refuse or another format's alike: the budget is what bounds that cost, not
// a second reader that answers for large sources by reading less of them.
//
// A source past the byte budget in opts is declined before any of it is read,
// with the refusal Compile would give it. The budget exists to bound reading
// the source, and detection is a read of it, so the one a caller set holds here
// as it does in the compile. It is reported rather than silent because it is
// the reason nothing took the source, whatever its format: saying no compiler
// recognized it would send the caller to look at the document rather than at
// the budget. A registry reads it only when no compiler takes the source, so it
// costs another format's compiler nothing.
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

// declaresProbeKey reports whether data declares a version under one of the
// discriminating keys, written where a document declares one. It is what
// separates a source of this compiler's own from one of another format that was
// never its business, and it is asked once: after the probe could not read the
// source, where "not YAML" alone says only what a Protobuf or Smithy source
// would also say. It reads the bytes once, linearly, and the bytes it reads are
// within the caller's budget, since nothing past it is read at all.
//
// Where a document declares its version, the two styles answer by different
// structure: column 0 in block style, the root mapping's own entries in flow
// style. Neither reading may be widened to "the name occurs somewhere followed
// by a colon", because other formats nest a key of that name, and reporting
// their bytes under this compiler's parse error is the one thing detection must
// never do.
//
// The version is what makes the claim, as it is for a source that parses
// (declaredVersions): prose beside the key is a README or a changelog, and a
// mapping under it is another tool's configuration section. Neither is an
// OpenAPI document that failed to read, and saying so would send the caller to
// fix a file that was never a spec (GitHub #497). The price is a spec broken on
// its own version line, `openapi: [3.1.0`, which declares no version to read and
// is reported as unrecognized rather than as unreadable. What cannot be told
// apart at all is a README that quotes a spec in a code block: its lines are a
// spec's, and it is claimed like one.
func declaresProbeKey(data []byte) bool {
	data = trimBOM(data)
	return declaresBlockKey(data, "openapi") || declaresBlockKey(data, "swagger") ||
		declaresFlowKey(data)
}

// declaresBlockKey reports whether data writes key bare at the start of a line
// with a version beside it. Column 0 is where a top-level key goes in block
// style and nowhere else: a key nested under another is indented past it, and a
// block scalar's content is indented past its own key.
//
// Only the bare spelling is read here, because the quoted one is how flow style
// writes every key and flow structure is what scopes it — declaresFlowKey has
// it. A block document that quotes its top-level key is therefore not seen, and
// is declined in silence rather than claimed; that is the direction to be wrong
// in, and the spelling is rare enough that widening column 0 to admit the shape
// JSON writes at every depth would cost far more than it buys.
//
// The line is read by the YAML parser rather than by hand, alone: the document
// around it did not parse, but the line that declares a version is usually
// whole, and the parser is what knows how a value is quoted and where a comment
// begins.
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

// declaresFlowKey reports whether data opens a flow mapping — the shape JSON
// writes — that names one of the discriminating keys among its own entries.
//
// Nesting depth is what makes the answer top-level, and it is the half a plain
// search for `"openapi":` gets wrong: a quoted name followed by a colon reads as
// a key wherever it sits, and other formats nest one. A source that opens no
// mapping at all — a JSON array, say — declares nothing here for the same
// reason: whatever it names, it does not name it as its own root key.
//
// It is a lexer, not a parser: it tracks quoted strings and nesting, and reads
// one value — the one after a root key it names. It has to answer on bytes that
// will not parse, which is the case it exists for — a document broken before
// the key that names it — so there is no tree to ask instead. A nested string is
// stepped over by flowString alone.
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

// sniff reads the discriminating keys out of data, and returns the zero probe
// and the parser's error for anything it cannot read. Whether that error is
// worth reporting is Detect's question, not this one's: here it is only the
// record of what happened.
//
// The document is decoded whole and exactly, which is the only way to tell one
// that declares nothing from one that will not parse, and the parse is the one
// the compile goes on to lower rather than a second reading beside it. A second
// reading is what detection used to have past 64 KiB — a byte scan standing in
// for the parse on large documents — and it cost what two readings of one
// document always cost: a table holding them to one answer, a written reason
// for every shape they differed on, and each change to document structure
// made twice (GitHub #486). What bounds the parse is the caller's byte budget,
// which Detect checks before this is reached.
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
//
// seen is the bound depth is not. Depth limits how far a chain is followed and
// says nothing about how wide it is: a mapping may merge one anchor k times,
// and each of those merges walked that anchor's own k merges, so the cost was
// the product rather than the sum — 115ms for a 4,000-key document against
// 13ms for a 1,000-key one, four times the input for nine times the work, and
// rising (GitHub #487). Entering each node once collapses that to the node
// count the caller has already parsed and paid for.
//
// The answer does not move. fillFrom keeps the first contribution, and a node
// entered twice yields the same probe both times, so the visit that is skipped
// could only re-supply what the first one already gave.
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
