#!/usr/bin/env bash

set -euo pipefail

if [[ -n "${NIX_CONFIG:-}" ]]; then
  export NIX_CONFIG="${NIX_CONFIG}"$'\n'"experimental-features = nix-command flakes"
else
  export NIX_CONFIG="experimental-features = nix-command flakes"
fi

repo_root="${1:-$(pwd -P)}"
repo_flake_ref="path:${repo_root}"

host_meta_json="$(nix --extra-experimental-features 'nix-command flakes' eval --json "${repo_flake_ref}#lib.hostMeta")"
tmp_file="$(mktemp)"

mapfile -t common_keys < <(
  printf '%s' "$host_meta_json" \
    | jq -r '.[].ownerAgeRecipients[]? // empty' \
    | sed '/^$/d' \
    | sort -u
)

{
  printf 'creation_rules:\n'

  if [[ "${#common_keys[@]}" -gt 0 ]]; then
    printf '  - path_regex: ^secrets/common\\.yaml$\n'
    printf '    key_groups:\n'
    printf '      - age:\n'
    for key in "${common_keys[@]}"; do
      printf '          - %s\n' "$key"
    done
  fi

  while read -r host; do
    host_pub_file="$repo_root/secrets/hosts/${host}.age.pub"
    mapfile -t host_keys < <(
      printf '%s' "$host_meta_json" \
        | jq -r --arg host "$host" '.[$host].ownerAgeRecipients[]? // empty'
    )

    if [[ -f "$host_pub_file" ]]; then
      host_keys+=("$(tr -d '[:space:]' < "$host_pub_file")")
    fi

    mapfile -t host_keys < <(
      printf '%s\n' "${host_keys[@]}" \
        | sed '/^$/d' \
        | sort -u
    )

    if [[ "${#host_keys[@]}" -eq 0 ]]; then
      continue
    fi

    printf '  - path_regex: ^secrets/hosts/%s\\.yaml$\n' "$host"
    printf '    key_groups:\n'
    printf '      - age:\n'
    for key in "${host_keys[@]}"; do
      printf '          - %s\n' "$key"
    done
  done < <(printf '%s' "$host_meta_json" | jq -r 'keys[]')
} > "$tmp_file"

mv "$tmp_file" "$repo_root/.sops.yaml"
