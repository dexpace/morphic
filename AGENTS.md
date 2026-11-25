# AGENTS.md

The instructions for every agent working in this repository are in [`CLAUDE.md`](CLAUDE.md),
whichever agent its name suggests. Read that file in full before any other work here, and follow
it as you would this one.

This file only points there. An agent that loads `AGENTS.md` may load only its start: Antigravity
gives a session at most the first 24,000 bytes of a rule file and a count of the rest, and
`CLAUDE.md` is longer than that. So the instructions are neither moved here nor included from here.

The skills `CLAUDE.md` describes are under `.claude/skills`, one directory each.
`.agents/skills.json` names that directory for an agent that looks for skills under `.agents`, so a
skill is written once and read in place.
