#!/usr/bin/env bash

set -euo pipefail

script_dir="$(dirname -- "${BASH_SOURCE[0]}")"
script_dir="$(cd -- "$script_dir" && pwd -P)"
# shellcheck source=./common.sh
source "$script_dir/common.sh"

host=""
repo_arg="/etc/nixos"
marker_path="/var/lib/config-nix/finalize-pending"

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

repo_root="$(normalize_repo_root "$repo_arg")"
ensure_flake_repo "$repo_root"
[[ -f "$marker_path" ]] || die "finalize marker not found: $marker_path"

install_plan_json="$(load_install_plan_json "$repo_root" "$host")"
meta_json="$(load_host_meta_json "$repo_root" "$host")"

needs_finalize="$(printf '%s' "$install_plan_json" | jq -r '.needsFinalize')"
[[ "$needs_finalize" == "true" ]] || {
  printf 'No deferred finalization is required for host %s\n' "$host"
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

if [[ "$secure_boot_deferred" == "true" ]]; then
  install -d -m 0700 "$pki_bundle"
  sbctl_config="$(mktemp)"
  write_sbctl_config "$sbctl_config" "$pki_bundle"

  if [[ ! -f "$pki_bundle/keys/db/db.key" ]]; then
    sbctl --config "$sbctl_config" create-keys
  fi
fi

/run/current-system/sw/bin/nixos-rebuild boot --flake "path:${repo_root}#${final_output}"

if [[ "$secure_boot_deferred" == "true" ]]; then
  sbctl --config "$sbctl_config" enroll-keys --microsoft
fi

rm -f "$marker_path"
sync
systemctl --no-block reboot
