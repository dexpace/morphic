package openapi

import (
	"bytes"
	"fmt"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// maxSniffBytes is the size at which detection stops parsing and scans instead.
// Detection reads two top-level keys, and 64 KiB reaches them in any document a
// person wrote, so the cost of asking stays flat while spec size does not: a
// full parse of a 10 MB document costs hundreds of milliseconds before the
// compiler's own size and node budgets have agreed to pay for one.
//
// Nothing is declined for being large. Past the cap the same two keys are read
// by scanProbe, in one linear pass that builds no tree.
const maxSniffBytes = 64 << 10

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
	i := skipSpaceAndComments(data, 0)
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
		_, i = nextLine(data, i)
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
func sniff(data []byte) (sniffProbe, error) {
	probe, err := readProbe(data)
	return declaredVersions(probe), err
}

// readProbe reads the probe keys by whichever means the document's size affords.
func readProbe(data []byte) (sniffProbe, error) {
	if len(data) <= maxSniffBytes {
		return decodeYAML(data)
	}
	return scanProbe(data), nil
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

// scanProbe reads the probe keys and their versions out of data without building
// a tree of it. Both styles are scanned, because which one a document is written
// in is not known until it has been read: block style writes a top-level key at
// column 0, flow style writes it among the root mapping's own entries.
//
// Both keys are read wherever they sit, so where a document declares one carries
// no meaning here — mapping keys being unordered, that is the whole property.
// Which of the two wins when a document declares both is Detect's question.
//
// The scan reads less than the parse it stands in for, and the cap decides which
// of them answers, so every shape they disagree on is a document that names one
// format below the cap and another above it. The shapes the scan declines by
// design — a root merge key, a quoted key in block style, an unquoted one in
// flow style, an anchor or a tag before the version — are declared in
// TestReadings_AgreeExceptWhereDeclared, each against its reason, rather than
// here: that table fails when a reason goes stale or a new divergence appears,
// and prose can do neither.
func scanProbe(data []byte) sniffProbe {
	var probe sniffProbe
	first := firstDocument(trimBOM(data))
	scanBlockProbe(first, &probe)
	scanFlowProbe(first, &probe)
	return probe
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

// firstDocument returns the content of data's first YAML document. A stream may
// carry several, opened by `---` and ended by `...` at column 0, and load reads
// only the first, so a key in a later one names a format for bytes the compile
// never parses. A marker ends the document even in the middle of a scalar,
// which is what makes a line scan the right reading for one.
//
// The opening marker is left behind rather than returned, so what comes back
// begins where the document's own bytes do: a flow document written after a
// `---`, or on its line, is then the same bytes to the flow scan as one written
// without a marker at all.
func firstDocument(data []byte) []byte {
	start, opened := 0, false
	for i := 0; i < len(data); {
		line, next := nextLine(data, i)
		marker, isMarker := docMarker(line)
		if isMarker && (opened || marker == '.') {
			return data[start:i]
		}
		if isMarker {
			start, opened = i+len("---"), true
		}
		if contentLine(line) {
			opened = true
		}
		i = next
	}
	return data[start:]
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

// docMarker reports whether line is a document marker — `---` opening one or
// `...` ending one — and which. A marker is the three bytes at column 0
// followed by whitespace or the end of the line; `----` and `...x` are content.
func docMarker(line []byte) (marker byte, ok bool) {
	if len(line) < 3 || (line[0] != '-' && line[0] != '.') {
		return 0, false
	}
	if line[1] != line[0] || line[2] != line[0] {
		return 0, false
	}
	if len(line) > 3 && line[3] != ' ' && line[3] != '\t' && line[3] != '\r' {
		return 0, false
	}
	return line[0], true
}

// contentLine reports whether line carries document content: anything but
// blank space, a comment, or a `%` directive. Content before any `---` opens
// the document implicitly, so a marker after it ends the document rather than
// opening one.
func contentLine(line []byte) bool {
	rest := bytes.TrimLeft(line, " \t\r")
	return len(rest) > 0 && rest[0] != '#' && rest[0] != '%'
}

// scanBlockProbe reads a block document's top-level entries, which are its lines
// beginning at column 0 — outside any flow collection or quoted scalar still
// open from a line above, since YAML continues both at any column. It allocates
// nothing per line: a document past the cap is megabytes of lines this walks
// and keeps none of.
//
// A document whose root is a flow collection has no block entries at all, and
// is left to scanFlowProbe: the parse reads nothing written after the closing
// bracket, and walking the collection here would only lex it a second time.
func scanBlockProbe(data []byte, probe *sniffProbe) {
	if i := skipSpaceAndComments(data, 0); i < len(data) && (data[i] == '{' || data[i] == '[') {
		return
	}
	sc := blockScan{probe: probe, scalarIndent: -1}
	for i := 0; i < len(data); {
		var line []byte
		line, i = nextLine(data, i)
		sc.line(line)
	}
	// A construct never closed is not a construct: the parse refuses such a
	// document, and what a scan owes an unreadable document that names the
	// key is to name its format, so the compile reports the break by name
	// (TestDetect_Formats, "key past the cap on an unparseable prefix"). The
	// lines read inside it were root lines after all.
	if sc.open() {
		sc.probe.fillFrom(sc.pending)
	}
}

// blockScan is the state the block scan carries from one line to the next: the
// flow collection or quoted scalar a line above opened and has not closed. A
// line inside one is not a root entry whatever column it begins in — the
// parse nests it — so the scan lexes through to the close instead of reading it.
//
// It is a lexer over three things only: quotes (with each style's escape),
// bracket depth, and the comments flow style admits. Everything it cannot be
// sure of it leaves open, since an open construct declines lines and a closed
// one claims them, and declining is the direction detection may be wrong in.
type blockScan struct {
	probe *sniffProbe
	depth int  // flow brackets open
	quote byte // the quote a scalar was opened with, or 0
	// atToken is whether the next byte in flow style begins a token, which is
	// where a quote opens a scalar; elsewhere it is content, as in `{a: it's}`.
	atToken bool
	// scalarIndent is the indentation of the entry whose block scalar (`|` or
	// `>`) is being read, or -1 outside one. Its content is every line indented
	// deeper, whatever those lines look like, and the first that is not ends it.
	scalarIndent int
	// pending holds what the lines inside an open construct said, kept apart
	// from probe until the construct closes — which discards them — or the
	// document ends with it open, which adopts them (see scanBlockProbe).
	pending sniffProbe
}

// open reports whether a construct from a line above is still open.
func (sc *blockScan) open() bool { return sc.quote != 0 || sc.depth > 0 }

// line reads one line: a root entry, if the line is one — into probe when
// nothing is open, into pending when something is; then whatever the line
// opens or closes, so the next line is read right.
func (sc *blockScan) line(line []byte) {
	if sc.scalarIndent >= 0 {
		if blankLine(line) || indentOf(line) > sc.scalarIndent {
			return
		}
		sc.scalarIndent = -1
	}
	// A root entry begins at column 0, so an indented line is not asked.
	if name, value, ok := blockEntry(line); ok && indentOf(line) == 0 {
		if sc.open() {
			setVersion(&sc.pending, name, value)
		} else {
			setVersion(sc.probe, name, value)
		}
	}
	sc.atToken = true
	for j := 0; j < len(line); {
		if sc.open() {
			j = sc.lex(line, j)
		} else {
			j = sc.openAt(line, j)
		}
	}
	if !sc.open() {
		sc.pending = sniffProbe{}
	}
}

// blankLine reports whether line carries nothing but blank space.
func blankLine(line []byte) bool { return skipBlank(line, 0) == len(line) }

// indentOf returns the column of the first byte of line that is not blank.
func indentOf(line []byte) int { return skipBlank(line, 0) }

// openAt finds the next place on line, from j, where a flow collection or a
// quoted scalar begins — the value of a block entry or a sequence item, or a
// quoted key — opens it, and returns the index just past its opener. It returns
// the line's end when the line opens nothing more.
//
// A value that is a block scalar indicator opens one instead, whose content
// the following lines carry (see scalarIndent): the indicator is what says
// those lines are text, whatever byte they begin with.
func (sc *blockScan) openAt(line []byte, j int) int {
	indent := skipBlank(line, j)
	j = indent
	for j < len(line) && line[j] == '-' && (j+1 == len(line) || isBlank(line[j+1])) {
		j = skipBlank(line, j+1)
	}
	if j == len(line) || line[j] == '#' {
		return len(line)
	}
	if sc.openWith(line[j]) {
		return j + 1
	}
	if blockScalarAt(line, j) {
		sc.scalarIndent = indent
		return len(line)
	}
	k := separatedColon(line, j)
	if k < 0 {
		return len(line)
	}
	k = skipBlank(line, k+1)
	if k == len(line) {
		return len(line)
	}
	if sc.openWith(line[k]) {
		return k + 1
	}
	if blockScalarAt(line, k) {
		sc.scalarIndent = indent
	}
	return len(line)
}

// blockScalarAt reports whether the value at line[j] is a block scalar
// indicator: `|` or `>`, followed by the end of the line, a blank, or a
// chomping or indentation modifier.
func blockScalarAt(line []byte, j int) bool {
	if line[j] != '|' && line[j] != '>' {
		return false
	}
	if j+1 == len(line) {
		return true
	}
	b := line[j+1]
	return isBlank(b) || b == '+' || b == '-' || (b >= '0' && b <= '9')
}

// openWith opens the construct b begins, and reports whether b begins one.
func (sc *blockScan) openWith(b byte) bool {
	switch b {
	case '{', '[':
		sc.depth = 1
		sc.atToken = true
	case '"', '\'':
		sc.quote = b
	default:
		return false
	}
	return true
}

// lex reads line from j through the close of whatever is open, and returns the
// index just past it — or the line's end, leaving the construct open.
func (sc *blockScan) lex(line []byte, j int) int {
	if sc.quote != 0 {
		return sc.lexQuoted(line, j)
	}
	return sc.lexFlow(line, j)
}

// lexQuoted reads a quoted scalar to its closing quote. A double-quoted scalar
// escapes with a backslash, a single-quoted one by doubling the quote; a
// backslash ending the line escapes the line break, and stepping past the end
// is the same as reaching it.
func (sc *blockScan) lexQuoted(line []byte, j int) int {
	for j < len(line) {
		switch {
		case sc.quote == '"' && line[j] == '\\':
			j += 2
		case line[j] != sc.quote:
			j++
		case sc.quote == '\'' && j+1 < len(line) && line[j+1] == '\'':
			j += 2
		default:
			sc.quote = 0
			return j + 1
		}
	}
	return len(line)
}

// lexFlow reads flow-style bytes: brackets nest, a quote at a token's start
// opens a scalar, and a `#` after a blank opens a comment that ends the line.
func (sc *blockScan) lexFlow(line []byte, j int) int {
	for j < len(line) {
		b := line[j]
		switch {
		case b == '{' || b == '[':
			sc.depth++
			sc.atToken = true
		case b == '}' || b == ']':
			sc.depth--
			sc.atToken = false
			if sc.depth == 0 {
				return j + 1
			}
		case b == ',' || b == ':':
			sc.atToken = true
		case isBlank(b):
			// Blank space neither begins nor ends a token.
		case b == '#' && (j == 0 || isBlank(line[j-1])):
			return len(line)
		case (b == '"' || b == '\'') && sc.atToken:
			sc.quote = b
			return j + 1
		default:
			sc.atToken = false
		}
		j++
	}
	return len(line)
}

// separatedColon returns the index of the first colon at or after j that is
// followed by a blank or the end of the line — the colon that ends a plain key
// — or -1 when there is none.
func separatedColon(line []byte, j int) int {
	for k := bytes.IndexByte(line[j:], ':'); k >= 0; k = bytes.IndexByte(line[j:], ':') {
		j += k
		if j+1 == len(line) || isBlank(line[j+1]) {
			return j
		}
		j++
	}
	return -1
}

// isBlank reports whether b is a space, a tab, or a carriage return.
func isBlank(b byte) bool { return b == ' ' || b == '\t' || b == '\r' }

// skipBlank returns the index of the first byte at or after j that is not
// blank, or len(line) if there is none.
func skipBlank(line []byte, j int) int {
	for j < len(line) && isBlank(line[j]) {
		j++
	}
	return j
}

// blockEntry returns the probe key line writes and the value beside it, and
// reports whether line writes one at all.
//
// A space, a tab or the end of the line has to follow the colon. YAML reads
// `openapi:3.1.0` as a plain scalar and not as a key — the parse below the cap
// refuses that document for having a string at its root — so a scan that took it
// for a key would name a format on bytes the parser says declare none.
//
// Whitespace between the name and the colon is YAML's to allow — `openapi :
// 3.1.0` keys the same entry — so the name is trimmed before it is compared.
func blockEntry(line []byte) (name, value []byte, ok bool) {
	name, rest, cut := bytes.Cut(line, []byte(":"))
	if !cut || !isProbeName(trimBlank(name)) || !separated(rest) {
		return nil, nil, false
	}
	return trimBlank(name), blockValue(rest), true
}

// trimBlank returns name without trailing spaces and tabs. The last byte is
// looked at before anything is trimmed, because this runs once per line of a
// document past the cap and nearly every line has nothing to trim.
func trimBlank(name []byte) []byte {
	if n := len(name); n == 0 || (name[n-1] != ' ' && name[n-1] != '\t') {
		return name
	}
	return bytes.TrimRight(name, " \t")
}

// separated reports whether rest, the bytes after a colon, begins the way a
// block mapping value must: with whitespace, or with nothing at all.
func separated(rest []byte) bool {
	return len(rest) == 0 || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r'
}

// blockValue returns the scalar a block entry writes after its colon, without the
// space around it, a trailing comment, or the quotes either style of quoting may
// have put around it.
//
// A comment begins at a `#` after whitespace — a space or a tab, since YAML
// admits either before one — and a `#` with neither before it is content.
func blockValue(raw []byte) []byte {
	value := bytes.TrimSpace(raw)
	if i := commentStart(value); i >= 0 {
		value = bytes.TrimSpace(value[:i])
	}
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	return value
}

// commentStart returns the index of the `#` that opens a trailing comment in
// value, or -1 when it carries none.
func commentStart(value []byte) int {
	for i := 1; i < len(value); i++ {
		if value[i] == '#' && (value[i-1] == ' ' || value[i-1] == '\t') {
			return i
		}
	}
	return -1
}

// scanFlowProbe reads the entries of the flow mapping data opens, which is the
// shape JSON writes. Nesting depth is what makes an entry the document's own: a
// quoted name followed by a colon reads as a key wherever it sits, and other
// formats nest one.
//
// The scan is a lexer, not a parser: it tracks quoted strings and nesting and
// reads nothing else. It has to answer on bytes that will not parse, which is the
// case it exists for — a document broken before the key that names it — so there
// is no tree to ask instead.
func scanFlowProbe(data []byte, probe *sniffProbe) {
	walkFlowRoot(data, func(name []byte, next int) (int, bool) {
		if !isProbeName(name) {
			return next, false
		}
		value, after, ok := flowValue(data, next)
		if !ok {
			return next, false
		}
		setVersion(probe, name, value)
		return after, false
	})
}

// flowValue returns the scalar written after the colon at i, and the index just
// past it. A name with no colon after it is no key, and a value that opens a
// collection is no version.
//
// A bare scalar is read as well as a quoted one, because the parse reads both:
// `"openapi": 3.1` is a number to JSON and a version to yaml.v3's scalar text
// alike, and a scan that took only the quoted spelling declined past the cap a
// document the parse claimed below it. The bare scalar ends where flow syntax
// ends it — at a comma, a closing bracket, or whitespace.
func flowValue(data []byte, i int) ([]byte, int, bool) {
	i = skipSpace(data, i)
	if i == len(data) || data[i] != ':' {
		return nil, i, false
	}
	if i = skipSpace(data, i+1); i == len(data) {
		return nil, i, false
	}
	if data[i] == '"' {
		value, next := flowString(data, i)
		return value, next, true
	}
	j := i
	for j < len(data) && !isFlowScalarEnd(data[j]) {
		j++
	}
	if j == i {
		return nil, i, false
	}
	return data[i:j], j, true
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

// setVersion stores value under probe's field for name.
func setVersion(probe *sniffProbe, name, value []byte) {
	switch string(name) {
	case "openapi":
		probe.OpenAPI = string(value)
	case "swagger":
		probe.Swagger = string(value)
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
