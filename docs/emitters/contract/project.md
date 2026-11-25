# The project configuration

A run that generates needs, for each target, its options and where its files go, and for all of
them the policy the plan is built under. Flags cannot carry that: options are structured and each
target's own, and the engine needs the configuration of a target the run does not name, as a docs
target's sample targets are ([docs and
mocks](../docs-and-mock.md#samples-come-from-the-sdk-emitters)). So a run reads them from one
document, the project configuration, which its caller names.

```json
{
  "version": 1,
  "plan": { "version": 1 },
  "targets": {
    "go": { "output": "sdk/go", "options": { "version": 1 } }
  }
}
```

```go
package project

type Project struct {
    Plan    plan.Policy                   // what the plan is built under (../plan/policy.md)
    Targets map[emitters.TargetKey]Target // each target the project configures
}

type Target struct {
    Output  string         // the target's output root, absolute
    Options jsontext.Value // the target's own configuration, undecoded (options.md)
}

// Decode reads a project configuration. directory is what a relative output is resolved against.
func Decode(raw []byte, directory string) (Project, []ir.Diagnostic, error)

// Validate holds a project to what every run needs of one, however it was built.
func Validate(project Project) error
```

- **Named, never discovered.** The command that generates takes the file's path, and nothing
  searches a working directory or its parents for one. A run that found its configuration by where
  it was started would generate two trees from one command line, started in two directories. A
  relative `output` is resolved against the directory holding the file, for the same reason, and
  never against the working directory.
- **Strict and versioned.** The file is decoded as every configuration is ([target
  options](options.md)): it states its `version`, an unknown or repeated member and a misplaced
  `null` are refused, and what it leaves unset takes the defaults of the version it states. Its
  members are named in lower camel case. A fault is a Go error, the invocation's, reported before
  the spec is read.
- **`plan` is the plan's policy**, a configuration of its own that states its own version
  ([plan policy](../plan/policy.md)). It is required, so no project plans under defaults nobody
  chose.
- **What `Decode` reports** is the upgrade report alone ([target options](options.md)): an info
  `engine/project-behind` for each setting the project leaves unset whose default a later version
  of the project's configuration moved, and `emitters/plan/policy-behind` for each such setting of
  the plan's policy. They are findings of the run that generates from the project: its caller hands
  them to it, and the run reports and rates them with the rest, as it does the upgrade report a
  target's `Configure` returns, so a threshold and a triage rule read all three alike.
- **A target is its output root and its options.** Both are required, and an empty `output` is
  none. `options` is the target's configuration as the target's `Configure` reads it ([the
  contract](emitter.md#a-target-is-configured-once)), kept undecoded here, since only the target
  knows its shape: a run decodes the options of the targets it runs, and reports their faults.
- **Each target has an output root of its own** ([write /
  integrate](../write-integrate.md#one-output-root-per-target)). Two targets whose roots are one
  directory, or one inside the other, compared segment by segment under the path fold once each is
  cleaned, are refused: `out` twice, `Out` beside `out`, `out` beside `out/go`, `a/../out` beside
  `out`. An output written relative is read with `/` between its segments and cleaned, and each
  segment it then names, a leading `..` and a `.` that is all of it aside, is one every file system
  writes ([emit rules](../emit/rules.md)), so a project checked out on another system names the
  same roots: `./sdk/go`, `sdk/go/` and `a/../out` are `sdk/go`, `sdk/go` and `out`; `.`, `./` and
  `a/..` are the directory holding the file; `../sdk/go` is a root beside that directory; and
  `sdk/nul` and `C:\sdk\go` are refused. A `..` that leads and a `.` that is the whole cleaned
  output are path syntax and no name, and refusing either would send a project kept in a directory
  of its own, or in the root it generates, to an absolute output, which no second checkout shares.
- **One rule for a file and for Go.** A caller using the engine as a library builds the same
  `Project` value in Go. `Validate` is what a project must hold, and both `Decode` and the engine
  call it, so a project built in Go is refused where the same one decoded would be.

A run names the targets it generates, each of which the project must configure, and a run naming
none generates nothing: it is the compile it was. A target the project configures and the run does
not name is not run, but for a target another names for its samples.

The project's own policy and hint layers, the mapping that reads a document's embedded policy
([embedded policy](embedded-policy.md)), and each target's policy and hint layers
([layers](runtime-policy.md#layers-and-operations)) are members of this document too. Each joins
it with the code that resolves it, the first runtime templates.

Per-run flags naming each target's options were the alternative. They cannot state a structure,
they would repeat on every invocation what a project decides once, and a docs run would have to
be told the options of every SDK it shows.
