// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package detect

import (
	"bytes"

	yaml "go.yaml.in/yaml/v3"
)

// declaresProbeKey reports whether data declares a version under a
// discriminating key. Recognize asks only once the probe cannot read the
// source, where "not YAML" alone fits a Protobuf or Smithy source too, and only
// within its byte budget.
//
// Neither reading may widen to "the name occurs before a colon": other formats
// nest such a key and must not get this compiler's parse error. The version
// makes the claim (GitHub #497), so a spec broken on its version line,
// `openapi: [3.1.0`, is unrecognized rather than unreadable, and a README
// quoting a spec in a code block is claimed like one.
func declaresProbeKey(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte(bomUTF8))
	return declaresBlockKey(data, "openapi") || declaresBlockKey(data, "swagger") ||
		declaresFlowKey(data)
}

// bomUTF8 is the UTF-8 byte-order mark. YAML admits one at the start of a
// stream and JSON is its subset, so a spec written by an editor that emits one
// is a spec like any other: yaml.v3 reads straight through it and so does the
// loader. declaresProbeKey reads bytes rather than a parse and has to skip it
// itself, or a spec with one would be declined in silence when broken where a
// spec without one is reported.
const bomUTF8 = "\xef\xbb\xbf"

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
