# Ground truth for #273: fix(compilers/openapi): drop and site broken security references

Initial commit reviewed: 050344d3b4e39e237fd4e6527c48263d2af49a2d

Each item below is a commit pushed after the initial one. Most fix a defect a local review found in the initial change; some are planned continuation work and are NOT defects -- judge each.

## db7c125c
fix(compilers/openapi): clarify auth.go doc comment wording

Follow-up polish on the previous commit: tighten two doc comments left
awkwardly worded after the security-requirement fix.

## a6144cfb
test(compilers/openapi): pin the security diagnostic's own declaration site

The requirement pointer this change introduced was only ever asserted for
an option at index 0 of an inline path operation, and both halves of the
pointer are load-bearing. Spelling every option's index as 0, or basing an
operation's pointer on its mount rather than its declaration, left the whole
suite green while sending a reader to a requirement that is perfectly valid
— or, for a $ref'd path item, to a node that addresses nothing at all.

Pin the broken option's own index on the partial-drop case, and add a case
covering the carriers an inline path operation cannot stand in for: a
webhook, a callback operation, and a $ref'd path item, whose mount and
declaration differ.

Also write down what collapsing a fully dropped list to nil costs. A carrier
whose every option drops becomes indistinguishable from one that never
declared security, so an operation reads as requiring the service default's
scheme — one it never named — or as unauthenticated where there is no
default. nil is still the right spelling, being the only one that never
reduces a demanded requirement to explicitly public, but the residue belongs
next to the decision rather than in a reviewer's head.

## 23dcce82
test(compilers/openapi): report every undeclared scheme an option names

lowerSecurityRequirement documents that a multi-member option reports each
of its bad names, but nothing held it to that: stopping at the first bad
member drops the same option and collapses the same list, so the compiled
Auth is identical either way and only the diagnostics tell them apart.
Replacing continue with break left the whole suite green.

Pin it with an option naming two undeclared schemes, asserting both
messages in source order and the pointer they share.

Also record at LowerSecurityRequirements that a non-object security entry
arrives as an empty option rather than a nil one, so it still lowers to
the empty-option encoding. That sibling is issue #284's, and saying so
here keeps the next reader off the nil guard, which is not its site.

## 72af7d31
docs(compilers/openapi): name the guard the security note points away from

## a913b4ad
fix(compilers/openapi): site the security scheme a $ref cannot reach

A components.securitySchemes entry whose $ref resolves to nothing was
skipped without a word. The load phase does report the resolution failure,
but with no pointer at all (#235), and the only sited report came from a
requirement naming the scheme — which nothing has to do. An unreferenced
entry was therefore a scheme the document declares, the IR drops, and no
diagnostic places.

Report it where it is written, at /components/securitySchemes/<name>.

That exposed a contradiction between the two reports. A requirement naming
such a scheme was told it "references undeclared scheme", while the entry
beside it says the document declares one whose $ref is broken — sending a
reader to add a declaration already there. Both now say the name resolves
to no scheme, which is true whether the entry was written or not.

## bc191ebc
fix(compilers/openapi): report only the scheme entry nothing else places

The report added for an unresolvable securitySchemes entry claimed more
than it could know. Two kinds of entry lower to no scheme: one whose $ref
resolves to nothing, and one written as something other than an object.
Both were told their $ref resolved to nothing, which for a null or a
scalar names a reference the document never wrote.

Only the first is unplaced, and only it is reported here. An entry that is
not an object already draws the loader's type-mismatch, which names the
entry and the fault, so a second report sends the reader to the same
position to learn less.

The message now names the failing reference as well as the scheme, so it
stands on its own once the two are separated.
