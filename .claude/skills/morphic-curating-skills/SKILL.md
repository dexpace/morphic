---
name: morphic-curating-skills
description: Use when work in this repository taught something worth keeping (a defect found late, a check that passed without reaching its target, a correction from a maintainer), or before adding to, editing or deleting anything under .claude/skills.
---

# Curating the morphic skills

Each lesson is kept once, in the strongest form it can take, where the next person will be when it
applies.

## Where a lesson goes

Take the first that fits.

1. **A check.** If a test or a lint or archtest rule can catch it, write or file that check. A
   filed check keeps a rule naming its issue until it lands.
2. **A script.** Commands that went wrong when typed by hand go in a skill's `scripts/`.
3. **A rule in a skill.** Add it to a reference file of the narrowest skill whose trigger fires
   when the lesson applies. If none does, widen a description or add a skill: a rule no trigger
   reaches is never read.
4. **Not in the repository.** A fact about one machine, shell or person stays out of
   `.claude/skills`.

A skill links to a normative fact in `docs/`, such as a `docs/parser-library.md` or
`docs/compilers/openapi.md` entry, and never restates it. List the skills with
`grep -h '^description:' .claude/skills/*/SKILL.md`; a hand list here would drift.

## Rule format

```markdown
## <the rule in one line>
`<stable-key>`

- **Rule.** What to do, stated so it can be checked.
- **Why.** The failure it prevents, citing the PR or issue that showed it (#N).
- **Probe.** The command or planted defect that shows whether it holds, unless the Rule is one.
```

A rule stays within 150 words, or becomes two. No narrative, dates or first person, and no count a
command beside it does not derive, bar a Why's evidence, which its citation pins. Cite a rule as
`references/<file>.md#<key>`, from another skill by its full path; the lint holds each to its file.

## Limits

- A `SKILL.md` body stays within 500 words. Its description starts "Use when" and says when to load
  the skill, never what the skill does: an agent follows a summary instead of reading the body.
- A reference file stays within 4,000 words. A full file takes a new rule only by merging or
  removing one.
- Everything works from a fresh clone and names no machine. Paths start at the repository root,
  bar those inside the skill's own folder, and commands run from the root.
- Scripts are Python 3.9 with the standard library only, each with a `test_<name>.py` beside it.
  Run a skill's tests with `python3 -m unittest discover -s .claude/skills/<skill>/scripts`.

## Upkeep

- Run `python3 .claude/skills/morphic-curating-skills/scripts/skills_lint.py` after every edit: it
  checks the frontmatter, limits, rule format and that every name a skill uses exists. The gate runs
  neither it nor the scripts' tests; `preflight.py` and `review_setup.py` run it on each branch
  they check, code changes included.
- After writing or changing a rule's Why, run
  `python3 .claude/skills/morphic-curating-skills/scripts/citations.py <key>` and read each
  citation it lists against its item.
- A lesson a skill already holds that failed again means the skill failed: sharpen its wording or
  its trigger, not add a second copy.
- Skill changes ship like code, titled like `chore(skills): <subject>`.
