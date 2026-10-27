#!/usr/bin/env bash
# currency reports every dependency, toolchain, and pin that trails its latest
# release, one line each, and exits non-zero when it reports any. It reads the
# network and writes nothing. Run it through `mise run currency`.
#
# Each scan captures its command's output in a variable before reading it, so
# under set -e a scan command that fails fails currency instead of reporting
# nothing.
set -euo pipefail
cd "$MISE_PROJECT_ROOT"
export GOWORK=off

stale=0
report() {
	echo "$1"
	stale=1
}

# matches prints each match of a grep pattern in the given files that exist.
# A missing file or no match is no output; any other grep error fails.
matches() {
	local pattern=$1 file
	local files=()
	shift
	for file in "$@"; do
		if [ -e "$file" ]; then files+=("$file"); fi
	done
	[ ${#files[@]} -gt 0 ] || return 0
	grep -ho "$pattern" "${files[@]}" || [ $? -eq 1 ]
}

go_minor=$(mise latest go | cut -d. -f1,2)

for mod in $GO_MODULES; do
	# Direct requirements with a newer version within their major.
	updates=$(cd "$mod" && go list -m -u -f \
		'{{if and (not .Main) (not .Indirect) .Update}}{{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}' all)
	while read -r line; do
		[ -n "$line" ] && report "$mod/go.mod: $line"
	done <<<"$updates"

	# The go directive's minor against the current Go minor.
	directive=$(cd "$mod" && go mod edit -json | jq -r .Go | cut -d. -f1,2)
	[ "$directive" = "$go_minor" ] || report "$mod/go.mod: go $directive -> go $go_minor"
done

# mise tools pinned below their latest version.
outdated=$(mise outdated --bump --local --json |
	jq -r 'to_entries[] | "mise.toml: \(.key) \(.value.requested) -> \(.value.bump)"')
while read -r line; do
	[ -n "$line" ] && report "$line"
done <<<"$outdated"

# GitHub Actions pinned below the action's latest release. The repository is
# the project, so its workflows are the project root's; with none, the glob
# stays unexpanded and matches skips it.
workflows=(.github/workflows/*.yml)
actions=$(matches 'uses: *[^ ]*@[^ ]*' "${workflows[@]}" | sed 's/uses: *//' | sort -u)
for uses in $actions; do
	action=${uses%@*}
	pin=${uses#*@}
	repo=$(echo "$action" | cut -d/ -f1,2)
	latest=$(gh api "repos/$repo/releases/latest" --jq .tag_name)
	[ "$pin" = "$latest" ] || report ".github/workflows: $action $pin -> $latest"
done

exit "$stale"
