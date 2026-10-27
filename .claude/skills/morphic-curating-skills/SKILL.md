---
name: morphic-curating-skills
description: Use when work in this repository taught something worth keeping (a defect found late, a check that passed without reaching its target, a correction from a maintainer), or before adding to, editing or deleting anything under .claude/skills.
---

# Curating the morphic skills

Each lesson past work here learned is kept once, in the strongest form it can take, where the next
person will be when it applies.

## Where a lesson goes

Take the first that fits.

1. **A check.** If a test, a lint rule or an archtest rule can catch it, write or file that check.
   Prose is the weakest home.
2. **A script.** If it is a sequence of commands that went wrong when typed by hand, put it in a
   skill's `scripts/`.
3. **A rule in a skill.** Add it to a reference file of the narrowest skill whose trigger covers
   the moment the lesson applies.
4. **Not in the repository.** A fact about one machine, one shell or one person's habits does not
   belong under `.claude/skills`.

A normative fact already has a home in `docs/`: a parser-library behaviour in
`docs/parser-library.md` with its canary, a lowering rule in `docs/compilers/openapi.md`. A skill
links to that entry and never restates it.

| The lesson is about | Skill |
|---|---|
| proving a claim, tests, mutation, differentials | `morphic-verifying-changes` |
| reviewing a pull request | `morphic-reviewing-prs` |
| the OpenAPI compiler, its corpus or its keyed docs | `morphic-openapi-compiler` |
| keeping the skills | this one |

## Rule format

```markdown
## <the rule, imperative, one line>
`<stable-key>`

- **Rule.** What to do, stated so it can be checked.
- **Why.** The failure it prevents, with the PR or issue that showed it.
- **Probe.** The command or planted defect that shows whether it holds.
```

Keep a rule under 150 words. No narrative, no dates, no first person, and no count that a command
beside it does not derive.

## Limits

- A `SKILL.md` body stays under 500 words. Its description starts "Use when" and says when to load
  the skill, never what the skill does: an agent follows a summary instead of reading the body.
- A reference file stays under 4,000 words. A full file takes a new rule only by merging or
  removing one.
- Everything works from a fresh clone. Paths are relative to the repository root and nothing
  names one machine.
- Scripts are Python 3.9 with the standard library only, each with a `test_<name>.py` beside it.
  Run a skill's tests with `python3 -m unittest discover -s .claude/skills/<skill>/scripts`.

## Upkeep

- Run `python3 .claude/skills/morphic-curating-skills/scripts/skills_lint.py` after every edit. It
  checks the limits, that everything a skill names exists, and the scripts' form. The skills are
  kept apart from the codebase, so the gate never runs this or the scripts' tests.
- When the lint reports a name that no longer exists, update or delete the rule; lines quoting
  history (`- **Seen in`) are exempt.
- A lesson a skill already holds that failed again means the skill failed: sharpen its wording or
  its trigger rather than adding a second copy.
- Skill changes ship like code: a branch, a PR, a title such as `chore(skills): <subject>`.
