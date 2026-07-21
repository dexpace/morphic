package protobuf

import (
	"strings"
	"unicode"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// canonicalWords renders name as a neutral lower_snake word sequence: it splits
// on _/-/space/dot and on camel-case and letter/digit boundaries, lowercases,
// and joins with "_". It holds no acronym opinion beyond boundary detection;
// casing policy is an emitter concern (ir-design §3.2).
func canonicalWords(name string) string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		if r == '_' || r == '-' || r == ' ' || r == '.' {
			flush()
			continue
		}
		if len(cur) > 0 && wordBoundary(cur[len(cur)-1], r, runes, i) {
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return strings.Join(words, "_")
}

// wordBoundary reports whether a new word starts at runes[i] given the previous
// accumulated rune prev.
func wordBoundary(prev, r rune, runes []rune, i int) bool {
	switch {
	case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
		return true // lower/digit -> Upper: "userID" -> user|ID
	case unicode.IsUpper(prev) && unicode.IsUpper(r) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
		return true // acronym tail: "HTTPServer" -> HTTP|Server
	case unicode.IsLetter(prev) && unicode.IsDigit(r), unicode.IsDigit(prev) && unicode.IsLetter(r):
		return true // letter<->digit: "APIKey2" -> ...Key|2
	default:
		return false
	}
}

// packageWords splits a proto package path ("example.v1") into its segments,
// used as a type's or service's Namespace. An empty package yields nil.
func packageWords(pkg string) []string {
	if pkg == "" {
		return nil
	}
	return strings.Split(pkg, ".")
}

// jsonNameDefault reproduces protobuf's auto-derived JSON name for a field name:
// underscores are dropped and the following letter is upper-cased, leaving all
// other characters untouched ("created_at" → "createdAt"). It is the baseline an
// explicit json_name option is compared against so the IR never stores a
// compiler-derived camelCase as a wire name.
func jsonNameDefault(protoName string) string {
	var b strings.Builder
	b.Grow(len(protoName))
	upperNext := false
	for _, r := range protoName {
		if r == '_' {
			upperNext = true
			continue
		}
		if upperNext {
			b.WriteRune(unicode.ToUpper(r))
			upperNext = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// explicitWireName returns a field's serialized name only when its json_name was
// explicitly overridden — i.e. it differs from the auto-derived default. An
// auto-derived camelCase is left to the emitter, which owns casing.
func explicitWireName(fd protoreflect.FieldDescriptor) string {
	if name := string(fd.Name()); fd.JSONName() != jsonNameDefault(name) {
		return fd.JSONName()
	}
	return ""
}
