#!/usr/bin/env bash

set -euo pipefail

script_dir="$(dirname -- "${BASH_SOURCE[0]}")"
script_dir="$(cd -- "$script_dir" && pwd -P)"
# shellcheck source=./common.sh
source "$script_dir/common.sh"

host=""
disk=""
repo_arg=""
assume_yes=0
mount_point="/mnt"

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --host)
      host="$2"
      shift 2
      ;;
    --disk)
      disk="$2"
      shift 2
      ;;
    --repo)
      repo_arg="$2"
      shift 2
      ;;
    --yes)
      assume_yes=1
      shift
      ;;
    --mount-point)
      mount_point="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$host" ]] || die "usage: nix run .#install-host -- --host <name> --disk <device> [--repo /path/to/checkout] [--yes]"
[[ -n "$disk" ]] || die "usage: nix run .#install-host -- --host <name> --disk <device> [--repo /path/to/checkout] [--yes]"

require_tools=(age-keygen disko jq mkpasswd nix sops nixos-generate-config nixos-install)
for tool in "${require_tools[@]}"; do
  command -v "$tool" >/dev/null 2>&1 || die "required command not found: $tool"
done

repo_root="$(prepare_repo_root "$repo_arg")"

meta_json="$(load_host_meta_json "$repo_root" "$host")"
assert_owner_recipients_ready "$host" "$meta_json"

host_dir="$repo_root/hosts/$host"
[[ -d "$host_dir" ]] || die "unknown host: $host"

user_name="$(printf '%s' "$meta_json" | jq -r '.user.name')"
host_secret_file="$repo_root/secrets/hosts/${host}.yaml"
host_pub_file="$repo_root/secrets/hosts/${host}.age.pub"
mkdir -p "$(dirname "$host_secret_file")"

confirm_disk_wipe "$disk" "$assume_yes"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

cat > "$tmpdir/disko-config.nix" <<EOF
(import "${repo_root}/hosts/${host}/disko.nix" {
  diskDevice = "${disk}";
})
EOF

mkdir -p "$mount_point"
export DISKO_ROOT_MOUNTPOINT="$mount_point"
disko --mode destroy,format,mount "$tmpdir/disko-config.nix"

nixos-generate-config --root "$mount_point" --no-filesystems --show-hardware-config \
  > "$host_dir/hardware-configuration.nix"

install -d -m 0700 "$mount_point/var/lib/sops-nix"
age-keygen -o "$mount_point/var/lib/sops-nix/key.txt" >/dev/null
chmod 600 "$mount_point/var/lib/sops-nix/key.txt"
age-keygen -y "$mount_point/var/lib/sops-nix/key.txt" > "$host_pub_file"

render_sops_config "$repo_root"

if [[ ! -f "$host_secret_file" ]]; then
  while true; do
    read -r -s -p "Initial password for ${user_name}: " password_one
    printf '\n' >&2
    read -r -s -p "Confirm password: " password_two
    printf '\n' >&2

    [[ -n "$password_one" ]] || {
      printf 'Password cannot be empty.\n' >&2
      continue
    }

    [[ "$password_one" == "$password_two" ]] || {
      printf 'Passwords do not match.\n' >&2
      continue
    }

    break
  done

  password_hash="$(printf '%s\n' "$password_one" | mkpasswd --method=yescrypt --stdin)"
  unset password_one password_two

  cat > "$host_secret_file" <<EOF
userPasswordHash: "$password_hash"
EOF

  sops_in_repo "$repo_root" --encrypt --in-place "$host_secret_file"
else
  if is_sops_file "$host_secret_file"; then
    sops_in_repo "$repo_root" updatekeys -y "$host_secret_file"
  else
    sops_in_repo "$repo_root" --encrypt --in-place "$host_secret_file"
  fi
fi

if is_sops_file "$repo_root/secrets/common.yaml"; then
  sops_in_repo "$repo_root" updatekeys -y "$repo_root/secrets/common.yaml"
fi

nixos-install --root "$mount_point" --flake "$repo_root#${host}"

printf '\nInstall completed for host %s.\n' "$host"
printf 'Generated or updated:\n'
printf '  - hosts/%s/hardware-configuration.nix\n' "$host"
printf '  - secrets/hosts/%s.age.pub\n' "$host"
printf '  - secrets/hosts/%s.yaml\n' "$host"
printf '  - .sops.yaml\n'
printf 'Commit those files after first boot.\n'
