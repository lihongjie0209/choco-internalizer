#!/usr/bin/env bash
set -uo pipefail

repository="${CHOCO_REPOSITORY:-https://choco.lihongjie.cn}"
report_dir="${REPORT_DIR:-reports}"
mkdir -p "$report_dir" dist
report="$report_dir/${REPORT_NAME:-sync-summary.md}"

if [[ $# -eq 0 ]]; then
  mapfile -t packages < packages.txt
elif [[ $# -eq 1 && -f "$1" ]]; then
  mapfile -t packages < "$1"
else
  packages=("$@")
fi

{
  echo "# Chocolatey sync summary"
  echo
  echo "Run: $(date -u +'%Y-%m-%dT%H:%M:%SZ')"
  echo
  echo '| Package | Result |'
  echo '|---|---|'
} > "$report"

success=0
failed=0
sync_package() {
  local package_name="$1"
  local alias_dir="aliases/$package_name"
  if [[ ! -d "$alias_dir" ]]; then
    ./bin/choco-internalizer sync "$package_name" --output dist --repository "$repository"
    return
  fi

  while IFS= read -r dependency; do
    [[ -z "$dependency" || "$dependency" == \#* ]] && continue
    ./bin/choco-internalizer sync "$dependency" --output dist --repository "$repository"
  done < "$alias_dir/dependencies.txt"
  local alias_work
  alias_work=$(mktemp -d)
  local package_path="$alias_work/$package_name.nupkg"
  (cd "$alias_dir" && zip -qr "$package_path" . -x dependencies.txt)
  ./bin/choco-internalizer internalize --input "$package_path" --output "$alias_work/output" --repository "$repository"
  rm -rf "$alias_work"
}

for package_name in "${packages[@]}"; do
  [[ -z "$package_name" || "$package_name" == \#* ]] && continue
  echo "::group::sync $package_name"
  if output=$(sync_package "$package_name" 2>&1); then
    echo "$output"
    printf '| `%s` | ✅ synchronized |\n' "$package_name" >> "$report"
    ((success+=1))
  else
    echo "$output"
    message=$(printf '%s' "$output" | tail -n 1 | sed 's/|/\\|/g')
    printf '| `%s` | ⚠️ %s |\n' "$package_name" "$message" >> "$report"
    echo "::warning title=Package compatibility::$package_name: $message"
    ((failed+=1))
  fi
  echo "::endgroup::"
done

{
  echo
  echo "Successful roots: $success"
  echo "Compatibility failures: $failed"
} >> "$report"

cat "$report"
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  cat "$report" >> "$GITHUB_STEP_SUMMARY"
fi

# Compatibility failures are reported but intentionally do not interrupt CI.
exit 0
