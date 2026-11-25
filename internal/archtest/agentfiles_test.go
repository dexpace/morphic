// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
	"github.com/dexpace/morphic/ir/irtest"
)

const (
	// skillsTree is where the repository keeps its skills, one directory each.
	skillsTree = ".claude/skills"
	// skillsManifestPath is the file naming directories of skills for
	// Antigravity, which does not look for them under .claude.
	skillsManifestPath = ".agents/skills.json"
	// agentRulesPath is the rule file an agent that does not read CLAUDE.md
	// loads, which sends it there.
	agentRulesPath = "AGENTS.md"
	// ruleFileByteCap is how much of a rule file Antigravity gives a session
	// at most, cutting at a line's end and counting the bytes it left out. Its
	// command line showed that at 1.5.17.
	ruleFileByteCap = 24_000
	// ruleFileInclude opens an include in a rule file. Antigravity puts the
	// named file's text in its place before it cuts, so the cap counts text
	// the rule file does not hold. Its command line showed that at 1.5.17.
	ruleFileInclude = "@["
)

// skillsManifest is the manifest as this repository writes it. Its reader also
// takes members that keep a skill of an entry out or add another manifest's
// entries; decoding refuses them, so an entry loads each skill directly under
// its directory.
type skillsManifest struct {
	// Entries are the directories read for skills.
	Entries []skillsManifestEntry `json:"entries"`
}

// skillsManifestEntry is one directory of skills.
type skillsManifestEntry struct {
	// Path is the directory, from the repository's root.
	Path string `json:"path"`
}

// TestSkills_ManifestLoadsEachOne fails on a skill the manifest's reader would
// not load, and on an entry naming a directory that holds none. An agent
// missing a skill reports nothing, so no session shows either. No gate runs
// the reader: this holds the manifest to what Antigravity's command line did
// with one at 1.5.17.
func TestSkills_ManifestLoadsEachOne(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(skillsManifestPath)))
	require.NoError(t, err)
	var manifest skillsManifest
	require.NoError(t, json.Unmarshal(data, &manifest, json.RejectUnknownMembers(true)),
		"%s must decode as entries holding a path and nothing else", skillsManifestPath)
	directories := make([]string, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		directories = append(directories, entry.Path)
	}

	skills := skillFiles(t, root, skillsTree)
	require.NotEmpty(t, skills, "found no skill under %s, so this proves nothing", skillsTree)

	for _, problem := range manifestProblems(directories, skills) {
		t.Errorf("%s: %s", skillsManifestPath, problem)
	}
}

// TestSkills_NoneSitsBesideTheManifest fails on a skill under the manifest's
// directory. Antigravity loads one from there whatever the manifest names, as
// its command line showed at 1.5.17, while skills_lint and an agent reading
// skillsTree alone never meet it. A directory of that name below the root,
// which Antigravity reads when started inside it, is not looked for.
func TestSkills_NoneSitsBesideTheManifest(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	manifestDirectory := path.Dir(skillsManifestPath)

	assert.Empty(t, skillFiles(t, root, manifestDirectory),
		"a skill under %s is outside %s, which holds every skill", manifestDirectory, skillsTree)
}

// skillFiles returns each SKILL.md under tree, as a path from root.
func skillFiles(t *testing.T, root, tree string) []string {
	t.Helper()
	var skills []string
	err := keydoc.Walk(root, tree, func(file string) error {
		if filepath.Base(file) == "SKILL.md" {
			skills = append(skills, keydoc.Rel(root, file))
		}
		return nil
	})
	require.NoError(t, err)
	return skills
}

// manifestProblems returns what keeps the directories a manifest names from
// loading exactly the skills: a skill that sits directly under none of them,
// since each is read one level deep, and a directory holding no skill. A
// directory is named from the root in any spelling [path.Clean] reads alike.
func manifestProblems(directories, skills []string) []string {
	var problems []string
	holdsSkill := make(map[string]bool, len(directories))
	for _, directory := range directories {
		holdsSkill[path.Clean(directory)] = false
	}
	for _, skill := range skills {
		directory := path.Dir(path.Dir(skill))
		if _, named := holdsSkill[directory]; !named {
			problems = append(problems, fmt.Sprintf("%s sits directly under no directory it names", skill))
			continue
		}
		holdsSkill[directory] = true
	}
	for _, directory := range directories {
		if !holdsSkill[path.Clean(directory)] {
			problems = append(problems, fmt.Sprintf("%s holds no skill", directory))
		}
	}
	return problems
}

func TestManifestProblems_ReportsAnUnloadedSkillAndAnEmptyDirectory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		directories []string
		skills      []string
		want        []string
	}{
		{
			name:        "each skill directly under a named directory",
			directories: []string{"skills", "more"},
			skills:      []string{"skills/a/SKILL.md", "more/b/SKILL.md"},
		},
		{
			name:        "a directory spelt with a dot and a trailing slash",
			directories: []string{"./skills/"},
			skills:      []string{"skills/a/SKILL.md"},
		},
		{
			name:        "a directory named twice",
			directories: []string{"skills", "skills"},
			skills:      []string{"skills/a/SKILL.md"},
		},
		{
			name:        "the root named by an empty path",
			directories: []string{""},
			skills:      []string{"a/SKILL.md", "skills/b/SKILL.md"},
			want:        []string{"skills/b/SKILL.md sits directly under no directory it names"},
		},
		{
			name:        "a skill one level deeper than is read",
			directories: []string{"skills"},
			skills:      []string{"skills/a/SKILL.md", "skills/group/b/SKILL.md"},
			want:        []string{"skills/group/b/SKILL.md sits directly under no directory it names"},
		},
		{
			name:        "a skill at the directory itself",
			directories: []string{"skills"},
			skills:      []string{"skills/SKILL.md", "skills/a/SKILL.md"},
			want:        []string{"skills/SKILL.md sits directly under no directory it names"},
		},
		{
			name:        "a directory the skills moved out of",
			directories: []string{"old"},
			skills:      []string{"skills/a/SKILL.md"},
			want: []string{
				"skills/a/SKILL.md sits directly under no directory it names",
				"old holds no skill",
			},
		},
		{
			name:   "no directory named",
			skills: []string{"skills/a/SKILL.md"},
			want:   []string{"skills/a/SKILL.md sits directly under no directory it names"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, manifestProblems(test.directories, test.skills))
		})
	}
}

// TestAgentRules_PointAtTheInstructionsWithinTheCap fails when the rule file
// stops naming CLAUDE.md, grows past the size it is cut at, or includes a
// file, whose text counts toward that size. So the instructions stay out of
// it: every session would get their start and a count of the rest.
func TestAgentRules_PointAtTheInstructionsWithinTheCap(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	text, err := os.ReadFile(filepath.Join(root, agentRulesPath))
	require.NoError(t, err)

	assert.Contains(t, string(text), "](CLAUDE.md)", "%s links no CLAUDE.md", agentRulesPath)
	assert.NotContains(t, string(text), ruleFileInclude,
		"%s includes a file, whose text counts toward the %d bytes it is cut at", agentRulesPath, ruleFileByteCap)
	assert.LessOrEqual(t, len(text), ruleFileByteCap, "%s is cut at %d bytes", agentRulesPath, ruleFileByteCap)
}
