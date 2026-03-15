#!/usr/bin/env bash

set -euo pipefail

append_nix_config() {
  local config_line="$1"

  if [[ -n "${NIX_CONFIG:-}" ]]; then
    export NIX_CONFIG="${NIX_CONFIG}"$'\n'"${config_line}"
  else
    export NIX_CONFIG="${config_line}"
  fi
}

append_nix_config "experimental-features = nix-command flakes"

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
  [[ -f "$repo_root/flake.nix" ]] || die "repo checkout not found at $repo_root"
}

flake_ref_for_repo() {
  local repo_root="$1"
  printf 'path:%s\n' "$repo_root"
}

normalize_repo_root() {
  local repo_root="$1"
  realpath -m "$repo_root"
}

assert_expected_repo_revision() {
  local repo_root="$1"
  local expected_rev="${CONFIG_NIX_BOOTSTRAP_REV:-}"
  local actual_rev=""

  [[ -n "$expected_rev" ]] || return 0
  [[ -d "$repo_root/.git" ]] || return 0

  actual_rev="$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || true)"
  [[ -n "$actual_rev" ]] || return 0
  [[ "$actual_rev" == "$expected_rev" ]] && return 0

  die "existing checkout at $repo_root is at $actual_rev but installer expects $expected_rev; remove that directory or pass a fresh --repo path"
}

bootstrap_repo_checkout() {
  local repo_root="$1"
  local repo_parent

  repo_parent="$(dirname "$repo_root")"
  mkdir -p "$repo_parent"

  if [[ -n "${CONFIG_NIX_BOOTSTRAP_REPO_URL:-}" ]]; then
    printf 'Bootstrapping writable repository checkout at %s\n' "$repo_root" >&2
    git clone "$CONFIG_NIX_BOOTSTRAP_REPO_URL" "$repo_root"

    if [[ -n "${CONFIG_NIX_BOOTSTRAP_REV:-}" ]]; then
      git -C "$repo_root" checkout "$CONFIG_NIX_BOOTSTRAP_REV"
    fi

    return 0
  fi

  if [[ -n "${CONFIG_NIX_FLAKE_SOURCE:-}" ]]; then
    printf 'Bootstrapping writable source copy at %s\n' "$repo_root" >&2
    mkdir -p "$repo_root"
    cp -R --no-preserve=mode,ownership "$CONFIG_NIX_FLAKE_SOURCE"/. "$repo_root"/
    chmod -R u+w "$repo_root"
    return 0
  fi

  die "cannot bootstrap a writable repo checkout automatically; provide --repo <path-to-checkout>"
}

prepare_repo_root() {
  local requested_repo_root="${1:-}"
  local repo_root=""

  if [[ -n "$requested_repo_root" ]]; then
    repo_root="$requested_repo_root"
  elif git rev-parse --show-toplevel >/dev/null 2>&1; then
    repo_root="$(git rev-parse --show-toplevel)"
  else
    repo_root="$(pwd -P)/config.nix"
  fi
  repo_root="$(normalize_repo_root "$repo_root")"

  if [[ -f "$repo_root/flake.nix" ]]; then
    assert_expected_repo_revision "$repo_root"
    printf '%s\n' "$repo_root"
    return 0
  fi

  if [[ -e "$repo_root" && ! -d "$repo_root" ]]; then
    die "$repo_root exists and is not a directory"
  fi

  if [[ -d "$repo_root" ]]; then
    if find "$repo_root" -mindepth 1 -maxdepth 1 -print -quit >/dev/null 2>&1 \
      && [[ -n "$(find "$repo_root" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]]; then
      die "$repo_root exists but is not a config.nix checkout; pass --repo to an existing checkout or an empty target directory"
    fi
    rmdir "$repo_root" 2>/dev/null || true
  fi

  bootstrap_repo_checkout "$repo_root"
  ensure_flake_repo "$repo_root"
  assert_expected_repo_revision "$repo_root"
  printf '%s\n' "$repo_root"
}

load_host_meta_json() {
  local repo_root="$1"
  local host="$2"
  nix --extra-experimental-features 'nix-command flakes' eval --json "$(flake_ref_for_repo "$repo_root")#lib.hostMeta.${host}"
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

sops_in_repo() {
  local repo_root="$1"
  shift

  sops --config "$repo_root/.sops.yaml" "$@"
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
