# Ground truth for #566: fix(compilers/openapi): read a $defs pointer from its own schema

Initial commit reviewed: 3515599f43363a39f59434c5809c4ce47f5512a7

Each item below is a commit pushed after the initial one. Most fix a defect a local review found in the initial change; some are planned continuation work and are NOT defects -- judge each.

## fafdb087
fix(compilers/openapi): refuse cycles the resolver would enter

Some documents make the vendored schema resolver recurse forever and
crash the process with an unrecoverable stack overflow before the
compiler can turn anything into a diagnostic. The existing pre-parse
pointer-chain scan only understands ordinary same-document $ref
pointers; it has no model of $anchor, $id, or $defs-relative
resolution, so a cycle built from those still reaches the parser
unguarded (GitHub #526, #546).

An exact model of the resolver is not reachable from a static read
of the document. The resolver re-registers a schema under a fresh
base as it resolves each reference, which document a $defs-relative
pointer resolves against depends on the chain that reached it and is
cached for every later reference spelling the same pointer, and a
duplicate anchor declaration resolves by registration order. All
three make a crash depend on which order the document's components
happen to be declared in: the same two schemas can crash the
resolver in one order and resolve cleanly in the other. A model
precise enough to reproduce that would have to track the resolver's
own internal state by hand and would still only be right for the
resolver version it was built against.

This instead over-approximates. For every schema reference it takes
every node the resolver could conceivably land on under any
anchor/id/defs state, and refuses the document if any choice among
those targets closes a cycle. Because it never commits to one
resolver state, the verdict does not depend on declaration order.

The cost is measured directly rather than assumed. Across the JSON
Schema Test Suite's own draft2020-12 groups, each embedded four
different ways, the check refuses nothing: 0 false refusals out of
1736 documents. Against adversarial fuzzing of $ref/$anchor/$id/$defs
combinations built to stress this exact mechanism, it refuses
roughly 9% of the documents the real resolver would have accepted,
concentrated in documents mixing $defs pointers with bare "#"/"#/"
references; every crash the fuzzer found was refused, and none were
missed.

The right long-term fix is for the compiler to stop delegating
same-document schema $ref resolution to a stateful third-party
resolver and walk it directly, bounded and order-independent by
construction, which removes this cost rather than only bounding it.
That is a larger change filed separately as a follow-up.

## 750d27db
fix(compilers/openapi): read a $defs pointer from its own schema

Two component schemas that each $ref their own `#/$defs/n` collapsed
onto one node, `t/anon/$defs/n`, a position the document does not
have. One definition was dropped, and the other schema's property was
typed with it, with no diagnostic (GitHub #557).

Probed against speakeasy-api/openapi v1.25.2 directly: for a reference
it meets first, the resolver reads `#/$defs/X` relative to the schema
that spells it - that schema's own `$defs` when it carries an `$id`,
else the nearest ancestor from the reference's own parent up, never
the OpenAPI root. But it caches the first definition it finds for a
pointer and hands it to every later reference spelling the same
pointer, in any schema, and it resolves a reference reached through
another against that other's definitions. Both make its answer depend
on declaration order.

The fix follows the resolver's own rule, per reference, in its own
place, instead of following its cache. internal/defs holds the rule,
built from the library's own navigation primitives. Load holds every
`#/$defs/...` reference out of the resolver's pass and hands each one
to the resolver as the pointer of the definition the rule names, so
every reader of the resolved model - nullability, docs, multipart
parts, allOf targets, discriminator mappings - sees the same
definition. IDs and provenance come from where the definition is
written, never from the pointer the reference spells.

This reconciles with the reference-chain cycle check added for #526
and #546 (#563): the resolver no longer runs its own `$defs` lookup,
so the check's `$defs` edge is now the exact definition the rule
names for each reference, not every node holding the same path. The
drift that check modeled - the library resolving a pointer against a
document other than its own - no longer happens, so that code is
gone. The $anchor/$id half of the check is unchanged. Two of its
false-refusal rows, both caused by the old every-node-holding-the-path
edge, are now accepted; every crash in its table is still refused.

The fuzz regression's oracle changes with it. With $defs references
held out of the resolver's own lookup, the raw library's crash set no
longer describes what this pipeline does, so runReachFuzzOracle now
runs this package's own Load with the chain check disabled rather
than the raw library, and the golden was regenerated against it: 25
of the 200 generated documents crash the pipeline, and all 25 are
refused.

Measured, before (raw library) versus after (this pipeline), refused
out of each corpus: JSON Schema Test Suite (1736), 0 to 0; committed
grammar (200), 54 to 49; fuzz seed 7 (4000), 1125 to 1076; fuzz seed
11 (4000), 1233 to 1017. Fewer documents are refused everywhere, and
none newly. Against the raw library the false-refusal rate falls on
every corpus; against the pipeline that actually runs, what remains
is the registry over-approximation the $anchor/$id half already
accepts.

Known limits: an ancestor that is a raw node - an extension the
library only unmarshals as a standalone schema once something
resolves into it - is not searched, and a $defs pointer spelled with
a document part stays unresolved.

## 574b2302
chore: merge main into fix/openapi-defs-relative-identity

The branch was cut on the first commits of #541 and #563, which landed on
main as squashes, so every file they touched conflicted. Main's side is
authoritative for those; only this branch's own change needed resolving,
against what main has since done to the same code.

- load: main's resolver pass (#562, #565) reports each failure and finding
  at the $ref that drew it. A held "#/$defs/..." reference now goes through
  that pass's own reporting, and is held out again on the document
  resolveExternal rebuilds.
- reach: #563's reference-chain check was reworked into shared classes and
  position sets. The exact edge for a held reference is a class of its own
  there. The drift model stays, seeded only from the $defs pointers the
  resolver still reads itself (in an extension, or with a document part),
  which a document from the fuzz sweep showed it still needs.
- discriminator mapping: main (#564) hoists a mapping target in either
  declaration order. A "#/$defs/..." mapping value is read from its
  discriminator, then meets a $ref in resolvePointer.
- doc comments brought under the 100-word cap, and the fuzz oracle's
  chain-check seam restored, since main's #563 dropped it.

## 890493ac
fix(compilers/openapi): bound $defs lookups and report missing targets

A "#/$defs/..." reference that no definition answers for was left to the
lowering to report, and the lowering reaches only the positions it
models. Under "not", or any construct kept verbatim, a dangling
reference that main reports as an error went unreported. resolveHeld now
reports it at the reference in the resolver's wording, and the compiler
keeps that report where the lowering makes the same one.

The rule found a definition by navigating each ancestor of the reference
from the document root, once per ancestor per reference, in load, in the
reference-chain check and in the lowering: cubic in how deep a schema
nests. A document 200 levels deep with a reference at every level took
43s where main takes 0.08s. defs.Reader reads each position once, from
its parent, and remembers which ancestor answers a pointer, so the work
is linear in the document. The same document now takes 0.09s, and a
flow-style one nested 4000 deep takes 5.4s against main's 3.1s. load,
the reach check and the lowering context each read through one reader.

Also:

- the local $id rule's second alternative, a schema inside an $id
  resource reading its own $defs first, had no test; one now pins it
- a test compares the rule with the resolver's own reading of the first
  reference it meets, over generated documents, so a change to the
  library that moves the resolver's reading fails there
- fragmentOf is checked for every byte value at the start, middle and
  end of a key, and no longer reimplements strings.IndexByte
- MappingPointer refuses a nil discriminator instead of faulting
- comments that misdescribed document-part references, and labels left
  over from probing, are corrected; defsRefs is now heldRefs, so the
  name no longer means two things in package load

## 3db5b01b
fix(compilers/openapi): keep the $defs reader's memory per position

defs.Reader remembered which ancestor answered each pointer, one entry
per position per distinct pointer asked. A document nested d deep whose
references each spell a different pointer made that O(d^2) in memory: a
370 KB document nested 4000 deep took 4.7 GB and 195s.

Only an object with a $defs of its own can answer a pointer, so the
reader now remembers, for each position, the object, whether it has a
$defs of its own, and the closest ancestor that does. A reference probes
only those holders, so the memory is O(positions) and the cost of a
reference no longer depends on what it spells. The same document takes
971 MB and 6.8s; one where every level reuses a pointer is unchanged.

The parity grammar now also draws references that are a schema keyword's
own value (items, not), where the reference's parent is the holder
itself. A mutation that skipped the starting ancestor had survived
without them, so a unit test pins that shape too.

## 5d9fb192
chore: merge main into fix/openapi-defs-relative-identity

main gained #567, which validates the objects a reference brings in from
another document, and gave eachReference a parameter naming what was
running when it recovers from a panic. Only references.go conflicted.

- references: the resolution pass keeps its visit method, and takes
  main's eachReference signature. Both the walk and resolveHeld name
  "reference resolver" through one recovered helper, which now takes the
  name as eachReference does, so a panic in either reads the same.
- reached validation reads each reference's resolution trail after the
  last resolution. A held "#/$defs/..." reference whose definition leads
  into another document is therefore validated once, at the least $ref
  that reaches it, as any other reference is; compiling one against main
  gives the same finding at the same $ref.

## 6e1a8ff4
refactor(compilers/openapi): simplify the $defs reader and its callers

No behaviour change.

- defs.PointerOf is the one statement of which "#/$defs/..." references
  the resolver reads relative to their schema. load holds out exactly
  those and the lowering reads exactly those; each had spelled the
  document-part test for itself.
- defs.Reader.MappingTarget takes the discriminator, so the reader owns
  the lookup of where an object sits in its document, as Target does.
  TargetFrom is unexported and its guards, which both callers repeated,
  are gone.
- The search is one loop over the holders rather than a first step
  followed by the same loop, and the read of one position is its own
  method.
- heldRef gains hold, restore and retarget, and resolution a missing
  report, so resolveHeld reads as the two passes it is.
- Tests use openapitest.DiagMessageAt instead of filtering by hand, and
  name the method they exercise.

The reader's answers are unchanged: they match the original search on
about 550000 generated queries, shuffled and including references that
are a schema keyword's own value, and the parity test still compares
them with the resolver's own reading.

## f858a05c
test(compilers/openapi): cover $defs in operations and callbacks

Every $defs test lived under components. The rule also reads references
in operation, parameter, header, callback and webhook schemas, whose
pointers carry escaped path keys, and a component a callback reaches
through another $ref.

The new corpus file defs_in_operations.yaml holds two operations'
definitions of one name, a path and a definition key that need '~', '/',
braces, a space, '%41' and '+', and such a component, so the harness
sweep and its order-invariance oracle reach them. main reports a false
unresolved reference on the component and merges the operations'
definitions into one node. A compiler-level test asserts that each
lowers at the pointer that names it; leaving '%' or '+' out of the
encoded fragment, or holding nothing out of the resolver, reddens it.

The resolution drift test compares resolveWith with the library over the
whole corpus. The component the library cannot resolve is the case the
rule exists for, so the test names it and requires the library to still
fail there, which keeps the entry from outliving its reason.

## 02c3e00d
fix(compilers/openapi): index positions instead of searching per object

The rule reads a reference from where its schema sits in the document,
and asked the library's GetJSONPointer for it, which searches the whole
YAML tree for the node. That is a walk of the document per reference, so
a document with thousands of $defs references cost the square of its
size: 5000 components that each hold a $defs and reference it took 17s
where main takes 1s.

positions walks the tree once, on the first question, and records where
each container is first met in the order that search visits them,
following aliases and skipping an entry whose key is empty or no scalar,
as it does. A pointer is then the chain of tokens back to the root. The
same documents take 1.7s, and 10000 and 20000 components take 3.5s and
7.0s.

The answers are the library's. The committed test compares every schema
and discriminator of documents with aliases, merge keys, escaped and
empty keys and sequences. A throwaway sweep over about 330000 generated
objects, a third of them placed by an alias or a merge, found no
difference, and each rule the index encodes has a mutant that reddens
it.

## 1f296071
test(compilers/openapi): pin $defs in aliased and merged schemas

A YAML alias or merge key reuses the anchor's own nodes, and the library
places an object it meets twice where it was first written. A reference
in an aliased schema therefore reads the anchor's $defs: B, an alias of
A, reads A's, and Child, which merges Base, reads Base's. main merged
every such definition into one node and lost the others; nothing here
said what the rule should do instead.

defs_alias_and_merge.yaml joins the corpus, so the harness sweep reaches
it, and a compiler-level test asserts each reading, with a schema that
only resembles another keeping its own. Holding nothing out of the
resolver reddens it. The position index agrees with the library on the
tree the loader builds, anchors released, as it does on the parsed one;
a throwaway sweep over about 366000 generated objects found no
difference.

positions.go reads a document node's content from the node it resolved
rather than from the one it was given.
