#!/usr/bin/env bash
# Entry point for the infradiff composite GitHub Action. All configuration comes
# from INPUT_* environment variables set by action.yml.
set -uo pipefail

path="${INPUT_PATH:-.}"
base_ref="${INPUT_BASE_REF:-}"
format="$(printf '%s' "${INPUT_FORMAT:-mermaid}" | tr '[:upper:]' '[:lower:]')"
output="${INPUT_OUTPUT:-}"
comment="$(printf '%s' "${INPUT_COMMENT:-false}" | tr '[:upper:]' '[:lower:]')"
fail_on_change="$(printf '%s' "${INPUT_FAIL_ON_CHANGE:-false}" | tr '[:upper:]' '[:lower:]')"

case "$format" in
  mermaid) extension="mmd" ;;
  json) extension="json" ;;
  html) extension="html" ;;
  *)
    echo "::error::unknown format \"$format\"; choose mermaid, json, or html"
    exit 2
    ;;
esac

if [ -z "${GITHUB_ACTION_PATH:-}" ] || [ -z "${GITHUB_WORKSPACE:-}" ]; then
  echo "::error::GITHUB_ACTION_PATH and GITHUB_WORKSPACE must be set"
  exit 2
fi

bin="${RUNNER_TEMP:-/tmp}/infradiff"
if [ "${RUNNER_OS:-}" = "Windows" ]; then
  bin="${bin}.exe"
fi
(
  cd "$GITHUB_ACTION_PATH" || exit 1
  go build -o "$bin" ./cmd/infradiff
) || {
  echo "::error::failed to build infradiff"
  exit 1
}

if [ -z "$output" ]; then
  output="${RUNNER_TEMP:-/tmp}/infradiff.$extension"
fi
case "$output" in
  /* | [A-Za-z]:*) ;;
  *) output="$GITHUB_WORKSPACE/$output" ;;
esac
mkdir -p -- "$(dirname -- "$output")"

summary_file="${RUNNER_TEMP:-/tmp}/infradiff-summary.txt"
status=0
cd "$GITHUB_WORKSPACE" || exit 1
if [ -n "$base_ref" ]; then
  args=(diff --git "$base_ref" "$path" -f "$format" -o "$output")
  if [ "$fail_on_change" = "true" ]; then
    args+=(--fail-on-change)
  fi
  "$bin" "${args[@]}" 2>"$summary_file" || status=$?
else
  "$bin" generate "$path" -f "$format" -o "$output" 2>"$summary_file" || status=$?
fi

if [ "$status" -ne 0 ] && ! { [ "$status" -eq 1 ] && [ "$fail_on_change" = "true" ] && [ -n "$base_ref" ]; }; then
  cat "$summary_file" >&2
  echo "::error::infradiff exited with status $status"
  exit "$status"
fi

echo "::notice::infradiff wrote $output"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  printf 'output-path=%s\n' "$output" >>"$GITHUB_OUTPUT"
fi

render_markdown() {
  echo "## infradiff"
  echo
  if [ -s "$summary_file" ]; then
    echo '```'
    cat "$summary_file"
    echo '```'
    echo
  fi
  if [ "$format" = "html" ]; then
    echo "HTML diagram written to \`$output\`."
    return
  fi
  echo "<details><summary>Infrastructure $([ -n "$base_ref" ] && echo diff || echo diagram)</summary>"
  echo
  language=mermaid
  if [ "$format" = "json" ]; then
    language=json
  fi
  fence='````'
  echo "${fence}${language}"
  cat "$output"
  echo "$fence"
  echo
  echo "</details>"
}

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  render_markdown >>"$GITHUB_STEP_SUMMARY"
fi

if [ "$comment" = "true" ]; then
  if [ -z "${PR_NUMBER:-}" ]; then
    echo "::warning::comment is enabled but this run is not a pull request"
  elif [ "$format" = "html" ]; then
    echo "::warning::HTML output is not commented on pull requests; use mermaid or json"
  else
    body_file="${RUNNER_TEMP:-/tmp}/infradiff-comment.md"
    {
      echo "<!-- infradiff -->"
      render_markdown
    } >"$body_file"
    gh pr comment "$PR_NUMBER" --body-file "$body_file" ||
      echo "::warning::failed to comment on pull request $PR_NUMBER"
  fi
fi

exit "$status"
