#!/usr/bin/env bash
# The Windows half of `cv gate`: copy this working tree to the Windows VM,
# run the suite there with VELLUM_REQUIRE_DEPS=1, and hold its skips to a
# fixed list.
#
#   scripts/windows-gate.sh            # host from $VELLUM_WINDOWS_HOST, default hms-vm
#
# What is copied is the working tree as it stands (tracked files plus
# untracked files that are not ignored), so the run tests what the macOS
# half of the gate tests, uncommitted edits included.
#
# Lessons this script encodes:
#   - macOS tar writes AppleDouble "._name" entries for files with extended
#     attributes. Go reads every file in a fuzz corpus directory, so one
#     "._" file there fails FuzzDocument on Windows. COPYFILE_DISABLE=1
#     stops tar writing them. Real "._" files on disk (left by copying
#     through a non-Apple volume) are dropped from the file list. The
#     archive cannot be checked with macOS tar instead: its listing hides
#     "._" entries even when they are there.
#   - The race detector does not run on windows/arm64, so there is no -race.
#   - Each run gets its own folder on the VM and removes it afterwards, so a
#     stale tree from an earlier run is never tested by mistake.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

host="${VELLUM_WINDOWS_HOST:-hms-vm}"
vm_name="Windows 11"           # Parallels VM name, used only to wake it
ssh_timeout=10                 # seconds for one connection attempt
wake_attempts=36               # x 5s: how long to wait for a woken VM

# Skips allowed on Windows, by test name. Locked in both directions: a new
# skip fails the run, and so does one of these that no longer skips (then
# remove it here). Each names the target that will remove it.
allowed_skips=(
	TestMmdcCommand_ImportsTheConfineHook # 🎯T69: Mermaid confinement on Windows
)

work="$(mktemp -d)"
remote_dir="vellum-gate-$(date +%Y%m%d%H%M%S)-$$"
cleanup() {
	rm -rf "$work"
	ssh -o ConnectTimeout="$ssh_timeout" "$host" \
		"Remove-Item -Recurse -Force \$HOME\\$remote_dir, \$HOME\\$remote_dir.tgz -ErrorAction SilentlyContinue" \
		>/dev/null 2>&1 || true
}
trap cleanup EXIT

reachable() {
	ssh -o ConnectTimeout="$ssh_timeout" -o BatchMode=yes "$host" 'exit 0' >/dev/null 2>&1
}

if ! reachable; then
	if command -v prlctl >/dev/null; then
		state="$(prlctl status "$vm_name" 2>/dev/null | awk '{print $NF}')"
		case "$state" in
		stopped) echo "windows-gate: starting VM \"$vm_name\""; prlctl start "$vm_name" >/dev/null ;;
		suspended | paused) echo "windows-gate: resuming VM \"$vm_name\""; prlctl resume "$vm_name" >/dev/null ;;
		esac
		for ((i = 0; i < wake_attempts; i++)); do
			reachable && break
			sleep 5
		done
	fi
	reachable || {
		echo "windows-gate: cannot reach $host over ssh. Start the Parallels VM \"$vm_name\" and retry." >&2
		exit 1
	}
fi

# The working tree's files, skipping tracked files deleted on disk.
list="$work/files"
: >"$list"
while IFS= read -r -d '' f; do
	[[ -e "$f" ]] || continue
	if [[ "$(basename "$f")" == ._* ]]; then
		echo "windows-gate: leaving out AppleDouble file $f"
		continue
	fi
	printf '%s\0' "$f" >>"$list"
done < <(git ls-files -z --cached --others --exclude-standard)

archive="$work/tree.tgz"
COPYFILE_DISABLE=1 tar --null -T "$list" -czf "$archive"

scp -q "$archive" "$host:$remote_dir.tgz"

log="$work/test.log"
set +e
# One line: the VM's ssh hands the command to PowerShell as a single string.
remote="New-Item -ItemType Directory \$HOME\\$remote_dir | Out-Null; "
remote+="tar -xzf \$HOME\\$remote_dir.tgz -C \$HOME\\$remote_dir; if (\$LASTEXITCODE -ne 0) { exit 2 }; "
remote+="cd \$HOME\\$remote_dir; \$env:VELLUM_REQUIRE_DEPS = '1'; "
remote+="go test ./... -count=1 -v 2>&1 | ForEach-Object { \"\$_\" }; exit \$LASTEXITCODE"
ssh "$host" "$remote" >"$log" 2>&1
rc=$?
set -e

grep -E '^(ok |FAIL|--- FAIL)' "$log" || true
if [[ $rc -ne 0 ]]; then
	echo "windows-gate: suite failed on $host (exit $rc); failures:" >&2
	grep -E -B2 -A12 -- '--- FAIL' "$log" >&2 || tail -40 "$log" >&2
	exit 1
fi

# Skip census against allowed_skips.
skipped="$(grep -E -- '--- SKIP: ' "$log" | sed -E 's/.*--- SKIP: ([^ ]+).*/\1/' | sort -u)"
status=0
for t in $skipped; do
	known=0
	for a in "${allowed_skips[@]}"; do [[ "$t" == "$a" ]] && known=1; done
	if [[ $known -eq 0 ]]; then
		echo "windows-gate: unexpected skip: $t" >&2
		grep -E -B3 -- "--- SKIP: $t" "$log" >&2
		status=1
	fi
done
for a in "${allowed_skips[@]}"; do
	if ! printf '%s\n' "$skipped" | grep -qx "$a"; then
		echo "windows-gate: $a no longer skips; remove it from allowed_skips in $0" >&2
		status=1
	fi
done
[[ $status -eq 0 ]] || exit 1

n="$(printf '%s\n' "$skipped" | grep -c . || true)"
echo "windows-gate: green on $host (skipped=$n, all on the allowed list)"
