# IR version

Part of the [IR design](./README.md). The schema stamp a document carries, the compatibility policy
around it, and the names it covers. What each past generation changed is in the [version
history](./version-history.md).

`IRVersion` names the generation of the IR *schema* — the shape of the document itself, not the
API it describes (that is `Version`) and not the commit that produced it. It is the one claim in a
document that a reader cannot recompute from the contents: everything else about a document can be
checked against the document, but which spelling of the schema its keys are in has to be declared.

**What moves it.** Any change to the JSON shape of a `Document`: a key added, renamed or removed, an
encoding changed, or the meaning of an existing key changed. An addition counts, because the decoder
refuses a member it does not know, so a build at the old version cannot read the new document. A
line of work that changes the shape several times bumps it once, where it lands on `main` — a
version that moves within an unmerged branch tells a consumer nothing. `ir.IRVersion` is the
constant; the [version history](./version-history.md) records what each past bump changed.

**A change to a name moves it too.** An emitter renders an identifier from `Naming.Canonical`, or
from `Naming.Hint` where the source wrote no name, so changing how either is derived renames every
symbol it touches in every SDK generated after it, with no key of the document moved. That is a
breaking change to the IR's contract, versioned like a change of shape, whichever of these it comes
from: the [word grammar](./naming.md#the-word-grammar), the Unicode tables the grammar reads, which a
Go release can move, or how a compiler composes a hint from a position. Such a bump keeps the shape,
so its row states the meaning that changed.

**What a bump implies.** Pre-1.0 (`0.MINOR.PATCH`), MINOR is the breaking position and every bump
so far has been breaking. There is no non-breaking bump in the history and nothing distinguishes
one, so PATCH carries no promise a consumer may read compatibility into. Moving off `0.` is a
decision about the project's stability rather than about any one shape change, and no policy for
MAJOR is written here until that decision is taken.

- *Compilers* stamp `ir.IRVersion` on every document they produce, and declare beside it the
  namespaces their IDs live in ([identity](./identity.md)). Of the version itself that is the whole
  obligation: a compiler never emits an older generation, and there is no option to ask it to.
- *Emitters* and any other consumer are built against exactly one generation. A bump is a change
  they must be updated for; there is no "read it anyway" mode, because the failure a stale
  consumer produces is silent — it finds no key it recognizes where a renamed one used to be and
  drops the construct rather than reporting it.

**What a consumer does on mismatch: refuse.** `ir.CompatibleVersion(v)` is the predicate, and it is
exact equality with the `ir.IRVersion` the consumer was compiled against. A differing patch, a
prerelease suffix, and a value that is not a version at all are all equally unreadable; accepting a
neighbouring version would mean claiming to know what changed between them, which is the knowledge
a version exists because nobody has. Morphic ships **no migration path** between generations — a
document written by another generation is re-compiled from its source spec, not converted.

**Where it is enforced.** In two places, one for each way a document reaches a consumer. Decoding
one from JSON refuses it outright: `ir.Document`'s decoder reads `irVersion` before any other member
and fails with `ir.ErrVersionAbsent` or `ir.ErrVersionIncompatible`, so another generation's
document is reported as one rather than as whichever renamed key it trips over first. The same
decoder refuses a member the schema does not define, a duplicate name and invalid UTF-8, whatever
options the caller passed, since each is a way for a document to be read differently from how it
was written. A document built in memory never meets that decoder, so `irverify`, the gate every
consumer runs before trusting a document, checks the stamp too. Two codes, because the two failures
name different writers: `ir/ir-version-absent` for a document carrying no stamp — a producer that
forgot, and the failure `omitempty` hides best, since a document without the key is byte-identical
to one that never had it — and `ir/ir-version-incompatible` for a stamp this build does not read.

**How it is held to the shape.** `TestIRVersion_PinsTheEncodedShape` (`ir/irversion_test.go`)
describes every type a `Document` reaches through `internal/jsonshape`: each member's JSON name, Go
type and tag options, the methods the encoder calls in place of reading fields, the constants each
type declares, and each sealed sum's variants with their tags. A name is in the description where
it gives a value its meaning: a member's Go type, so retyping a string member to an ID type moves
the description, and a numeric constant's name, since nothing else says what its number
means. A field's Go name and a string constant's are left out, because the JSON name and the
string say it themselves. The test requires the description's SHA-256 to be the one its
`generations` table records for `IRVersion`, so a shape change at an unmoved version fails. So
does a bump that changes no shape, unless its row's `meaning` says what changed: a change of
meaning, such as a lowering that starts filling a member, is the one reason to bump that the
description cannot see. The description reads declarations, so what a codec method's body writes
is outside it, left to the JSON contract tests in package `ir`. A type
from another package, such as `jsontext.Value`, is described by its name, underlying type and codec
methods, so a Go release that changes how one of them encodes moves the description too.

**How it is held to the names.** `TestIRVersion_PinsTheNames` (`ir/irversion_names_test.go`)
describes them in `ir/testdata/names.golden.txt` and requires the file's SHA-256 to be the `names`
its `generations` row records for `IRVersion`, so a name changed at an unmoved version fails and a
reviewer reads it as that file's diff. The description holds `unicode.Version`, the canonical form of
every spelling the grammar's conformance table names, that of every name of up to three runes over
one rune of each class the Unicode tables tell apart, and the hint of every `Naming` in
`testdata/golden/openapi/hint_positions.golden.json`. Three runes are enough because the grammar
decides at a rune from that rune and its two neighbours, so those names reach every decision it
makes over those classes. A hint has no such closure, since it is composed per compiler from
whatever position the compiler names: the pin reaches the positions `hint_positions.yaml` writes,
all those one document can, and a position it does not write, such as a document a `$ref` reads
whole, shows only in the corpus goldens, as the one bump no test requires.

**Bumping.** A bump sets `IRVersion` to the next MINOR, appends the row the failing test prints,
and adds an entry to the [history](./version-history.md), which
`TestIRVersion_HistoryRecordsEachGeneration` requires to end at `IRVersion`. The descriptions
themselves are `ir/testdata/shape.golden.txt` and `ir/testdata/names.golden.txt`, each rewritten with
`go test ./ir -run TestIRVersion -update`, after `go test ./compilers/openapi/e2e -run
TestGolden_HintPositions -update` where a hint moved, so a reviewer reads the change as their diff.
No other golden moves. `TestIRVersion_PinsTheNames` prints the row's `names` once the row exists.
Adding a position to `hint_positions.yaml` moves that hash as a renamed one does, so it lands with
a bump. A row on `main` is never edited: two branches that claim one version append their rows in
the same place, so whichever merges second meets the other's in a conflict and takes the next
version. A change to how `jsonshape` spells a shape moves `jsonshape.Format` and appends a row
describing the current generation anew, with no bump.

**Goldens store no stamp.** `irtest.WriteGolden` and `irtest.CompareGolden` refuse a document not
stamped with `ir.IRVersion`, then encode it without the stamp, and `irtest.ReadGolden` puts it back.
Every golden still asserts the stamp, and a bump rewrites none of them. Confirm rather than trust;
the second count is zero:

```bash
ls testdata/*/openapi/*.golden.json | wc -l
grep -l '"irVersion"' testdata/*/openapi/*.golden.json | wc -l
```
