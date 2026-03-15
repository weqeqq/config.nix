#!/usr/bin/env bash

set -euo pipefail

script_dir="$(dirname -- "${BASH_SOURCE[0]}")"
script_dir="$(cd -- "$script_dir" && pwd -P)"
# shellcheck source=./common.sh
source "$script_dir/common.sh"

host=""
host_key_file=""
repo_arg=""

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --host)
      host="$2"
      shift 2
      ;;
    --host-key-file)
      host_key_file="$2"
      shift 2
      ;;
    --repo)
      repo_arg="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$host" ]] || die "usage: nix run .#rekey-host -- --host <name> [--repo /path/to/checkout] [--host-key-file /path/to/key.txt]"

repo_root="$(prepare_repo_root "$repo_arg")"

meta_json="$(load_host_meta_json "$repo_root" "$host")"
assert_owner_recipients_ready "$host" "$meta_json"

host_pub_file="$repo_root/secrets/hosts/${host}.age.pub"
mkdir -p "$(dirname "$host_pub_file")"

if [[ ! -f "$host_pub_file" ]]; then
  if [[ -z "$host_key_file" && -f /var/lib/sops-nix/key.txt ]]; then
    host_key_file="/var/lib/sops-nix/key.txt"
  fi

  [[ -n "$host_key_file" ]] || die "host public key is missing; provide --host-key-file or run this command on the host"
  [[ -f "$host_key_file" ]] || die "host key file not found: $host_key_file"

  age-keygen -y "$host_key_file" > "$host_pub_file"
fi

render_sops_config "$repo_root"

for secret_file in \
  "$repo_root/secrets/common.yaml" \
  "$repo_root/secrets/hosts/${host}.yaml"
do
  if [[ ! -f "$secret_file" ]]; then
    continue
  fi

  if is_sops_file "$secret_file"; then
    sops updatekeys -y "$secret_file"
  else
    sops --encrypt --in-place "$secret_file"
  fi
done

printf 'Updated .sops.yaml and rekeyed secrets for host %s\n' "$host"
