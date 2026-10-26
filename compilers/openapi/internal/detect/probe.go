package detect

import (
	"context"
	"errors"
	"fmt"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// sniffProbe holds the two discriminating top-level keys. Which one is present
// is the whole of the format question: an OpenAPI 3.x document declares
// `openapi`, a Swagger 2.0 document declares `swagger`.
//
// It carries no struct tags: nothing decodes into it. The parse over a tree
// names the two keys itself, in fieldFor.
type sniffProbe struct {
	OpenAPI string
	Swagger string
	// Cut reports a root mapping that merges through `<<` past
	// nodeview.MergeDepthLimit without settling the answer, so a key the read
	// did not reach may outrank what it found.
	Cut bool
}

// sniff reads the discriminating keys out of data. For anything it cannot read
// it returns the zero probe and the parser's error; whether that error is
// worth reporting is Recognize's question.
//
// The document is decoded whole and exactly, the only way to tell one that
// declares nothing from one that will not parse. The parse is the one the
// compile lowers, not a second reading beside it that would have to be kept in
// agreement with it (GitHub #486). The caller's byte budget, which Recognize
// checks first, bounds it.
func sniff(ctx context.Context, data []byte) (sniffProbe, *load.Parsed, error) {
	probe, parsed, err := decodeYAML(ctx, data)
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

// decodeYAML reads the probe keys from a complete YAML (or JSON, its subset)
// document.
//
// The document is parsed and its root mapping read, never decoded into
// sniffProbe: yaml.v3 compares every pair of a mapping's keys before reading
// any, so a mapping repeating one key n times raises n(n-1)/2 errors and then
// abandons the mapping, leaving the probe empty as well as expensive. Reading
// the keys off the tree is linear and answers the same for repeated keys,
// which the compile's own parse reports once each, sited.
func decodeYAML(ctx context.Context, data []byte) (sniffProbe, *load.Parsed, error) {
	parsed, err := load.Decode(ctx, data)
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
		probe, err := probeFromMapping(root)
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

// probeFromMapping reads the probe keys off a root mapping through nodeview,
// the one reading of a mapping the compile's parser gives: through aliases, its
// `<<` merges expanded as the library expands them, and a key written twice at
// its last value (docs/parser-library.md#duplicate-key). A second reading
// would let detection name a version the compile does not read, which
// compilers.Detection.Compile checks.
//
// Only a key's effective value is read, so a value it overrides cannot fail
// the probe. Only the depth bound hides a key (View.Exhausted): a merge cycle,
// which the compile refuses, closes having read every mapping on it.
func probeFromMapping(root *yaml.Node) (sniffProbe, error) {
	view := nodeview.New()
	var probe sniffProbe
	for _, pair := range view.MappingPairs(root) {
		field := probe.fieldFor(pair.Key)
		if field == nil {
			continue
		}
		version, err := probeVersion(pair.Val)
		if err != nil {
			return sniffProbe{}, err
		}
		*field = version
	}
	probe.Cut = view.Exhausted() && !settledByOwnPairs(root, probe)
	return probe, nil
}

// settledByOwnPairs reports whether probe, read off root, decides detection
// although root's merges were cut short. A mapping's own pair outranks every
// merge, so a key root writes itself holds the value read: `openapi` settles it
// when that value is a version, and with `swagger` written too whatever it is.
// A key root leaves to its merges may lie past the bound.
func settledByOwnPairs(root *yaml.Node, probe sniffProbe) bool {
	own := nodeview.WrittenChildren(root)
	_, writesOpenAPI := own["openapi"]
	_, writesSwagger := own["swagger"]
	return writesOpenAPI && (isVersion(probe.OpenAPI) || writesSwagger)
}

// probeVersion returns the version string a probe key's value declares, and an
// error for a value that is no scalar, or none at all.
//
// The scalar's text is taken as written rather than decoded, because the two
// disagree only for tags no version carries — a version key is not !!binary —
// and because decoding is what must not happen here: a mapping handed back to
// the decoder is the quadratic path decodeYAML exists to avoid, and a probe
// key's own value is the last place one could still be handed to it.
func probeVersion(value *yaml.Node) (string, error) {
	if value == nil {
		return "", errors.New("version key has no value")
	}
	if value.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("version key is %s, not a scalar", value.ShortTag())
	}
	return value.Value, nil
}

// fieldFor returns the probe field that key names, or nil for a key that names
// neither.
func (p *sniffProbe) fieldFor(key string) *string {
	switch key {
	case "openapi":
		return &p.OpenAPI
	case "swagger":
		return &p.Swagger
	default:
		return nil
	}
}
