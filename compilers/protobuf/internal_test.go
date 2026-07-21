package protobuf // white-box: exercises unexported helpers and defensive branches

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// mustLoad compiles one .proto source through the load phase and returns its
// linked file descriptor.
func mustLoad(t *testing.T, src string) protoreflect.FileDescriptor {
	t.Helper()
	ld, diags, err := load(context.Background(), 0,
		compilers.Source{Path: "t.proto", Data: []byte(src)}, Options{}.withDefaults())
	if err != nil || ld == nil {
		t.Fatalf("load failed: err=%v diags=%v", err, diags)
	}
	return ld.File
}

func TestWrapperPrim_All(t *testing.T) {
	cases := map[string]ir.PrimKind{
		"google.protobuf.DoubleValue": ir.PrimFloat64,
		"google.protobuf.FloatValue":  ir.PrimFloat32,
		"google.protobuf.Int64Value":  ir.PrimInt64,
		"google.protobuf.UInt64Value": ir.PrimUint64,
		"google.protobuf.Int32Value":  ir.PrimInt32,
		"google.protobuf.UInt32Value": ir.PrimUint32,
		"google.protobuf.BoolValue":   ir.PrimBool,
		"google.protobuf.StringValue": ir.PrimString,
		"google.protobuf.BytesValue":  ir.PrimBytes,
	}
	for name, want := range cases {
		got, ok := wrapperPrim(name)
		if !ok || got != want {
			t.Errorf("wrapperPrim(%q) = %q,%v; want %q,true", name, got, ok, want)
		}
	}
	if _, ok := wrapperPrim("google.protobuf.Timestamp"); ok {
		t.Error("wrapperPrim must report false for a non-wrapper type")
	}
}

func TestScalarPrim_All(t *testing.T) {
	scalars := []protoreflect.Kind{
		protoreflect.BoolKind, protoreflect.StringKind, protoreflect.BytesKind,
		protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Int64Kind,
		protoreflect.Sint64Kind, protoreflect.Sfixed64Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed64Kind, protoreflect.FloatKind, protoreflect.DoubleKind,
	}
	for _, k := range scalars {
		if _, _, ok := scalarPrim(k); !ok {
			t.Errorf("scalarPrim(%v) reported non-scalar", k)
		}
	}
	for _, k := range []protoreflect.Kind{protoreflect.EnumKind, protoreflect.MessageKind, protoreflect.GroupKind} {
		if _, _, ok := scalarPrim(k); ok {
			t.Errorf("scalarPrim(%v) reported scalar", k)
		}
	}
}

func TestPackable(t *testing.T) {
	for _, k := range []protoreflect.Kind{protoreflect.StringKind, protoreflect.BytesKind, protoreflect.MessageKind, protoreflect.GroupKind} {
		if packable(k) {
			t.Errorf("packable(%v) should be false", k)
		}
	}
	if !packable(protoreflect.Int32Kind) {
		t.Error("packable(int32) should be true")
	}
}

func TestScopeOf(t *testing.T) {
	if got := scopeOf("a.b.c"); got != "a.b" {
		t.Errorf("scopeOf qualified = %q", got)
	}
	if got := scopeOf("bare"); got != "bare" {
		t.Errorf("scopeOf unqualified = %q", got)
	}
}

func TestLastSegment(t *testing.T) {
	if got := lastSegment("google.protobuf.Empty"); got != "Empty" {
		t.Errorf("lastSegment qualified = %q", got)
	}
	if got := lastSegment("bare"); got != "bare" {
		t.Errorf("lastSegment unqualified = %q", got)
	}
}

func TestPackageWords(t *testing.T) {
	if got := packageWords(""); got != nil {
		t.Errorf("packageWords(empty) = %v; want nil", got)
	}
	got := packageWords("a.b")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("packageWords(a.b) = %v", got)
	}
}

func TestCanonicalWords_Boundaries(t *testing.T) {
	cases := map[string]string{
		"HTTPServer": "http_server",
		"userID":     "user_id",
		"api2key":    "api_2_key",
		"snake_case": "snake_case",
	}
	for in, want := range cases {
		if got := canonicalWords(in); got != want {
			t.Errorf("canonicalWords(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestCleanComment(t *testing.T) {
	if got := cleanComment(""); got != "" {
		t.Errorf("cleanComment(empty) = %q", got)
	}
	if got := cleanComment(" line one\n line two\n"); got != "line one\nline two" {
		t.Errorf("cleanComment = %q", got)
	}
	huge := strings.Repeat("x\n", maxCommentLines+10)
	if got := cleanComment(huge); got == "" {
		t.Error("cleanComment truncation should still return content")
	}
}

func TestJSONRaw_Error(t *testing.T) {
	if _, ok := jsonRaw(math.NaN()); ok {
		t.Error("jsonRaw(NaN) should fail: NaN is not valid JSON")
	}
	if _, ok := jsonRaw("ok"); !ok {
		t.Error("jsonRaw(string) should succeed")
	}
}

func TestReservedRaw_Empty(t *testing.T) {
	if reservedRaw(nil, nil) != nil {
		t.Error("reservedRaw with no ranges or names must be nil")
	}
}

func TestImportOrCompileCode(t *testing.T) {
	if got := importOrCompileCode(fs.ErrNotExist); got != codeUnresolvedImport {
		t.Errorf("importOrCompileCode(ErrNotExist) = %q", got)
	}
	if got := importOrCompileCode(errors.New("boom")); got != codeCompile {
		t.Errorf("importOrCompileCode(other) = %q", got)
	}
}

func TestEnumMemberName(t *testing.T) {
	file := mustLoad(t, "syntax = \"proto3\";\npackage e;\nenum K { A = 0; B = 1; }\n")
	ed := file.Enums().Get(0)
	if got := enumMemberName(ed, 1); got != "B" {
		t.Errorf("enumMemberName(1) = %v; want B", got)
	}
	if got := enumMemberName(ed, 999); got != int64(999) {
		t.Errorf("enumMemberName(unknown) = %v; want 999", got)
	}
}

func TestOptionBool_MissingOption(t *testing.T) {
	file := mustLoad(t, "syntax = \"proto3\";\npackage o;\nmessage M {\n  option deprecated = true;\n  string s = 1;\n}\n")
	md := file.Messages().Get(0)
	if !optionBool(md, "deprecated") {
		t.Error("optionBool should read a set deprecated option")
	}
	if optionBool(md, "no_such_option") {
		t.Error("optionBool must report false for a missing option field")
	}
}

func TestRenderDepthGuard(t *testing.T) {
	// A message-valued custom option gives us a real option message and field to
	// drive the recursion-depth guards past their cap.
	const src = `syntax = "proto2";
package d;
import "google/protobuf/descriptor.proto";
message Box { optional string v = 1; }
extend google.protobuf.MessageOptions { optional Box box = 50300; }
message M {
  option (box) = { v: "x" };
}
`
	file := mustLoad(t, src)
	opts := optionsMessage(file.Messages().Get(1)) // M's options
	var boxFD protoreflect.FieldDescriptor
	var boxVal protoreflect.Value
	opts.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.IsExtension() {
			boxFD, boxVal = fd, v
		}
		return true
	})
	if boxFD == nil {
		t.Fatal("the custom option must be present")
	}
	if _, ok := renderValue(boxFD, boxVal, maxOptionDepth+1); ok {
		t.Error("renderValue past the depth cap must report failure")
	}
	if _, ok := renderMessage(boxVal.Message(), maxOptionDepth+1); ok {
		t.Error("renderMessage past the depth cap must report failure")
	}
}

func TestSyntaxDigit(t *testing.T) {
	p2 := mustLoad(t, "syntax = \"proto2\";\npackage a;\nmessage M { optional string s = 1; }\n")
	if got := syntaxDigit(p2); got != "2" {
		t.Errorf("syntaxDigit(proto2) = %q", got)
	}
	p3 := mustLoad(t, "syntax = \"proto3\";\npackage b;\nmessage M { string s = 1; }\n")
	if got := syntaxDigit(p3); got != "3" {
		t.Errorf("syntaxDigit(proto3) = %q", got)
	}
	ed := mustLoad(t, "edition = \"2023\";\npackage c;\nmessage M { string s = 1; }\n")
	if got := syntaxDigit(ed); got != "2023" {
		t.Errorf("syntaxDigit(editions) = %q", got)
	}
}
