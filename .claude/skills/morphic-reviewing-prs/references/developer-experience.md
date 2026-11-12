# Developer experience

The angle that reads the change as the next contributor meets it: whether its names, places,
comments and messages let a reader find, follow and extend it without asking. It loads
`morphic-checking-style`, which holds the Go the change adds to the styleguide.

1. **Names.** Each new identifier says what it holds or does, at a length in proportion to its
   scope; whether it matches its neighbours is Consistency's.
2. **Placement.** Each declaration sits in the package and file its responsibility names, never a
   `utils`, `helpers` or `common` package (`CLAUDE.md`'s Packages rule), and each test where
   `CONTRIBUTING.md`'s "Where a test goes" puts it.
3. **Reading.** Each function does one thing a reader can follow alone, and each comment says why,
   not what. Question any added line nobody can explain.
4. **Comments and documents.** Each comment the change falsifies is swept
   (`.claude/skills/morphic-verifying-changes/references/sweep-every-site.md#sweep-comments-a-behaviour-change-falsifies`),
   and each normative document and `CONTRIBUTING.md` recipe it touches changes with it
   (`.claude/skills/morphic-verifying-changes/references/sweep-every-site.md#update-normative-docs-in-same-change`).
   Moved code and divided documents take `references/developer-experience.md#moved-comments`, code
   crossing a package `references/developer-experience.md#move-keeps-what-the-package-enforced`,
   and a restructured document
   `references/developer-experience.md#restructure-accounts-for-every-sentence`.
5. **Messages.** Each new diagnostic or error names what is wrong, where, and what the writer can
   do.

The matrix is each new identifier, declaration, comment and message, by count, with the rule each
finding breaks.

## Moved code carries stale comments
`moved-comments`

- **Rule.** For a file split or a cross-package move, read each moved declaration's comment against
  its callers, and the `doc.go` of each package a declaration moves into against what arrives. Grep
  the whole repository for every moved name, and for the new package's name used as an ordinary
  word. A divided document takes `references/developer-experience.md#moved-prose`.
- **Why.** Byte-identical moves shipped false comments: 8 in #1064, 4 in #1073, 22 in #1099. A
  cross-package move broke comments in files it never touched (#1104). A move of a function and a
  variable into `internal/testspec` left its `doc.go` saying it holds only constants (#1540).
- **Probe.** `git diff --name-only --diff-filter=AMR "$main"...HEAD -- '*.go'` names each file
  that gained code, a move included; read the `doc.go` in each one's directory.

## A move across packages keeps what the old package enforced
`move-keeps-what-the-package-enforced`

- **Rule.** For each declaration crossing a package boundary, list what the old package's
  unexported fields and functions made impossible for code now sharing its package: each comment
  the move deletes or rewrites saying "only through", "private", "unexported" or "cannot" names
  one. Plant a write bypassing each; one no test refuses gets a test, or a type keeping it by
  construction.
- **Why.** #1650 moved `KeptOnce` into the package of its one writer, deleting a comment saying a
  lowering writes it only through `Once`; a write skipping it, and a fresh read set per operation,
  passed every test until a review planted past the defects the PR listed (#1651).
- **Probe.** `git diff "$main"...HEAD | grep -nE '^-[[:space:]]*//.*(only through|private|unexported|cannot)'`
  lists the deleted comments, each read beside its plant.

## Moved prose leans on what stayed behind
`moved-prose`

- **Rule.** After a document or table is divided across files, read each sentence and cell that
  leaned on a neighbour now elsewhere, each citation into a divided document for what it cites, and
  each rewrapped paragraph for a line opening with block markup. A comparison with the original
  cannot object: the text matches.
- **Why.** Kotlin's table kept cells reading "the same" of a Go column it had lost, citations
  outlived the passages they named, and a reflowed line rendered as a list, all past word and link
  checks (#1179, #1184).
- **Probe.** `git grep -nwE 'the same|there|too|also' -- <files>`, each section's first line for a
  pronoun, `git show <rev>:<cited file> | grep -c <name>` for each name cited, and
  `awk 'FNR==1{p="";c=0;f=($0=="---")} f{if(FNR>1&&$0=="---")f=0;next}/^\040{0,3}(\140{3}|~{3})/{c=!c;p="";next}c{next} p ~ /^[[:alpha:]]/ && /^ {0,3}([-*] |[0-9]+\. |>|#+ |-+ *$|=+ *$)/{print FILENAME":"FNR} {p=$0}' <files>`.
  `TestDocuments_OpenNoLineWithAPlus` holds `+`.

## A restructured document accounts for every sentence it held
`restructure-accounts-for-every-sentence`

- **Rule.** When a change restructures a document, such as a skill split into references, account
  for each sentence of the old text, the prose between rules as well as the rules: moved, reworded
  or dropped with a reason the PR states. A comparison of the rules alone passes a dropped sentence
  of prose.
- **Why.** The review skill's split moved its rules word for word, and a second round found the old
  `SKILL.md`'s sentence on deliberate oddities with no home (#1522 at a5cc10441).
- **Probe.** For each sentence of the old file, `git grep -F` a phrase of it in the new tree, and
  name each one missing.
