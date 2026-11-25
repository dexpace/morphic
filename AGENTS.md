# AGENTS.md

The instructions for every agent working in this repository are in [`CLAUDE.md`](CLAUDE.md),
whichever agent its name suggests. Read that file in full before any other work here, and follow
it as you would this one.

This file only points there. An agent that loads `AGENTS.md` may load only its start: Antigravity
gives a session at most the first 24,000 bytes of a rule file, the text of a file it includes
counted, and a count of the rest. `CLAUDE.md` is longer than that, so the instructions are neither
moved here nor included from here.

The skills `CLAUDE.md` describes are under `.claude/skills`, one directory each, and a new one goes
there. `.agents/skills.json` names that directory for Antigravity, which does not look there, so a
skill is written once and read in place.
