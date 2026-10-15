# Ground truth for #564: fix(compilers/openapi): resolve a mapping to an inline subtype

Initial commit reviewed: ebaa7d227361d2201fd1e29d4c135597097c3942

Each item below is a commit pushed after the initial one. Most fix a defect a local review found in the initial change; some are planned continuation work and are NOT defects -- judge each.

## ff688ad1
fix(compilers/openapi): let a declaration name what a $ref built

A $ref from one path into another path's schema interns the subtree
beneath its target when the referencing path lowers first. The
declaration renamed the target's own node when it arrived, but Intern
returned early there, so the nodes beneath kept hints the reference had
composed from its guess at the target's. Under /paths the pointer
cannot replay the declaration's hint, which comes from an operationId
or a response, so those names depended on declaration order (#529).

compile.Types now records which coordinates a reference built, and
InternDeclared builds them again, under the same IDs, when the
declaration that owns them arrives. The rebuild descends through every
child the reference interned, so the whole subtree takes the
declaration's names. The reference still interns the node while it
lowers, so anything reading the target at that point finds it in
either order.

A position no declaration lowers, such as one beneath not, has no
declaration to settle its name, and two references into one such
region named it by whichever lowered first. positionHint now replays
the pointer outside /components/schemas as well, resetting at every
token it does not know, so that name depends on the pointer alone.

## 9dfbd6df
fix(compilers/openapi): resolve a mapping to an inline subtype

A discriminator mapping may name its subtype by a JSON reference to an
inline position. The mapping value is never followed by the resolver,
so the compiler looked its target up among the nodes already interned,
and found one only if the schema enclosing the subtype had lowered
before the base. Declared the other way round, the entry was dropped as
unresolved and the compile failed (#530). A 3.2 defaultMapping was
resolved the same way.

A mapping target is now resolved the way a $ref is: a declared name or
component by ID, an interned node when one exists, and otherwise the
schema its pointer addresses, found as the resolver finds a $ref's
target and hoisted by the schema walk. Both orders resolve to the same
pointer-derived ID, and the hoisted node is named provisionally, like
any reference's, until its declaration names it.

A mapping to an existing sub-schema that is not a subtype, such as a
string property, now resolves too. pass.Validate reports it as
pass/discriminator-missing-variant, in place of the unresolved-ref
error that said the schema did not exist.

## 83d2dce1
test(compilers/openapi): expect what main now reports for a mapping

Two expectations in the mapping tests predate changes that landed on
main after this branch was cut, and the merge left them failing.

#531 gave an inline subtype no implicit tag, so a subtype that only a
defaultMapping names has an empty DiscriminatorValue, not the last token
of its pointer. #549 moved a pass finding's locator from
Provenance.Pointer to Provenance.Node, so the alias test now finds the
missing-variant finding by its node.

## da5fd2a4
test(compilers/openapi): pin every position DeclaredAt refuses

The pointer walk returns a typed nil for a keyword a schema leaves
unset, so DeclaredAt reports ok=false there only because it checks the
schema it found. No test reached that check: dropping it left the suite
green, though /components/schemas/Pet/not then came back as a nil schema
with ok=true.

The test now reads a parsed document and covers that position, plus raw
YAML under an extension key, which the parsed model holds as a node
rather than a schema.

## ec6a1f9a
docs(compilers/openapi): state what a mapping still shares with a $ref

Resolving a mapping target by hoisting its position makes it a $ref in
all but syntax, limits included. resolveMappingTarget now says so, and
names #749: a target whose body leads back above it crashes as the same
$ref would. DeclaredAt names #757: a schema held as raw YAML under an
extension key is reached only once a $ref has interned it.

The redundant component-pointer check in resolveMappingTarget gets its
reason, and the mapping and defaultMapping docs agree on when an entry
is dropped. A test comment that declared Kennel first because a mapping
found an inline target only once it was interned is now wrong, and goes.

## e9b161d9
test(harness): sweep a mapping to inline targets in both orders

The unit tests diff the type registry across two declaration orders for
one mapping at a time. Nothing in the corpus maps a tag to an inline
position, so the sweep's order oracle never met the case.

The fixture is written base-first, the order that failed, and adds what
the unit tests do not cover: a subtype that is itself a discriminated
base, so one mapping's hoist hoists another; a base under not that only a
$ref lowers; and a target inside a webhook's request body. Each of its
three entries is unresolved on main, and reverting the hoist turns the
in-repo sweep red on it.

## c9bfdca9
test(compilers/openapi): pin the findings of a hoisted mapping target

Resolving a mapping target can lower it, and what that lowering reports
is appended to the discriminator's diagnostics. No test noticed if it
was dropped, for a mapping entry or a defaultMapping: where a
declaration lowers the target too, it reports the same findings again.

The new test aims both at positions under not and if, which no
declaration lowers, so the hoist is the only lowering that sees the
format each declares beside a model. Dropping either side's findings
now fails it.

## 208f0942
test(compilers/openapi): check that a refused mapping interns nothing

TestResolveMappingTarget_Branches said an external reference and an
undeclared component are refused before a pointer is built, but the
component's pointer is built; it also asserted only ok=false, so a
refusal that interned a node on the way would have passed. It now
checks that the registry is unchanged after every refusal, and its doc
says what it pins. These tests also pass TopLevelDepth, as their
neighbours do, instead of a bare 0.

## f1754f7e
docs(compilers/openapi): name the parsed document among resolve's inputs

DeclaredAt reads the parsed document, but four descriptions of what
reference resolution reads still listed only the document's path and
declared names: the resolve package doc, Scope's and RefScope's docs,
and the archtest comment that justifies each import resolve may make,
which did not mention jsonpointer. Each now names the document too, and
Scope.Doc says why it is typed any.

DeclaredAt's doc named only extension values as the raw YAML it does not
read as a schema. An enum member is held the same way, and a mapping to
one is likewise reached only once a $ref has interned it, so the doc
now states the class rather than one member of it (#757).

## e73f0da4
test(compilers/openapi): refuse an enum member in DeclaredAt's test

The pointer walk returns an enum member as a raw YAML node, as it does
an extension's value, so DeclaredAt refuses both. The test pinned only
the extension. It now pins the enum member as well, which is the other
case DeclaredAt's doc names.

## 890e2a4f
docs(compilers/openapi): link the alias mapping limit to its issue

TestDiscriminatorMapping_ToAnAliasIsReportedByValidation pins a mapping
to a pure-$ref position as a pass.Validate finding, a limit this branch
states but leaves in place. Its doc now names #758, which tracks
deciding between resolving such a mapping to the $ref's target and
reading through the alias in validation.

## bdc63a9b
fix(compilers/compile): report a node still being built as absent

Intern records a coordinate before calling build, so a self-reference
reached while building resolves and recursion stops, and it stores the
node only once build returns. In between, NodeAt reported the coordinate
as present with a nil node, a state its doc said could not occur. A
reader that trusted it dereferenced nil (#749).

NodeAt now reports that window as a miss, and Building reports whether
an ID is in it, so a reader that finds no node can tell a node its
builder will finish from one that will never exist. The rest of the
contract is unchanged: a coordinate whose build has returned resolves to
its node in one step.

## 5f214099
fix(compilers/openapi): leave a node still being built to its builder

A reference that hoists a position whose body refers back to a position
above it, one nothing has interned yet, lowers that ancestor and walks
down into the first position again while its build is still running.
Interning hands the revisit the ID, as it must for recursion to end, but
the code that runs after interning then read a node that did not exist.
attachDeclaredAnnotations crashed on it, and registeredNode reported a
broken invariant first. The compiler panicked on a valid document when
the reference lowered before the declaration (#749). A discriminator
mapping reached the same crash once it hoisted its target the way a $ref
does.

A revisit while a node is Building now leaves the node alone. The frame
building it is a lowering of the same declaration at the same position.
Once its build returns, it runs the same steps: the annotations, the
keywords with no home, and at an own-node position the residue. A
revisit can reach a node mid-build only through the structural walk of an
ancestor, never through a $ref, which resolves to the ID without
lowering anything. So what it skips is a subset of what the builder does.

The two-order test holds that claim at a property, at items (where the
residue is recorded), through a discriminator mapping, and beneath
/paths. Each row is written reference first, the order that crashed,
and the registry must equal the declaration-first one.

## 736f03d7
test(harness): sweep a $ref back into a node still being built

Nothing in the corpus refers back into a node while it is being built,
so the sweep never met the crash. The fixture is written reference
first, the order that crashed. It reaches a property, an array's items,
a discriminator mapping's subtype and a path's response item, each
declaring an annotation and a keyword with no home, so every reader
that runs after interning meets a node still being built. It panics on
main and passes every oracle here, the order oracle included.

## 44c584bd
test(compilers/openapi): pin each mid-build reader in a test of its own

One test drove both attachDeclaredAnnotations and
recordDeclarationResidue at a node still being built, under the first
one's name. It also asserted that the node build returned held no
description yet, which cannot fail: build makes that node fresh.

Each reader now has its own test. It requires the reader not to panic
mid-build and to report nothing there, then shows the reader doing its
work once the build has returned, so the fixture declares what the
reader reads. Reverting NodeAt's fix fails both. The shared setup is
lowererDeclaringS, named for what it returns like its neighbours.

## ff5df623
docs(compilers/openapi): rewrap registeredNode's doc comment

An earlier edit left one line of the comment at 98 columns.

## 83117f91
refactor(compilers/openapi): resolve a $ref and a mapping in one step

A mapping target is resolved as a $ref is, but the two ran as parallel
copies of the same steps. resolveMappingTarget called mappingTargetID,
then parsed the target's pointer a second time and re-checked the
component case, which needed a comment to explain why a guard that
could never fire was there.

resolvePointer now holds the steps both share once each has its pointer:
a component by its ID, an interned node by its own, and otherwise the
declared schema, hoisted. resolveSchemaRef and resolveMappingTarget call
it, differing only in how they find the declaration, so they cannot
drift apart.

DeclaredAt returns the schema or nil, as annotation.DeclaredSchema does,
so it fills the same slot. Returning a pointer also turns the typed nil
an unset keyword yields into a plain nil, so the guard against reporting
one as found is gone with it. The bare-name rule both mapping readers
apply is componentIDByName, one place for the empty-name case.

Behaviour is unchanged: every spec under testdata/ and every probe of
these paths compiles to byte-identical IR and diagnostics.

## cfaaf9e5
refactor(compilers/openapi): return nil from both registeredNode misses

One miss returned nil and the other the zero-valued td it had just failed to find. Both now say nil.

## 1b549290
test(compilers/openapi): drive the two-order tests from one table each

The mapping and the mid-build two-order tests built every row's pair of
documents before the table: a local componentPair in each, with the same
name and different shapes, a pathPair, and a dozen variables that only
fed the rows. The mapping test also carried two base builders that
differed in one line, and an anonID helper restating a convention the
package writes as literals.

Each row now carries its spec builder and its two halves, and the loop
composes both orders. The pointers are constants that the references
and the expected IDs share. The documents compiled are unchanged.
