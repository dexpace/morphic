#!/usr/bin/env bash
# Holds the skills under .claude/skills to their limits and to the tree they describe.
#
# Usage: bash .claude/skills/morphic-curating-skills/scripts/skills-lint.sh
# Prints one "path:line: problem" per finding. Exit 0 when clean, 1 on a finding, 2 when it
# cannot run. Lines quoting history ("- **Seen in") are exempt from the tree checks: they
# describe the tree at the commit they name.
set -euo pipefail
export LC_ALL=C

root=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "skills-lint: run it inside the repository" >&2
  exit 2
}
skills="$root/.claude/skills"
[ -d "$skills" ] || { echo "skills-lint: $skills does not exist" >&2; exit 2; }

max_body_words=500
max_reference_words=4000
max_description_chars=500
max_frontmatter_chars=1024

# Split so this file does not match its own patterns.
machine_re='/U''sers/|/h''ome/[a-z]|/p''rivate/|~''/|\[''\[[a-z0-9-]+\]\]|\$HOME/\.cl''aude|scratch''pad'

findings=0
report() {
  printf '%s\n' "$*"
  findings=$((findings + 1))
}
rel() { printf '%s' "${1#"$root"/}"; }

top_level=$(git -C "$root" ls-files | cut -d/ -f1 | sort -u)

check_frontmatter() {
  local dir=$1 skill=$2 name end fm desc words
  name=$(basename "$dir")
  case "$name" in *[!a-z0-9-]*) report "$(rel "$skill"):1: name '$name' is not lowercase letters, digits and hyphens" ;; esac
  if [ "$(sed -n 1p "$skill")" != "---" ]; then
    report "$(rel "$skill"):1: frontmatter must open with ---"
    return
  fi
  end=$(awk 'NR > 1 && /^---$/ { print NR; exit }' "$skill")
  if [ -z "$end" ]; then
    report "$(rel "$skill"):1: frontmatter never closes"
    return
  fi
  fm=$(sed -n "2,$((end - 1))p" "$skill")
  [ "${#fm}" -le "$max_frontmatter_chars" ] ||
    report "$(rel "$skill"):1: frontmatter is ${#fm} characters, over $max_frontmatter_chars"
  printf '%s\n' "$fm" | grep -qx "name: $name" ||
    report "$(rel "$skill"):1: frontmatter name must be '$name'"
  desc=$(printf '%s\n' "$fm" | sed -n 's/^description: //p')
  case "$desc" in
    "Use when "*) ;;
    *) report "$(rel "$skill"):1: description must start \"Use when\"" ;;
  esac
  [ "${#desc}" -le "$max_description_chars" ] ||
    report "$(rel "$skill"):1: description is ${#desc} characters, over $max_description_chars"
  words=$(sed -n "$((end + 1)),\$p" "$skill" | wc -w | tr -d ' ')
  [ "$words" -le "$max_body_words" ] ||
    report "$(rel "$skill"): body is $words words, over $max_body_words"
}

check_references() {
  local dir=$1 ref words
  for ref in "$dir"/references/*.md; do
    [ -e "$ref" ] || continue
    words=$(wc -w <"$ref" | tr -d ' ')
    [ "$words" -le "$max_reference_words" ] ||
      report "$(rel "$ref"): $words words, over $max_reference_words"
    grep -qF "references/$(basename "$ref")" "$dir/SKILL.md" ||
      report "$(rel "$ref"): not named in $(rel "$dir")/SKILL.md"
  done
}

# path_exists reports whether a path a skill names exists, relative to the skill for its own
# references/ and scripts/, to the repository otherwise. Unknown first segments are not paths.
path_exists() {
  local dir=$1 p=$2 first
  p=${p#./}
  p=${p%%#*}
  p=$(printf '%s' "$p" | sed -E 's/:[0-9]+$//')
  case "$p" in *'*'*) p=${p%%\**} ;; esac
  p=${p%/}
  [ -n "$p" ] || return 0
  first=${p%%/*}
  case "$first" in
    references | scripts) [ -e "$dir/$p" ] ; return ;;
  esac
  printf '%s\n' "$top_level" | grep -qxF "$first" || return 0
  [ -e "$root/$p" ]
}

# current_lines prints "line:text" for the lines of a file that describe today's tree.
current_lines() { grep -nv '^- \*\*Seen in' "$1" || true; }

check_names() {
  local dir=$1 file=$2 line tok name doc num key path
  while IFS=: read -r line tok; do
    tok=${tok#\`}
    tok=${tok%\`}
    case "$tok" in
      morphic-*)
        case "$tok" in */*) ;; *)
          [ -d "$skills/$tok" ] || [ -d "$root/cmd/$tok" ] ||
            report "$(rel "$file"):$line: no skill or command named $tok"
          continue ;;
        esac ;;
    esac
    case "$tok" in */* | *.go | *.md | *.sh | *.yaml | *.json | *.txt) ;; *) continue ;; esac
    path_exists "$dir" "$tok" || report "$(rel "$file"):$line: $tok does not exist"
  done < <(current_lines "$file" | grep -oE '^[0-9]+|`[^` ]+`' | awk '/^[0-9]+$/ { n = $0 ":"; next } { print n $0 }')

  # Paths written outside backticks, such as scripts/x.sh in running prose.
  while IFS=: read -r line tok; do
    tok=$(printf '%s' "$tok" | sed -E 's/[.,;:)]+$//')
    path_exists "$dir" "$tok" || report "$(rel "$file"):$line: $tok does not exist"
  done < <(current_lines "$file" | sed 's/`[^`]*`//g' |
    grep -oE '^[0-9]+|[A-Za-z0-9_.-]+(/[A-Za-z0-9_.*-]+)+' | awk '/^[0-9]+$/ { n = $0 ":"; next } { print n $0 }')

  while IFS=: read -r line path; do
    case "$path" in http* | mailto:*) continue ;; esac
    [ -e "$(dirname "$file")/$path" ] || report "$(rel "$file"):$line: link target $path does not exist"
  done < <(current_lines "$file" | grep -oE '^[0-9]+|\]\([^)#]+' | awk '/^[0-9]+$/ { n = $0 ":"; next } { sub(/^\]\(/, ""); print n $0 }')

  while IFS=: read -r line name; do
    git -C "$root" grep -qE "func ${name}(\(|_)" -- '*_test.go' ||
      report "$(rel "$file"):$line: no test named $name"
  done < <(current_lines "$file" | grep -oE '^[0-9]+|(^|[^A-Za-z0-9_])Test[A-Z][A-Za-z0-9_]*' |
    awk '/^[0-9]+$/ { n = $0 ":"; next } { sub(/^[^T]/, ""); print n $0 }')

  while IFS=: read -r line doc num; do
    num=${num#§}
    num=${num# }
    grep -qE "^#{2,6} +$(printf '%s' "$num" | sed 's/\./\\./g')([. ]|\$)" "$root/docs/$doc.md" ||
      report "$(rel "$file"):$line: docs/$doc.md has no section $num"
  done < <(current_lines "$file" | grep -oE '^[0-9]+|(ir-design|emitter-design|architecture)(\.md)? ?§ ?[0-9]+(\.[0-9]+)*' |
    awk '/^[0-9]+$/ { n = $0 ":"; next } { sub(/\.md/, ""); sub(/ ?§/, ":§"); print n $0 }')

  while IFS=: read -r line doc key; do
    grep -qF "### \`$key\`" "$root/docs/$doc.md" ||
      report "$(rel "$file"):$line: docs/$doc.md has no entry $key"
  done < <(current_lines "$file" | grep -oE '^[0-9]+|(parser-library|compilers/openapi)\.md#[a-z0-9-]+' |
    awk '/^[0-9]+$/ { n = $0 ":"; next } { sub(/\.md#/, ":"); print n $0 }')
}

for dir in "$skills"/*/; do
  dir=${dir%/}
  check_frontmatter "$dir" "$dir/SKILL.md"
  check_references "$dir"
  for file in "$dir"/SKILL.md "$dir"/references/*.md; do
    [ -e "$file" ] && check_names "$dir" "$file"
  done
  for script in "$dir"/scripts/*.sh; do
    [ -e "$script" ] || continue
    bash -n "$script" 2>/dev/null || report "$(rel "$script"): does not parse (bash -n)"
  done
done

while IFS= read -r hit; do
  report "$(rel "${hit%%:*}"):$(printf '%s' "$hit" | cut -d: -f2): names one machine or a memory link"
done < <(grep -rnE "$machine_re" "$skills" || true)

if [ "$findings" -gt 0 ]; then
  echo "skills-lint: $findings finding(s)"
  exit 1
fi
echo "skills-lint: clean ($(ls -d "$skills"/*/ | wc -l | tr -d ' ') skills)"
