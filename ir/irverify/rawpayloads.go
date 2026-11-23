// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dexpace/morphic/ir"
)

var (
	unmodeledType = reflect.TypeFor[ir.Unmodeled]()
	rawConfigType = reflect.TypeFor[ir.RawConfig]()
)

// checkRawPayloads asserts the two maps that carry source JSON verbatim hold
// something a document can be marshaled with, and that each Unmodeled entry is
// one a consumer can route.
//
// Both are checked in one walk: their payload is the same type and the same
// hazard. An ir.RawValue is written into the output as the JSON it holds, so a
// payload that is not a JSON value fails the whole document's encoding
// (invariant #7).
//
// Verify sorts the result by (Code, Path), so entries need no ordering.
func checkRawPayloads(*ir.Document) walkCheck {
	var vs []Violation
	var reader entryReader
	var payloads payloadReader
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Kind() != reflect.Map {
				return true
			}
			switch v.Type() {
			case unmodeledType:
				vs = appendEntries(vs, &reader, &payloads, v, at, unmodeledEntry)
			case rawConfigType:
				vs = appendEntries(vs, &reader, &payloads, v, at, rawConfigEntry)
			default:
				return true // any other map may still hold one of these below it
			}
			return false // entries hold no nested payload map
		},
		report: reporting(&vs),
	}
}

// appendEntries appends check's verdict on every entry of the payload map m, at
// at, to vs, reading the entries through entryReaderValue and their payloads
// through payloadReaderValue. Both callers key on a plain string, so the key
// spells the entry's path.
func appendEntries(vs []Violation, entryReaderValue *entryReader, payloadReaderValue *payloadReader, m reflect.Value, at ir.WalkPath,
	check func(reader *entryReader, otherPayloadReader *payloadReader, key string, entry reflect.Value, at ir.WalkPath) []Violation,
) []Violation {
	held := entryReaderValue.heldFor(m)
	for iter := m.MapRange(); iter.Next(); {
		key, entry := held.read(iter)
		vs = append(vs, check(entryReaderValue, payloadReaderValue, key.String(), entry, at)...)
	}
	return vs
}

// unmodeledEntry checks one entry of an Unmodeled map: a non-empty
// origin-namespaced key, a marshalable payload, a declared UnmodeledReason,
// and RFC 6901 JSON Pointer keys on Targets that resolve inside Value.
// Consumers select entries by reason (see ir.UnmodeledEntry.Reason), so an entry
// left at the zero reason is bytes nothing can route, and one keyed "" cannot be
// looked up and is overwritten by the next empty-keyed entry. Both are compiler
// bugs of the same class checkRegistryKeys reports as ir/empty-*-id.
//
// The defects are checked independently so a doubly-broken entry names each of
// its defects rather than stopping at the first.
func unmodeledEntry(reader *entryReader, payloadReaderValue *payloadReader, key string, entry reflect.Value, at ir.WalkPath) []Violation {
	var vs []Violation
	if key == "" {
		vs = append(vs, Violation{
			Code:    "ir/empty-unmodeled-key",
			Message: "unmodeled map has an empty key",
		})
	}
	valueField := entry.FieldByName("Value")
	var validValue bool
	vs, validValue = appendRawValue(vs, payloadReaderValue, valueField, "unmodeled entry")

	switch reason := ir.UnmodeledReason(entry.FieldByName("Reason").String()); {
	case reason == "":
		vs = append(vs, Violation{
			Code:    "ir/empty-unmodeled-reason",
			Message: "unmodeled entry leaves its reason at the zero value",
		})
	case !reason.Valid():
		vs = append(vs, Violation{
			Code:    "ir/unknown-unmodeled-reason",
			Message: "unmodeled entry carries undeclared reason " + strconv.Quote(string(reason)),
		})
	}
	vs = placeEntry(vs, at, key, `[""]`)
	return appendTargetPointerViolations(vs, reader, payloadReaderValue, entry.FieldByName("Targets"), valueField.Bytes(), validValue, at, key)
}

// appendTargetPointerViolations reports any key of UnmodeledEntry.Targets that
// is not an RFC 6901 JSON Pointer or does not resolve to a JSON value inside
// Value; empty or dangling values are checkEmptyRefs' and
// checkReferentialIntegrity's own reports, and invalid UTF-8 is checkUTF8's.
func appendTargetPointerViolations(vs []Violation, reader *entryReader, payloads *payloadReader, targets reflect.Value,
	raw []byte, validValue bool, at ir.WalkPath, key string,
) []Violation {
	if targets.Len() == 0 {
		return vs
	}
	held := reader.heldFor(targets)
	var base string
	targetPath := func(ptr string) string {
		if base == "" {
			base = rawEntryPath(at, key, `[""]`) + ".Targets["
		}
		return base + ptr + "]"
	}
	for iter := targets.MapRange(); iter.Next(); {
		ptrVal, _ := held.read(iter)
		ptr := ptrVal.String()
		if !jsontext.Pointer(strings.ToValidUTF8(ptr, "\uFFFD")).IsValid() {
			vs = append(vs, Violation{
				Code:    "ir/unmodeled-target-pointer-malformed",
				Message: "unmodeled target pointer " + strconv.Quote(ptr) + " is not an RFC 6901 JSON Pointer",
				Path:    targetPath(ptr),
			})
			continue
		}
		if validValue && utf8.ValidString(ptr) && !payloads.resolves(raw, jsontext.Pointer(ptr)) {
			vs = append(vs, Violation{
				Code:    "ir/unmodeled-target-pointer-unresolved",
				Message: "unmodeled target pointer " + strconv.Quote(ptr) + " does not resolve inside Value",
				Path:    targetPath(ptr),
			})
		}
	}
	return vs
}

// rawConfigEntry checks one entry of a RawConfig map. Unlike an Unmodeled entry
// it carries no reason to check — the source declared it where the IR expects it
// — so the payload is all there is to hold it to.
func rawConfigEntry(_ *entryReader, payloadReaderValue *payloadReader, key string, entry reflect.Value, at ir.WalkPath) []Violation {
	vs, _ := appendRawValue(nil, payloadReaderValue, entry, "protocol config")
	return placeEntry(vs, at, key, "[]")
}

// rawEntryPath spells the path of one raw-payload map entry at at, keyed by
// key, using empty when key is empty.
func rawEntryPath(at ir.WalkPath, key, empty string) string {
	if key == "" {
		return at.String() + empty
	}
	return at.String() + "[" + key + "]"
}

// placeEntry gives each of one entry's violations vs the entry's path: the map
// at at, then the key in brackets, or empty when the key is. The path is spelled
// only when there is something to place.
func placeEntry(vs []Violation, at ir.WalkPath, key, empty string) []Violation {
	if len(vs) == 0 {
		return nil
	}
	path := rawEntryPath(at, key, empty)
	for i := range vs {
		vs[i].Path = path
	}
	return vs
}

// appendRawValue appends a violation to vs when the payload at value is not a
// JSON value, as payloadReaderValue reads it, naming its carrier in what, and
// reports whether the payload is valid JSON.
//
// Reading through reflect.Value.Bytes rather than Interface() keeps Verify a
// report-only oracle: the caller has matched the map's exact type, so Bytes
// never panics however the payload was reached.
//
// Nil is reported with malformed and empty bytes: a nil jsontext.Value encodes
// as null but decodes back as the four bytes "null", so the document stops
// round-tripping to an equal one.
func appendRawValue(vs []Violation, payloadReaderValue *payloadReader, value reflect.Value, what string) ([]Violation, bool) {
	raw := value.Bytes()
	if payloadReaderValue.valid(raw) {
		return vs, true
	}
	message := what + " value is not a JSON value, so the document cannot be marshaled"
	if raw == nil {
		message = what + " value is nil, which encodes as null and decodes back as the bytes null rather than nil"
	}
	return append(vs, Violation{Code: "ir/invalid-raw-value", Message: message}), false
}

// payloadReader reads raw payloads through one decoder and one reader it resets
// for each, so a payload costs nothing once the decoder's buffer has grown to
// the largest met. jsontext.Value.IsValid borrows a decoder from a pool for
// each instead, and a race build drops a share of what a pool is handed, so
// there each payload read that way costs an allocation at random. The zero
// payloadReader is ready to use.
type payloadReader struct {
	src    bytes.Reader
	dec    jsontext.Decoder
	keyBuf []byte
	tokBuf []byte
}

// valid reports whether raw is one JSON value, by the rules
// jsontext.Value.IsValid holds a value to by default: valid UTF-8, escapes that
// decode, no name twice in one object, and nothing after the value but
// whitespace.
func (p *payloadReader) valid(raw []byte) bool {
	p.src.Reset(raw)
	p.dec.Reset(&p.src)
	if p.dec.SkipValue() != nil {
		return false
	}
	_, err := p.dec.ReadToken()
	return errors.Is(err, io.EOF)
}

// resolves reports whether ptr, a valid RFC 6901 JSON Pointer, addresses a
// JSON value inside raw, a valid JSON value. It reuses p's decoder and scratch
// buffers so clean lookups allocate nothing once warmed up.
func (p *payloadReader) resolves(raw []byte, ptr jsontext.Pointer) bool {
	if ptr == "" {
		return true
	}
	p.src.Reset(raw)
	p.dec.Reset(&p.src)
	for rest := string(ptr); rest != ""; {
		rest = rest[1:]
		seg := rest
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			seg, rest = rest[:slash], rest[slash:]
		} else {
			rest = ""
		}
		if !p.step(seg) {
			return false
		}
	}
	return true
}

// step advances p.dec from the current JSON container to the member or element
// named by the raw (RFC 6901-escaped) token seg.
func (p *payloadReader) step(seg string) bool {
	switch p.dec.PeekKind() {
	case '{':
		return p.stepObject(seg)
	case '[':
		return p.stepArray(seg)
	default:
		return false
	}
}

// stepObject positions p.dec at the value of the object member whose name
// equals the unescaped RFC 6901 token seg.
func (p *payloadReader) stepObject(seg string) bool {
	want := p.unescapeToken(seg)
	_, _ = p.dec.ReadToken()
	for p.dec.PeekKind() != '}' {
		rawKey, _ := p.dec.ReadValue()
		p.keyBuf, _ = jsontext.AppendUnquote(p.keyBuf[:0], rawKey)
		if bytes.Equal(p.keyBuf, want) {
			return true
		}
		_ = p.dec.SkipValue()
	}
	return false
}

// stepArray positions p.dec at the array element named by the RFC 6901 array
// index seg ("0" or [1-9][0-9]*).
func (p *payloadReader) stepArray(seg string) bool {
	idx, ok := parsePointerIndex(seg)
	if !ok {
		return false
	}
	_, _ = p.dec.ReadToken()
	for i := 0; p.dec.PeekKind() != ']'; i++ {
		if i == idx {
			return true
		}
		_ = p.dec.SkipValue()
	}
	return false
}

// unescapeToken decodes an RFC 6901 reference token (~1 -> /, ~0 -> ~) into
// p.tokBuf with no allocation once the buffer has grown.
func (p *payloadReader) unescapeToken(seg string) []byte {
	p.tokBuf = p.tokBuf[:0]
	for i := 0; i < len(seg); i++ {
		if seg[i] == '~' {
			i++
			if seg[i] == '1' {
				p.tokBuf = append(p.tokBuf, '/')
			} else {
				p.tokBuf = append(p.tokBuf, '~')
			}
			continue
		}
		p.tokBuf = append(p.tokBuf, seg[i])
	}
	return p.tokBuf
}

// parsePointerIndex parses an RFC 6901 array index ("0" or [1-9][0-9]*).
func parsePointerIndex(seg string) (int, bool) {
	if seg == "0" {
		return 0, true
	}
	if len(seg) == 0 || seg[0] < '1' || seg[0] > '9' {
		return 0, false
	}
	idx, err := strconv.Atoi(seg)
	return idx, err == nil
}
