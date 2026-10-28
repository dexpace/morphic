---
name: morphic-curating-skills
description: Use when work in this repository taught something worth keeping (a defect found late, a check that passed without reaching its target, a correction from a maintainer), or before adding to, editing or deleting anything under .claude/skills.
---

# Curating the morphic skills

Keep each lesson once, in its strongest form, where the next person will be when it applies.

## Where a lesson goes

Take the first that fits.

1. **A check.** If a test or a lint or archtest rule can catch it, write or file that check. A
   filed check keeps a rule naming its issue until it lands.
2. **A script**, only to hold an invariant through a crash, count or compare exactly, or check
   every member of a large set. It prints facts, not verdicts parsed from tool output.
   Nothing runs a script's tests, so each is upkeep; otherwise the command goes in the rule's
   Probe.
3. **A rule in a skill.** Add it to a reference file of the narrowest skill whose trigger fires
   when the lesson applies. If none does, widen a description or add a skill: a rule no trigger
   reaches is never read.
4. **Not in the repository.** A fact about one machine, shell or person stays out of
   `.claude/skills`.

List the skills with `grep -h '^description:' .claude/skills/*/SKILL.md` rather than a hand
list.

## Rule format

```markdown
## <the rule in one line>
`<stable-key>`

- **Rule.** What to do, stated so it can be checked.
- **Why.** The failure it prevents, citing the PR or issue whose work showed it (#N), which its
  record may not describe.
- **Probe.** The command or planted defect that shows whether it holds, unless the Rule is one.
```

A rule stays within 150 words, or becomes two. No narrative, dates or first person, and no count a
command beside it does not derive, bar a Why's evidence, which its citation pins. Cite a rule as
`references/<file>.md#<key>`, from another skill by its full path.

## Limits

- A `SKILL.md` body stays within 500 words. Its description stays within 500 characters, since
  every session loads it, starts "Use when" and says when to load the skill, never what it does:
  an agent follows a summary instead of reading the body. Other frontmatter follows
  `https://agentskills.io/specification`.
- A reference file stays within 4,000 words. A full file takes a new rule only by merging or
  removing one.
- Everything works from a fresh clone and names no machine. Paths start at the repository root,
  bar those inside the skill's own folder, and commands run from the root. Write paths in code
  spans, where the lint holds each to the tree.
- Scripts are executable Python 3.14, standard library only, each with a `test_<name>.py` beside it.
  Run a skill's tests with `python3.14 -m unittest discover -s .claude/skills/<skill>/scripts`.

## Upkeep

- Run `python3.14 .claude/skills/morphic-curating-skills/scripts/skills_lint.py` after every edit.
  The gate does not run it; each push chains on it.
- After changing a rule's Why, confirm each item it cites is the work its clause describes.
- A lesson a skill holds that failed again means the skill failed: sharpen its wording or trigger,
  not add a copy.
- Skill changes ship like code, titled like `chore(skills): <subject>`.
