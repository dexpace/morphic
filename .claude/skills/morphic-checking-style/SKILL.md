---
name: morphic-checking-style
description: Use when checking Go in this repository against the org styleguide, in a review's written-rules pass, or when a styleguide rule or the org's go-styleguide skill seems to disagree with this repository.
---

# Checking morphic Go against the styleguide

`CLAUDE.md` makes the guide binding and restates the rules most likely to bite, and `make gate`
holds what a tool can state. Most of the guide's rules name review in their **Enforcement** line,
many of them nothing else. This skill is that review.

It reads the guide's chapters, not the org's go-styleguide skill, which this repository does not
install. `.gitignore` keeps everything under `.claude` but the skills out of a clone, so a clone
could not declare that plugin, and its digest tells an agent the opposite of this repository in
places (`references/departures.md`).

## Check a change

1. Take the chapters the change calls for from the table, and list their rules:

   ```bash
   gh api repos/dexpace/styleguide/contents/go --jq '.[].name'
   gh api -H 'Accept: application/vnd.github.raw' repos/dexpace/styleguide/contents/go/<file> | grep '^### '
   ```

2. Hold each line the change adds to each rule, and to `CLAUDE.md`'s Go style section and
   `CONTRIBUTING.md`, which add rules of their own. Skip what `.golangci.yml` or `internal/archtest`
   states, which `make gate` holds.
3. Where a line looks like a breach, read that rule to its **Enforcement** line: a rule's exception
   is in its body, never its heading.
4. Look the rule up in `references/departures.md`. A departure is followed, not reported.
5. A finding names the rule's number, the file and line, and what the rule says, ranked as
   `morphic-reviewing-prs` ranks one.

A line the change leaves alone is no finding here; a breach on one is filed by `CLAUDE.md`'s "Filing
issues". A breach a tool could catch is a check to file, by `morphic-curating-skills`.

| The change adds | Chapters |
|---|---|
| any Go | 01 formatting, 02 naming, 03 errors, 05 API design, 11 documentation, 12 variables |
| a test | 06 testing |
| a goroutine, a channel or a lock | 04 concurrency |
| a package or a file | 07 packages |
| a log call | 08 logging |
| JSON, or a time or a date | 09 serialization |
| a file, a network read or a buffer | 10 resources |
| work on a path every input takes | 13 performance |

## References

- `references/departures.md`: where this repository departs from the guide or the org's skill,
  what holds each difference, and the rules the tree still breaks.
