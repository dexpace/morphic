# Recorded lessons

Where a lesson written into a tracking issue, a plan or a handoff goes, and when.

## Route each lesson a tracker or plan records to the skills before its state next changes
`recorded-lesson-goes-to-the-skills`

- **Rule.** Before a tracker's state is updated or the next plan is drawn up, each lesson a
  tracker, plan or handoff records becomes a filed `skills:` issue, or a rule placed by "Where a
  lesson goes" where "Upkeep" allows the edit, and the record names that number or key beside it.
- **Why.** #1918's body recorded what its work taught and said a lasting lesson goes into the
  skills; none was moved, and later work repeated them: builders of #2098 changed decisions inside
  their builds, which #1918's had been told not to do (#1918, #2297).
- **Probe.** Each bullet under the record's lessons heading names a rule key or an issue number
  (`gh issue view <N> --json body --template '{{.body}}'`).
