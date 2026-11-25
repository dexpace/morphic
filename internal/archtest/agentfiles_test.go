// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
	"github.com/dexpace/morphic/ir/irtest"
)

const (
	// skillsTree is where the repository keeps its skills, one directory each.
	skillsTree = ".claude/skills"
	// skillFileName is the file that makes a directory a skill.
	skillFileName = "SKILL.md"
	// skillsManifestPath is the file naming skillsTree for Antigravity, which
	// does not look for skills under .claude.
	skillsManifestPath = ".agents/skills.json"
	// finderMetadataName is the file the macOS file manager leaves in a
	// directory it has shown, which .gitignore keeps out of the repository.
	finderMetadataName = ".DS_Store"
	// agentRulesPath is the rule file an agent that does not read CLAUDE.md
	// loads, which sends it there.
	agentRulesPath = "AGENTS.md"
	// instructionsPath is the file agentRulesPath sends an agent to.
	instructionsPath = "CLAUDE.md"
	// ruleFileByteCap is how much of a rule file Antigravity gives a session
	// at most, cutting at a line's end and counting the bytes it left out. Its
	// command line showed that at 1.5.17.
	ruleFileByteCap = 24_000
	// ruleFileInclude opens an include in a rule file. Antigravity puts the
	// named file's text in its place before it cuts, so the cap counts text
	// the rule file does not hold. Its command line showed that at 1.5.17.
	ruleFileInclude = "@["
)

// skillsManifest is the manifest as this repository writes it. Its loader also
// takes members that keep a skill of an entry out or add another manifest's
// entries, which decoding refuses.
type skillsManifest struct {
	// Entries are the directories read for skills.
	Entries []skillsManifestEntry `json:"entries"`
}

// skillsManifestEntry is one directory of skills.
type skillsManifestEntry struct {
	// Path is the directory, from the repository's root.
	Path string `json:"path"`
}

// TestAgentFiles_ReachAntigravity fails when a skill or the instructions would
// not reach an Antigravity session, or a skill would reach no other agent. An
// agent missing either reports nothing, so no session shows it. No gate runs
// the loader: this holds the files to what Antigravity's command line did with
// them at 1.5.17.
func TestAgentFiles_ReachAntigravity(t *testing.T) {
	t.Parallel()
	problems, err := agentFileProblems(irtest.RepoRoot(t))
	require.NoError(t, err)
	for _, problem := range problems {
		t.Error(problem)
	}
}

// agentFileProblems returns what keeps the tree at root from giving
// Antigravity each skill and the instructions, and from keeping each skill
// where every agent and skills_lint read it.
func agentFileProblems(root string) ([]string, error) {
	var problems []string
	checks := []func(root string) ([]string, error){
		manifestProblems, skillProblems, agentDirectoryProblems, agentRulesProblems,
	}
	for _, check := range checks {
		found, err := check(root)
		if err != nil {
			return nil, err
		}
		problems = append(problems, found...)
	}
	return problems, nil
}

// manifestProblems reports a manifest that is not the one entry naming
// skillsTree. The loader accepts more, each a way to lose a skill: a member
// filtering one out, an entry with no directory, for which it reads nothing,
// and a second directory, which hides the first's skills where it holds a
// skill file of its own. A path spelt another way loads alike and is refused
// with them.
func manifestProblems(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(skillsManifestPath)))
	if err != nil {
		return nil, err
	}
	manifest, decoded := decodeManifest(data)
	switch {
	case !decoded:
		return []string{skillsManifestPath + " does not decode as entries holding a path and nothing else"}, nil
	case len(manifest.Entries) != 1 || manifest.Entries[0].Path != skillsTree:
		return []string{fmt.Sprintf("%s must name %s and no other directory", skillsManifestPath, skillsTree)}, nil
	}
	return nil, nil
}

// decodeManifest returns the manifest data holds, and false where data is no
// manifest of entries holding a path and nothing else: what is wrong with it
// is the finding, not an error of the read.
func decodeManifest(data []byte) (skillsManifest, bool) {
	var manifest skillsManifest
	err := json.Unmarshal(data, &manifest, json.RejectUnknownMembers(true))
	return manifest, err == nil
}

// skillProblems reports each skill file under skillsTree the manifest's entry
// does not load as a skill of its own, and a tree holding none. The loader
// takes each directory skillsTree holds as a skill, one whose name opens with
// a dot included, and skillsTree itself as the one skill where it holds a
// skill file. The file's name is held to its spelling, which a file system
// that folds case does not ask for. A link to a directory is not followed.
func skillProblems(root string) ([]string, error) {
	var problems []string
	skills := 0
	start := filepath.Join(root, filepath.FromSlash(skillsTree))
	err := filepath.WalkDir(start, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(entry.Name(), skillFileName) {
			return nil
		}
		skill := keydoc.Rel(root, file)
		switch {
		case entry.Name() != skillFileName:
			problems = append(problems, fmt.Sprintf("%s is not spelt %s", skill, skillFileName))
		case path.Dir(path.Dir(skill)) != skillsTree:
			problems = append(problems, fmt.Sprintf("%s is in no directory directly under %s", skill, skillsTree))
		default:
			skills++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if skills == 0 {
		problems = append(problems, skillsTree+" holds no skill")
	}
	return problems, nil
}

// agentDirectoryProblems reports anything beside the manifest in its
// directory, and each other directory the loader reads as it does that one.
// It loaded a skill from there through a skills directory, a link to one and a
// plugin, whatever the manifest names, and skills_lint and an agent reading
// skillsTree alone never meet such a skill. Not looked for: a directory of the
// manifest's below the root, which the loader reads when started inside it.
func agentDirectoryProblems(root string) ([]string, error) {
	manifestDirectory := path.Dir(skillsManifestPath)
	entries, err := os.ReadDir(filepath.Join(root, manifestDirectory))
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, entry := range entries {
		if name := entry.Name(); name != path.Base(skillsManifestPath) && name != finderMetadataName {
			problems = append(problems, fmt.Sprintf("%s/%s is beside the manifest, which %s holds alone: a skill belongs under %s",
				manifestDirectory, name, manifestDirectory, skillsTree))
		}
	}
	for _, other := range []string{".agent", "_agents", "_agent"} {
		_, err := os.Lstat(filepath.Join(root, other))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		problems = append(problems, fmt.Sprintf("%s is read as %s is, which alone holds what an agent loads", other, manifestDirectory))
	}
	return problems, nil
}

// agentRulesProblems reports a rule file that links no instructions, includes
// a file, whose text counts toward the size it is cut at, or is past that
// size. So the instructions stay out of it: every session would get their
// start and a count of the rest. It reads the root's file alone. A GEMINI.md
// beside it and an AGENTS.md below the root load too, as Antigravity's command
// line showed at 1.5.17, and nothing holds them.
func agentRulesProblems(root string) ([]string, error) {
	text, err := os.ReadFile(filepath.Join(root, agentRulesPath))
	if err != nil {
		return nil, err
	}
	var problems []string
	if !linksFile(string(text), instructionsPath) {
		problems = append(problems, fmt.Sprintf("%s links no %s", agentRulesPath, instructionsPath))
	}
	if strings.Contains(string(text), ruleFileInclude) {
		problems = append(problems, fmt.Sprintf("%s includes a file, whose text counts toward the %d bytes it is cut at",
			agentRulesPath, ruleFileByteCap))
	}
	if len(text) > ruleFileByteCap {
		problems = append(problems, fmt.Sprintf("%s is %d bytes, past the %d it is cut at", agentRulesPath, len(text), ruleFileByteCap))
	}
	return problems, nil
}

// linksFile reports whether the Markdown text links file, a neighbour of the
// document holding it, by an inline link with no title, the one form the
// documents write. The target may open with a dot and name a heading. A link
// inside a span or block of code is none.
func linksFile(text, file string) bool {
	for _, line := range linkLines(text) {
		for _, link := range markdownLink.FindAllStringSubmatch(line, -1) {
			target, _, _ := strings.Cut(link[1], "#")
			if path.Clean(target) == file {
				return true
			}
		}
	}
	return false
}

// writeAgentFiles writes under root the least tree agentFileProblems finds
// nothing in: the manifest, one skill and a rule file linking the instructions.
func writeAgentFiles(t *testing.T, root string) {
	t.Helper()
	writeDocFile(t, filepath.Join(root, ".agents", "skills.json"), `{"entries": [{"path": ".claude/skills"}]}`)
	writeDocFile(t, filepath.Join(root, ".claude", "skills", "a", "SKILL.md"), "---\nname: a\n---\n")
	writeDocFile(t, filepath.Join(root, "AGENTS.md"), "Read [the instructions](CLAUDE.md).\n")
}

func TestAgentFileProblems_ReportsWhatKeepsAFileFromAnAgent(t *testing.T) {
	t.Parallel()
	const (
		undecoded  = ".agents/skills.json does not decode as entries holding a path and nothing else"
		misnamed   = ".agents/skills.json must name .claude/skills and no other directory"
		besideRest = " is beside the manifest, which .agents holds alone: a skill belongs under .claude/skills"
		readAlike  = " is read as .agents is, which alone holds what an agent loads"
	)
	skill := "---\nname: b\n---\n"
	write := func(file, text string) func(*testing.T, string) {
		return func(t *testing.T, root string) {
			t.Helper()
			writeDocFile(t, filepath.Join(root, filepath.FromSlash(file)), text)
		}
	}
	tests := []struct {
		name  string
		plant func(t *testing.T, root string)
		want  []string
	}{
		{name: "the files as the repository writes them", plant: func(*testing.T, string) {}},
		{
			name:  "a skill in a directory whose name opens with a dot",
			plant: write(".claude/skills/.b/SKILL.md", skill),
		},
		{
			name:  "the file manager's file beside the manifest",
			plant: write(".agents/.DS_Store", ""),
		},
		{
			name:  "a rule file of the size it is cut at",
			plant: write("AGENTS.md", "[it](CLAUDE.md)\n"+strings.Repeat("x", ruleFileByteCap-16)),
		},
		{
			name:  "a manifest member that filters a skill out",
			plant: write(".agents/skills.json", `{"entries": [{"path": ".claude/skills"}], "exclude": ["a"]}`),
			want:  []string{undecoded},
		},
		{
			name:  "a manifest that is not JSON",
			plant: write(".agents/skills.json", "entries: .claude/skills\n"),
			want:  []string{undecoded},
		},
		{
			name:  "a manifest naming another directory",
			plant: write(".agents/skills.json", `{"entries": [{"path": ".claude"}]}`),
			want:  []string{misnamed},
		},
		{
			name:  "a manifest naming a second directory",
			plant: write(".agents/skills.json", `{"entries": [{"path": ".claude/skills"}, {"path": ".claude"}]}`),
			want:  []string{misnamed},
		},
		{
			name:  "a manifest entry naming no directory",
			plant: write(".agents/skills.json", `{"entries": [{"path": ".claude/skills"}, {}]}`),
			want:  []string{misnamed},
		},
		{
			name:  "a manifest naming no directory",
			plant: write(".agents/skills.json", `{"entries": []}`),
			want:  []string{misnamed},
		},
		{
			name:  "a skill one directory deeper",
			plant: write(".claude/skills/group/b/SKILL.md", skill),
			want:  []string{".claude/skills/group/b/SKILL.md is in no directory directly under .claude/skills"},
		},
		{
			name:  "a skill one directory deeper, under a name that opens with a dot",
			plant: write(".claude/skills/.group/b/SKILL.md", skill),
			want:  []string{".claude/skills/.group/b/SKILL.md is in no directory directly under .claude/skills"},
		},
		{
			name:  "a skill file in the tree itself",
			plant: write(".claude/skills/SKILL.md", skill),
			want:  []string{".claude/skills/SKILL.md is in no directory directly under .claude/skills"},
		},
		{
			name:  "a skill file in another case",
			plant: write(".claude/skills/b/skill.md", skill),
			want:  []string{".claude/skills/b/skill.md is not spelt SKILL.md"},
		},
		{
			name: "a directory of the skill file's name and no skill",
			plant: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.Remove(filepath.Join(root, ".claude", "skills", "a", "SKILL.md")))
				require.NoError(t, os.Mkdir(filepath.Join(root, ".claude", "skills", "a", "SKILL.md"), 0o755))
			},
			want: []string{".claude/skills holds no skill"},
		},
		{
			name:  "a skill beside the manifest",
			plant: write(".agents/skills/b/SKILL.md", skill),
			want:  []string{".agents/skills" + besideRest},
		},
		{
			name: "a link to the skills beside the manifest",
			plant: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.Symlink(filepath.Join("..", ".claude", "skills"), filepath.Join(root, ".agents", "skills")))
			},
			want: []string{".agents/skills" + besideRest},
		},
		{
			name:  "a plugin manifest beside the manifest",
			plant: write(".agents/plugins.json", `{"entries": [{"path": "plugins"}]}`),
			want:  []string{".agents/plugins.json" + besideRest},
		},
		{
			name: "each other directory the loader reads",
			plant: func(t *testing.T, root string) {
				t.Helper()
				for _, other := range []string{".agent", "_agents", "_agent"} {
					writeDocFile(t, filepath.Join(root, other, "skills", "b", "SKILL.md"), skill)
				}
			},
			want: []string{".agent" + readAlike, "_agents" + readAlike, "_agent" + readAlike},
		},
		{
			name:  "a rule file linking another file",
			plant: write("AGENTS.md", "Read [the instructions](README.md).\n"),
			want:  []string{"AGENTS.md links no CLAUDE.md"},
		},
		{
			name:  "a rule file including the instructions",
			plant: write("AGENTS.md", "@[the instructions](CLAUDE.md)\n"),
			want:  []string{"AGENTS.md includes a file, whose text counts toward the 24000 bytes it is cut at"},
		},
		{
			name:  "a rule file one byte past the size it is cut at",
			plant: write("AGENTS.md", "[it](CLAUDE.md)\n"+strings.Repeat("x", ruleFileByteCap-15)),
			want:  []string{"AGENTS.md is 24001 bytes, past the 24000 it is cut at"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeAgentFiles(t, root)
			test.plant(t, root)

			problems, err := agentFileProblems(root)

			require.NoError(t, err)
			assert.Equal(t, test.want, problems)
		})
	}
}

func TestLinksFile_ReadsAnInlineLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "a link", text: "Read [the instructions](CLAUDE.md) first.", want: true},
		{name: "a label that is a span of code", text: "Read [`CLAUDE.md`](CLAUDE.md) first.", want: true},
		{name: "a target spelt from the directory", text: "Read [them](./CLAUDE.md).", want: true},
		{name: "a target at a heading", text: "Read [them](CLAUDE.md#project-status).", want: true},
		{name: "a link after one to another file", text: "See [this](README.md), then [them](CLAUDE.md).", want: true},
		{name: "a link to another file", text: "Read [them](README.md).", want: false},
		{name: "a file of that name elsewhere", text: "Read [them](docs/CLAUDE.md).", want: false},
		{name: "a link inside a span of code", text: "Write `[them](CLAUDE.md)` there.", want: false},
		{name: "a link inside a block of code", text: "```\n[them](CLAUDE.md)\n```\n", want: false},
		{name: "a link with a title", text: `Read [them](CLAUDE.md "the instructions").`, want: false},
		{name: "the file named and not linked", text: "Read CLAUDE.md first.", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, linksFile(test.text, "CLAUDE.md"))
		})
	}
}
