package protobuf

import (
	"encoding/json"
	"strings"

	"github.com/bufbuild/protocompile/protoutil"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/dexpace/morphic/ir"
)

// lowerService lowers the file into the single document service, one
// OperationGroup per proto service (ir-design §7.1). A file with no services
// still yields the service so the document always carries one.
func (l *lowerer) lowerService() ir.Service {
	pkg := string(l.file.Package())
	svc := ir.Service{
		ID:         serviceID(l.serviceScope()),
		Name:       ir.Naming{Source: pkg, Canonical: canonicalWords(pkg)},
		Namespace:  packageWords(pkg),
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: l.file.Path()},
	}
	services := l.file.Services()
	for i := range services.Len() {
		svc.Groups = append(svc.Groups, l.lowerOperationGroup(services.Get(i)))
	}
	return svc
}

// serviceScope is the identity scope of the document service: the proto package,
// or the source path when the file declares no package.
func (l *lowerer) serviceScope() string {
	if pkg := string(l.file.Package()); pkg != "" {
		return pkg
	}
	return l.file.Path()
}

// lowerOperationGroup lowers one proto service into an OperationGroup of rpc
// operations.
func (l *lowerer) lowerOperationGroup(sd protoreflect.ServiceDescriptor) ir.OperationGroup {
	name := string(sd.Name())
	g := ir.OperationGroup{
		Name: ir.Naming{Source: name, Canonical: canonicalWords(name)},
		Docs: l.docsFor(sd),
	}
	if ext := l.customOptions(sd); len(ext) > 0 {
		g.Extensions = ext
	}
	methods := sd.Methods()
	for i := range methods.Len() {
		g.Operations = append(g.Operations, l.lowerMethod(sd, methods.Get(i)))
	}
	return g
}

// lowerMethod lowers one rpc into an Operation with a gRPC RPCBinding. The
// request/response messages carry the payloads; streaming modifiers and the
// idempotency level lower onto the neutral core.
func (l *lowerer) lowerMethod(sd protoreflect.ServiceDescriptor, md protoreflect.MethodDescriptor) ir.Operation {
	name := string(md.Name())
	idem, level := methodIdempotency(md)
	op := ir.Operation{
		ID:          opID(string(md.FullName())),
		Name:        ir.Naming{Source: name, Canonical: canonicalWords(name)},
		Docs:        l.docsFor(md),
		Idempotency: idem,
		Provenance:  ir.Provenance{Source: l.srcIndex, Pointer: string(md.FullName())},
	}
	inRef, inEmpty := l.rpcMessage(md.Input())
	outRef, outEmpty := l.rpcMessage(md.Output())
	rpc := &ir.RPCBinding{
		System:           "grpc",
		FullMethod:       "/" + string(sd.FullName()) + "/" + name,
		IdempotencyLevel: level,
	}
	if !inEmpty {
		in := inRef
		op.Request = &ir.Payload{Contents: []ir.Content{{Type: in}}}
		rpc.InputType = &in
	}
	op.Responses = []ir.Response{rpcResponse(outRef, outEmpty)}
	applyStreaming(&op, md)
	op.Bindings = ir.OpBindings{RPC: rpc}
	if dep := deprecationOf(md); dep != nil {
		op.Deprecation = dep
	}
	if ext := l.customOptions(md); len(ext) > 0 {
		op.Extensions = ext
	}
	return op
}

// rpcMessage resolves an rpc request/response message type, reporting whether it
// is google.protobuf.Empty (which lowers to an absent payload, not a type).
func (l *lowerer) rpcMessage(md protoreflect.MessageDescriptor) (ir.TypeRef, bool) {
	if string(md.FullName()) == "google.protobuf.Empty" {
		return ir.TypeRef{}, true
	}
	return l.messageOrWKT(md), false
}

// rpcResponse builds the single response of an rpc: empty Conditions (RPC has no
// status codes) and a payload unless the response message is Empty.
func rpcResponse(ref ir.TypeRef, empty bool) ir.Response {
	resp := ir.Response{Name: ir.Naming{Hint: "response"}}
	if !empty {
		resp.Payload = &ir.Payload{Contents: []ir.Content{{Type: ref}}}
	}
	return resp
}

// applyStreaming records the operation's streaming direction from the rpc's
// client/server stream modifiers.
func applyStreaming(op *ir.Operation, md protoreflect.MethodDescriptor) {
	cs, ss := md.IsStreamingClient(), md.IsStreamingServer()
	switch {
	case cs && ss:
		op.Streaming = ir.StreamingBidi
		op.RequestStream = &ir.StreamDetail{}
		op.ResponseStream = &ir.StreamDetail{}
	case cs:
		op.Streaming = ir.StreamingClient
		op.RequestStream = &ir.StreamDetail{}
	case ss:
		op.Streaming = ir.StreamingServer
		op.ResponseStream = &ir.StreamDetail{}
	}
}

// methodIdempotency maps the rpc idempotency_level option onto the IR
// idempotency classification and its raw level string.
func methodIdempotency(md protoreflect.MethodDescriptor) (ir.Idempotency, string) {
	m := optionsMessage(md)
	fd := m.Descriptor().Fields().ByName("idempotency_level")
	switch m.Get(fd).Enum() {
	case 1: // NO_SIDE_EFFECTS
		return ir.Idempotency{Kind: ir.IdempotencySafe}, "NO_SIDE_EFFECTS"
	case 2: // IDEMPOTENT
		return ir.Idempotency{Kind: ir.IdempotencyIdempotent}, "IDEMPOTENT"
	default:
		return ir.Idempotency{}, ""
	}
}

// lowerExtensions attaches every proto2 extension field to the message it
// extends, walking file-level and message-nested extend blocks.
func (l *lowerer) lowerExtensions() {
	l.lowerExtensionSet(l.file.Extensions())
	msgs := l.file.Messages()
	for i := range msgs.Len() {
		l.lowerMessageExtensions(msgs.Get(i))
	}
}

// lowerMessageExtensions attaches extend blocks nested in a message and recurses.
func (l *lowerer) lowerMessageExtensions(md protoreflect.MessageDescriptor) {
	l.lowerExtensionSet(md.Extensions())
	nested := md.Messages()
	for i := range nested.Len() {
		l.lowerMessageExtensions(nested.Get(i))
	}
}

// lowerExtensionSet lowers each extension descriptor in a set.
func (l *lowerer) lowerExtensionSet(exts protoreflect.ExtensionDescriptors) {
	for i := range exts.Len() {
		l.lowerExtensionField(exts.Get(i))
	}
}

// lowerExtensionField attaches one extension field to the Model it extends,
// recording its declaring scope in Property.ExtensionOf. An extension that
// targets a well-known options message defines a custom option and is preserved
// at document level instead of contributing a data property.
func (l *lowerer) lowerExtensionField(ext protoreflect.FieldDescriptor) {
	extended := ext.ContainingMessage()
	model, ok := l.out.Types[namedTypeID(string(extended.FullName()))].(*ir.Model)
	if !ok {
		l.recordOptionDefinition(ext, extended)
		return
	}
	prop := l.lowerField(ext)
	prop.ExtensionOf = scopeOf(string(ext.FullName()))
	model.Properties = append(model.Properties, prop)
}

// recordOptionDefinition preserves a custom-option-defining extension at document
// level and emits an info diagnostic; it contributes no model property.
func (l *lowerer) recordOptionDefinition(ext protoreflect.FieldDescriptor, extended protoreflect.MessageDescriptor) {
	def := map[string]any{
		"extends": string(extended.FullName()),
		"number":  int32(ext.Number()),
		"name":    string(ext.FullName()),
	}
	raw, _ := json.Marshal(def) // a map of strings and an int always marshals
	l.out.Extensions = mergeRaw(l.out.Extensions,
		"protobuf:custom-option:"+string(ext.FullName()), ir.RawValue(raw))
	l.diags = append(l.diags, diagf(ir.SeverityInfo, codeCustomOptionDefinition,
		ir.Provenance{Source: l.srcIndex, Pointer: string(ext.FullName())},
		"extension %s defines a custom option on %s", ext.FullName(), extended.FullName()))
}

// scopeOf is the declaring scope of a fully-qualified name: the name with its
// final segment removed, or the name itself when it has no qualifier.
func scopeOf(fullName string) string {
	if i := strings.LastIndex(fullName, "."); i >= 0 {
		return fullName[:i]
	}
	return fullName
}

// lowerMeta records file-level metadata: the package as the document name and
// the file's syntax, edition, and standard options preserved in Extensions
// (ir-design §12, per-file metadata keyed under Document.Extensions).
func (l *lowerer) lowerMeta() {
	l.out.Name = string(l.file.Package())
	if raw := l.fileOptionsRaw(); raw != nil {
		l.out.Extensions = mergeRaw(l.out.Extensions, "protobuf:file", raw)
	}
}

// fileOptionsRaw renders the file's syntax, edition, and set standard options
// into deterministic JSON.
func (l *lowerer) fileOptionsRaw() ir.RawValue {
	fields := map[string]any{"syntax": l.file.Syntax().String()}
	fdp := protoutil.ProtoFromFileDescriptor(l.file)
	if l.file.Syntax() == protoreflect.Editions {
		fields["edition"] = fdp.GetEdition().String()
	}
	if o := fdp.GetOptions(); o != nil {
		putStr(fields, "goPackage", o.GetGoPackage())
		putStr(fields, "javaPackage", o.GetJavaPackage())
		putStr(fields, "javaOuterClassname", o.GetJavaOuterClassname())
		putStr(fields, "csharpNamespace", o.GetCsharpNamespace())
		putStr(fields, "objcClassPrefix", o.GetObjcClassPrefix())
		putStr(fields, "phpNamespace", o.GetPhpNamespace())
		putStr(fields, "rubyPackage", o.GetRubyPackage())
		if o.GetJavaMultipleFiles() {
			fields["javaMultipleFiles"] = true
		}
		if o.GetDeprecated() {
			fields["deprecated"] = true
		}
	}
	b, _ := json.Marshal(fields) // a map of strings and bools always marshals
	return ir.RawValue(b)
}

// putStr sets key to v in fields when v is non-empty.
func putStr(fields map[string]any, key, v string) {
	if v != "" {
		fields[key] = v
	}
}
