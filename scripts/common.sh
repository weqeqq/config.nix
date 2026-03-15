#!/usr/bin/env bash

set -euo pipefail

export NIX_CONFIG="${NIX_CONFIG:+$NIX_CONFIG"$'\n'"}experimental-features = nix-command flakes"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

resolve_repo_root() {
  if git rev-parse --show-toplevel >/dev/null 2>&1; then
    git rev-parse --show-toplevel
  else
    pwd -P
  fi
}

ensure_flake_repo() {
  local repo_root="$1"
  [[ -f "$repo_root/flake.nix" ]] || die "run this command from the repository root"
}

load_host_meta_json() {
  local repo_root="$1"
  local host="$2"
  nix --extra-experimental-features 'nix-command flakes' eval --json "$repo_root#hostMeta.${host}"
}

assert_owner_recipients_ready() {
  local host="$1"
  local meta_json="$2"
  local placeholder

  placeholder="$(printf '%s' "$meta_json" | jq -r '.ownerAgeRecipients[]? | select(test("replace"; "i"))' || true)"
  [[ -z "$placeholder" ]] || die "replace ownerAgeRecipients in hosts/${host}/vars.nix before running this command"

  if [[ "$(printf '%s' "$meta_json" | jq '.ownerAgeRecipients | length')" -eq 0 ]]; then
    die "hosts/${host}/vars.nix must define at least one ownerAgeRecipients entry"
  fi
}

is_sops_file() {
  local file="$1"
  [[ -f "$file" ]] && grep -q '^sops:' "$file"
}

render_sops_config() {
  local repo_root="$1"
  bash "$repo_root/scripts/render-sops-config.sh" "$repo_root"
}

confirm_disk_wipe() {
  local disk="$1"
  local assume_yes="${2:-0}"

  if [[ "$assume_yes" -eq 1 ]]; then
    return 0
  fi

  printf 'This will erase %s. Type "erase" to continue: ' "$disk" >&2
  read -r answer
  [[ "$answer" == "erase" ]] || die "aborted"
}
