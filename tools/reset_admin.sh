#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
project=${RESET_ADMIN_PROJECT:-openvms}
username=${OPENVMS_ADMIN_USERNAME:-admin}

if [[ ! "$project" =~ ^[a-zA-Z0-9][a-zA-Z0-9_-]*$ ]]; then
  printf 'Refusing unsafe Compose project name.\n' >&2
  exit 2
fi
if [[ ! "$username" =~ ^[a-zA-Z0-9_.-]+$ ]]; then
  printf 'Refusing unsafe admin username.\n' >&2
  exit 2
fi
if [[ ! -t 0 ]]; then
  printf 'Run this script from an interactive terminal.\n' >&2
  exit 2
fi

compose=(docker compose --project-name "$project" --env-file /dev/null -f "$root/docker-compose.yml")
api_container=$("${compose[@]}" ps -q api) || {
  printf 'Unable to locate the running API service for Compose project %q.\n' "$project" >&2
  exit 1
}
if [[ -z "$api_container" || "$api_container" == *$'\n'* ]]; then
  printf 'Expected exactly one running API service in Compose project %q.\n' "$project" >&2
  exit 1
fi

# The running API may use an image older than this checkout. Build the current
# CLI without restarting or replacing that service, then use the project's
# resolved image in an isolated one-off container.
if ! "${compose[@]}" build api >/dev/null; then
  printf 'Unable to build the API maintenance image from this checkout.\n' >&2
  exit 1
fi
images=$("${compose[@]}" config --images) || {
  printf 'Unable to resolve the API image for Compose project %q.\n' "$project" >&2
  exit 1
}
image=$(printf '%s\n' "$images" | awk -v expected="${project}-api" '$0 == expected { count++; image = $0 } END { if (count != 1) exit 1; print image }') || {
  printf 'The current API image is missing or ambiguous; no reset was attempted.\n' >&2
  exit 1
}
if [[ -z "$image" || "$image" == *$'\n'* ]] || ! docker image inspect "$image" >/dev/null 2>&1; then
  printf 'The current API image is missing or ambiguous; no reset was attempted.\n' >&2
  exit 1
fi
network_name="${project}_default"
network_id=$(docker inspect --format "{{with index .NetworkSettings.Networks \"$network_name\"}}{{.NetworkID}}{{end}}" "$api_container") || {
  printf 'Unable to resolve the API service network.\n' >&2
  exit 1
}
if [[ -z "$network_id" ]]; then
  printf 'The API service is not attached to its default Compose network.\n' >&2
  exit 1
fi
database_url=$(docker inspect --format '{{range .Config.Env}}{{if eq (printf "%.13s" .) "DATABASE_URL="}}{{println (slice . 13)}}{{end}}{{end}}' "$api_container") || {
  printf 'Unable to read the running API database configuration.\n' >&2
  exit 1
}
if [[ -z "$database_url" || "$database_url" == *$'\n'* ]]; then
  printf 'The running API database configuration is missing or ambiguous.\n' >&2
  exit 1
fi

printf 'Reset platform admin %q in Compose project %q. No volumes or data will be deleted.\n' "$username" "$project" >&2
IFS= read -r -s -p 'New password: ' password
printf '\n' >&2
IFS= read -r -s -p 'Confirm new password: ' confirmation
printf '\n' >&2
if [[ "$password" != "$confirmation" ]]; then
  unset password confirmation
  printf 'Passwords do not match. No changes made.\n' >&2
  exit 2
fi

# The rebuilt API image carries /vmsctl. Preserve the running API's database
# URL while running the one-off CLI without touching the service or its data.
# Do not require host Go or put the new password in args or environment.
if ! printf '%s\n' "$password" | DATABASE_URL="$database_url" docker run --rm -i --network "$network_id" --env DATABASE_URL --entrypoint /vmsctl "$image" reset-admin-password -user "$username" >/dev/null; then
  unset password confirmation
  printf 'Admin reset failed; Compose data was not deleted.\n' >&2
  exit 1
fi
unset password confirmation
printf 'Admin password reset for %q in Compose project %q; change it at next login.\n' "$username" "$project"
