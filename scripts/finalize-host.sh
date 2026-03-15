#!/usr/bin/env bash

set -euo pipefail

script_dir="$(dirname -- "${BASH_SOURCE[0]}")"
script_dir="$(cd -- "$script_dir" && pwd -P)"
# shellcheck source=./common.sh
source "$script_dir/common.sh"

set -E

host=""
repo_arg="/etc/nixos"
marker_path="/var/lib/config-nix/finalize-pending"
status_path="/var/lib/config-nix/finalize-status.json"

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --host)
      host="$2"
      shift 2
      ;;
    --repo)
      repo_arg="$2"
      shift 2
      ;;
    --marker-path)
      marker_path="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

write_finalize_status() {
  local status="$1"
  local stage="$2"
  local message="${3:-}"
  local payload=""

  payload="$(jq -n \
    --arg host "$host" \
    --arg status "$status" \
    --arg stage "$stage" \
    --arg message "$message" \
    --arg updatedAt "$(date -Is)" \
    '{
      host: $host,
      status: $status,
      stage: $stage,
      message: $message,
      updatedAt: $updatedAt
    }'
  )"

  write_json_file "$status_path" "$payload"
}

on_finalize_error() {
  local exit_code="$?"
  write_finalize_status "failed" "error" "finalization failed with exit code ${exit_code}"
  exit "$exit_code"
}

if [[ -z "$host" ]]; then
  if command -v hostnamectl >/dev/null 2>&1; then
    host="$(hostnamectl --static)"
  else
    host="$(hostname -s)"
  fi
fi

require_tools=(jq nix sbctl sync systemctl)
for tool in "${require_tools[@]}"; do
  command -v "$tool" >/dev/null 2>&1 || die "required command not found: $tool"
done
[[ -x /run/current-system/sw/bin/nixos-rebuild ]] || die "nixos-rebuild not found at /run/current-system/sw/bin/nixos-rebuild"

trap on_finalize_error ERR

repo_root="$(normalize_repo_root "$repo_arg")"
ensure_flake_repo "$repo_root"
[[ -f "$marker_path" ]] || die "finalize marker not found: $marker_path"

install_plan_json="$(load_install_plan_json "$repo_root" "$host")"
meta_json="$(load_host_meta_json "$repo_root" "$host")"

needs_finalize="$(printf '%s' "$install_plan_json" | jq -r '.needsFinalize')"
[[ "$needs_finalize" == "true" ]] || {
  printf 'No deferred finalization is required for host %s\n' "$host"
  write_finalize_status "skipped" "completed" "no deferred finalization required"
  exit 0
}

final_output="$(printf '%s' "$install_plan_json" | jq -r '.finalOutput')"
secure_boot_deferred="$(printf '%s' "$install_plan_json" | jq -e '.deferredFeatures | index("secure-boot") != null' >/dev/null && printf 'true' || printf 'false')"
pki_bundle="$(printf '%s' "$meta_json" | jq -r '.boot.secureBoot.pkiBundle // "/var/lib/sbctl"')"

sbctl_config=""
cleanup() {
  if [[ -n "$sbctl_config" && -f "$sbctl_config" ]]; then
    rm -f "$sbctl_config"
  fi
}
trap cleanup EXIT

write_finalize_status "running" "prepare" "starting deferred host finalization"

if [[ "$secure_boot_deferred" == "true" ]]; then
  printf 'Preparing Secure Boot key material under %s\n' "$pki_bundle"
  install -d -m 0700 "$pki_bundle"
  sbctl_config="$(mktemp)"
  write_sbctl_config "$sbctl_config" "$pki_bundle"

  if [[ ! -f "$pki_bundle/keys/db/db.key" ]]; then
    write_finalize_status "running" "secure-boot-keys" "creating Secure Boot keys"
    sbctl --config "$sbctl_config" create-keys
  fi
fi

write_finalize_status "running" "rebuild" "building the final signed system profile"
/run/current-system/sw/bin/nixos-rebuild boot --flake "path:${repo_root}#${final_output}"

if [[ "$secure_boot_deferred" == "true" ]]; then
  write_finalize_status "running" "secure-boot-enroll" "enrolling Secure Boot keys"
  sbctl --config "$sbctl_config" enroll-keys --microsoft
fi

rm -f "$marker_path"
write_finalize_status "success" "completed" "finalization completed successfully; rebooting"
sync
systemctl --no-block reboot
