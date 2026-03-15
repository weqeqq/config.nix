#!/usr/bin/env bash

set -euo pipefail

script_dir="$(dirname -- "${BASH_SOURCE[0]}")"
script_dir="$(cd -- "$script_dir" && pwd -P)"
# shellcheck source=./common.sh
source "$script_dir/common.sh"

host=""
disk=""
repo_arg=""
age_key_file=""
assume_yes=0
mount_point="/mnt"
non_interactive=0

repo_root=""
meta_json=""
install_plan_json=""
user_name=""
initial_output=""
final_output=""
needs_finalize=""
host_secret_file=""
host_pub_file=""
host_dir=""
password_hash=""
secret_mode=""
interactive_mode=0
install_receipt_file=""
finalize_status_file=""
invoked_from_git_checkout=0

cleanup_paths=()

usage() {
  cat >&2 <<'EOF'
usage: nix run .#install-host -- [--host <name>] [--disk <device>] [--age-key-file /path/to/keys.txt] [--yes] [--non-interactive]
EOF
}

register_cleanup_path() {
  cleanup_paths+=("$1")
}

cleanup() {
  local path=""

  for path in "${cleanup_paths[@]}"; do
    rm -rf "$path"
  done
}

trap cleanup EXIT

ui_supports_tui() {
  command -v gum >/dev/null 2>&1 && command -v fzf >/dev/null 2>&1
}

ui_heading() {
  local title="$1"
  local body="${2:-}"

  if [[ "$interactive_mode" -eq 1 ]]; then
    if [[ -n "$body" ]]; then
      gum style --border rounded --border-foreground 212 --padding "1 2" --margin "1 0" --bold \
        "${title}"$'\n'"${body}"
    else
      gum style --border rounded --border-foreground 212 --padding "1 2" --margin "1 0" --bold "${title}"
    fi
    return 0
  fi

  printf '\n== %s ==\n' "$title"
  if [[ -n "$body" ]]; then
    printf '%s\n' "$body"
  fi
}

ui_note() {
  local message="$1"

  if [[ "$interactive_mode" -eq 1 ]]; then
    gum style --foreground 245 "$message"
  else
    printf '%s\n' "$message"
  fi
}

ui_warn() {
  local message="$1"

  if [[ "$interactive_mode" -eq 1 ]]; then
    gum style --foreground 214 --bold "$message" >&2
  else
    printf 'warning: %s\n' "$message" >&2
  fi
}

ui_step() {
  local message="$1"

  if [[ "$interactive_mode" -eq 1 ]]; then
    gum style --foreground 45 --bold "$message"
  else
    printf '%s\n' "$message"
  fi
}

flake_revision_label() {
  if [[ -d "$repo_root/.git" ]]; then
    git -C "$repo_root" rev-parse --short HEAD 2>/dev/null || printf 'unknown'
    return 0
  fi

  if [[ -n "${CONFIG_NIX_BOOTSTRAP_REV:-}" ]]; then
    printf '%s\n' "${CONFIG_NIX_BOOTSTRAP_REV:0:12}"
    return 0
  fi

  printf 'unknown\n'
}

repo_source_label() {
  if [[ -n "$repo_arg" ]]; then
    printf 'explicit checkout: %s\n' "$repo_root"
    return 0
  fi

  if git rev-parse --show-toplevel >/dev/null 2>&1; then
    printf 'local checkout: %s\n' "$repo_root"
    return 0
  fi

  printf 'temporary bootstrap checkout: %s\n' "$repo_root"
}

preflight_checks() {
  [[ -d /sys/firmware/efi ]] || die "installer requires UEFI mode; /sys/firmware/efi is missing"

  if [[ -z "$repo_arg" ]] && ! git rev-parse --show-toplevel >/dev/null 2>&1 && [[ -n "${CONFIG_NIX_BOOTSTRAP_REPO_URL:-}" ]]; then
    git ls-remote "${CONFIG_NIX_BOOTSTRAP_REPO_URL}" HEAD >/dev/null 2>&1 \
      || die "remote bootstrap repo ${CONFIG_NIX_BOOTSTRAP_REPO_URL} is not reachable; connect networking first"
  fi
}

show_preflight_summary() {
  local revision=""

  revision="$(flake_revision_label)"
  ui_heading "config.nix installer" "$(printf 'UEFI: yes\nRevision: %s\nRepository: %s' "$revision" "$(repo_source_label)")"
}

select_host_if_needed() {
  local available_hosts=()

  if [[ -n "$host" ]]; then
    return 0
  fi

  mapfile -t available_hosts < <(list_host_names "$repo_root")
  [[ "${#available_hosts[@]}" -gt 0 ]] || die "no installable hosts found under $repo_root/hosts"

  if [[ "${#available_hosts[@]}" -eq 1 ]]; then
    host="${available_hosts[0]}"
    ui_note "Only one host profile is available; using ${host}."
    return 0
  fi

  [[ "$interactive_mode" -eq 1 ]] || die "non-interactive mode requires --host"

  host="$(printf '%s\n' "${available_hosts[@]}" | gum choose --header "Select the host profile to install")" \
    || die "host selection aborted"
}

load_host_context() {
  meta_json="$(load_host_meta_json "$repo_root" "$host")"
  install_plan_json="$(load_install_plan_json "$repo_root" "$host")"
  assert_owner_recipients_ready "$host" "$meta_json"

  host_dir="$repo_root/hosts/$host"
  [[ -d "$host_dir" ]] || die "unknown host: $host"

  user_name="$(printf '%s' "$meta_json" | jq -r '.user.name')"
  initial_output="$(printf '%s' "$install_plan_json" | jq -r '.initialOutput')"
  final_output="$(printf '%s' "$install_plan_json" | jq -r '.finalOutput')"
  needs_finalize="$(printf '%s' "$install_plan_json" | jq -r '.needsFinalize')"
  host_secret_file="$repo_root/secrets/hosts/${host}.yaml"
  host_pub_file="$repo_root/secrets/hosts/${host}.age.pub"
  mkdir -p "$(dirname "$host_secret_file")"
}

build_disk_choices() {
  local boot_disk=""
  local boot_disk_real=""
  local line=""
  local actual_path=""
  local actual_real=""
  local preferred_path=""
  local size=""
  local model=""
  local serial=""
  local transport=""
  local mountpoints=""
  local description=""

  boot_disk="$(live_media_disk 2>/dev/null || true)"
  boot_disk_real="$(realpath -e "$boot_disk" 2>/dev/null || true)"

  while IFS= read -r line; do
    actual_path="$(printf '%s' "$line" | jq -r '.path')"
    actual_real="$(realpath -e "$actual_path" 2>/dev/null || true)"
    [[ -n "$actual_real" ]] || continue

    if [[ -n "$boot_disk_real" && "$actual_real" == "$boot_disk_real" ]]; then
      continue
    fi

    preferred_path="$(preferred_disk_path "$actual_path")"
    size="$(printf '%s' "$line" | jq -r '.size // "?"')"
    model="$(printf '%s' "$line" | jq -r '([.vendor, .model] | map(select(. != null and . != "")) | join(" "))')"
    serial="$(printf '%s' "$line" | jq -r '.serial // ""')"
    transport="$(printf '%s' "$line" | jq -r '.tran // ""')"
    mountpoints="$(printf '%s' "$line" | jq -r '(.mountpoints // []) | map(select(. != null and . != "")) | join(", ")')"

    [[ -n "$model" ]] || model="Unknown device"
    [[ -n "$transport" ]] || transport="unknown"
    [[ -n "$serial" ]] || serial="no-serial"

    description="$(printf '%s | %s | %s | %s' "$size" "$transport" "$model" "$serial")"
    if [[ -n "$mountpoints" ]]; then
      description="${description} | mounted: ${mountpoints}"
    fi

    printf '%s\t%s\n' "$preferred_path" "$description"
  done < <(
    lsblk -J -o NAME,PATH,SIZE,TYPE,MODEL,VENDOR,SERIAL,TRAN,MOUNTPOINTS \
      | jq -c '.blockdevices[] | select(.type == "disk")'
  )
}

assert_safe_install_disk() {
  local selected_disk="$1"
  local selected_real=""
  local boot_disk=""
  local boot_real=""

  [[ -b "$selected_disk" || -L "$selected_disk" ]] || die "selected disk is not a block device: $selected_disk"

  selected_real="$(realpath -e "$selected_disk" 2>/dev/null || true)"
  [[ -n "$selected_real" ]] || die "cannot resolve selected disk: $selected_disk"

  boot_disk="$(live_media_disk 2>/dev/null || true)"
  boot_real="$(realpath -e "$boot_disk" 2>/dev/null || true)"

  if [[ -n "$boot_real" && "$selected_real" == "$boot_real" ]]; then
    die "selected disk $selected_disk appears to be the current boot medium; pick a different target disk"
  fi
}

select_disk_if_needed() {
  local selected=""

  if [[ -n "$disk" ]]; then
    disk="$(preferred_disk_path "$disk")"
    assert_safe_install_disk "$disk"
    return 0
  fi

  [[ "$interactive_mode" -eq 1 ]] || die "non-interactive mode requires --disk"

  if live_media_disk >/dev/null 2>&1; then
    ui_note "The current live boot medium is hidden from the disk picker."
  fi

  selected="$(
    build_disk_choices \
      | fzf --delimiter=$'\t' --with-nth=2.. --prompt='Disk > ' --height=40% --reverse \
          --header='Select the installation disk'
  )" || die "disk selection aborted"

  disk="$(printf '%s' "$selected" | awk -F'\t' '{print $1}')"
  [[ -n "$disk" ]] || die "no disk selected"
  assert_safe_install_disk "$disk"
}

prompt_password_hash() {
  local password_one=""
  local password_two=""

  while true; do
    if [[ "$interactive_mode" -eq 1 ]]; then
      password_one="$(gum input --password --placeholder "Initial password for ${user_name}")" \
        || die "password input aborted"
      password_two="$(gum input --password --placeholder "Confirm password")" \
        || die "password confirmation aborted"
    else
      read -r -s -p "Initial password for ${user_name}: " password_one
      printf '\n' >&2
      read -r -s -p "Confirm password: " password_two
      printf '\n' >&2
    fi

    [[ -n "$password_one" ]] || {
      ui_warn "Password cannot be empty."
      continue
    }

    [[ "$password_one" == "$password_two" ]] || {
      ui_warn "Passwords do not match."
      continue
    }

    printf '%s\n' "$password_one" | mkpasswd --method=yescrypt --stdin
    return 0
  done
}

resolve_existing_secret_mode() {
  local suggested_key=""
  local choice=""
  local key_path=""

  if [[ ! -f "$host_secret_file" ]]; then
    secret_mode="create"
    return 0
  fi

  if ! is_sops_file "$host_secret_file"; then
    secret_mode="reuse"
    return 0
  fi

  if sops_can_decrypt "$repo_root" "$host_secret_file"; then
    secret_mode="reuse"
    return 0
  fi

  [[ "$interactive_mode" -eq 1 ]] || die "existing encrypted secret found at $host_secret_file, but no age key could decrypt it; pass --age-key-file or replace the secret manually"

  while true; do
    suggested_key="$(default_age_key_file || true)"

    ui_warn "An encrypted host secret already exists for ${host}, but the current age key cannot decrypt it."
    choice="$(printf '%s\n' \
      "Provide an age key path" \
      "Replace the host secret with a new password" \
      "Abort" \
      | gum choose --header "Choose how to continue")" || die "secret resolution aborted"

    case "$choice" in
      "Provide an age key path")
        key_path="$(gum input --placeholder "/path/to/keys.txt" --value "$suggested_key")" \
          || die "age key prompt aborted"
        [[ -n "$key_path" ]] || {
          ui_warn "Age key path cannot be empty."
          continue
        }

        prepare_sops_age_key "$key_path"
        if sops_can_decrypt "$repo_root" "$host_secret_file"; then
          secret_mode="reuse"
          return 0
        fi

        ui_warn "That age key could not decrypt ${host_secret_file}."
        ;;
      "Replace the host secret with a new password")
        clear_sops_age_key
        secret_mode="replace"
        return 0
        ;;
      "Abort")
        die "aborted"
        ;;
    esac
  done
}

resolve_secret_inputs() {
  prepare_sops_age_key "$age_key_file"
  resolve_existing_secret_mode

  case "$secret_mode" in
    create|replace)
      password_hash="$(prompt_password_hash)"
      ;;
    reuse)
      password_hash=""
      ;;
    *)
      die "unexpected secret mode: $secret_mode"
      ;;
  esac
}

show_install_summary() {
  local secure_boot_note=""
  local age_key_note=""

  if [[ "$needs_finalize" == "true" ]]; then
    secure_boot_note="yes"
  else
    secure_boot_note="no"
  fi

  if [[ -n "${SOPS_AGE_KEY_FILE:-}" ]]; then
    age_key_note="${SOPS_AGE_KEY_FILE}"
  else
    age_key_note="not needed"
  fi

  ui_heading "Install summary" "$(printf 'Host: %s\nUser: %s\nDisk: %s\nInitial output: %s\nFinal output: %s\nNeeds first-boot finalization: %s\nAge key: %s' \
    "$host" "$user_name" "$disk" "$initial_output" "$final_output" "$secure_boot_note" "$age_key_note")"
}

confirm_install_with_ui() {
  local answer=""

  if [[ "$assume_yes" -eq 1 ]]; then
    return 0
  fi

  if [[ "$interactive_mode" -eq 1 ]]; then
    show_install_summary
    answer="$(gum input --placeholder "Type erase to continue")" || die "confirmation aborted"
    [[ "$answer" == "erase" ]] || die "aborted"
    return 0
  fi

  confirm_disk_wipe "$disk" "$assume_yes"
}

write_host_secret() {
  case "$secret_mode" in
    create|replace)
      cat > "$host_secret_file" <<EOF
userPasswordHash: "$password_hash"
EOF
      sops_in_repo "$repo_root" --encrypt --in-place "$host_secret_file"
      ;;
    reuse)
      if is_sops_file "$host_secret_file"; then
        sops_in_repo "$repo_root" updatekeys -y "$host_secret_file"
      else
        sops_in_repo "$repo_root" --encrypt --in-place "$host_secret_file"
      fi
      ;;
  esac
}

write_install_receipt_payload() {
  jq -n \
    --arg host "$host" \
    --arg disk "$disk" \
    --arg initialOutput "$initial_output" \
    --arg finalOutput "$final_output" \
    --arg repoPath "/etc/nixos" \
    --arg user "$user_name" \
    --arg installedAt "$(date -Is)" \
    --argjson needsFinalize "$(if [[ "$needs_finalize" == "true" ]]; then printf 'true'; else printf 'false'; fi)" \
    '{
      host: $host,
      disk: $disk,
      initialOutput: $initialOutput,
      finalOutput: $finalOutput,
      repoPath: $repoPath,
      user: $user,
      installedAt: $installedAt,
      needsFinalize: $needsFinalize
    }'
}

stage_install_artifacts() {
  local stage_paths=(
    ".sops.yaml"
    "hosts/${host}/hardware-configuration.nix"
    "secrets/hosts/${host}.age.pub"
    "secrets/hosts/${host}.yaml"
  )

  if [[ -f "$repo_root/secrets/common.yaml" ]]; then
    stage_paths+=("secrets/common.yaml")
  fi

  stage_paths_in_repo "$repo_root" "${stage_paths[@]}"
}

run_install() {
  local tmpdir=""
  local target_repo_root=""
  local state_dir=""
  local receipt_json=""

  tmpdir="$(mktemp -d)"
  register_cleanup_path "$tmpdir"

  ui_step "Preparing disko configuration"
  cat > "$tmpdir/disko-config.nix" <<EOF
(import "${repo_root}/hosts/${host}/disko.nix" {
  diskDevice = "${disk}";
})
EOF

  mkdir -p "$mount_point"
  export DISKO_ROOT_MOUNTPOINT="$mount_point"

  ui_step "Partitioning and mounting ${disk}"
  disko --mode destroy,format,mount "$tmpdir/disko-config.nix"

  ui_step "Generating hardware configuration"
  nixos-generate-config --root "$mount_point" --no-filesystems --show-hardware-config \
    > "$host_dir/hardware-configuration.nix"

  ui_step "Generating host sops key"
  install -d -m 0700 "$mount_point/var/lib/sops-nix"
  age-keygen -o "$mount_point/var/lib/sops-nix/key.txt" >/dev/null
  chmod 600 "$mount_point/var/lib/sops-nix/key.txt"
  age-keygen -y "$mount_point/var/lib/sops-nix/key.txt" > "$host_pub_file"

  ui_step "Rendering sops config and host secret"
  render_sops_config "$repo_root"
  write_host_secret

  if is_sops_file "$repo_root/secrets/common.yaml"; then
    sops_in_repo "$repo_root" updatekeys -y "$repo_root/secrets/common.yaml"
  fi

  ensure_git_checkout "$repo_root"
  stage_install_artifacts

  target_repo_root="$mount_point/etc/nixos"
  ui_step "Persisting git checkout to /etc/nixos"
  copy_repo_snapshot "$repo_root" "$target_repo_root"

  state_dir="$mount_point/var/lib/config-nix"
  install -d -m 0700 "$state_dir"

  if [[ "$needs_finalize" == "true" ]]; then
    : > "$state_dir/finalize-pending"
  fi

  receipt_json="$(write_install_receipt_payload)"
  install_receipt_file="${state_dir}/install-receipt.json"
  finalize_status_file="${state_dir}/finalize-status.json"
  write_json_file "$install_receipt_file" "$receipt_json"
  if [[ "$needs_finalize" == "true" ]]; then
    write_json_file "$finalize_status_file" "$(jq -n --arg host "$host" --arg status "pending" --arg updatedAt "$(date -Is)" '{host: $host, status: $status, updatedAt: $updatedAt}')"
  fi

  ui_step "Installing NixOS (${initial_output})"
  nixos-install --root "$mount_point" --flake "path:${target_repo_root}#${initial_output}"
}

show_completion_receipt() {
  local finalize_message=""

  if [[ "$needs_finalize" == "true" ]]; then
    finalize_message="First boot will finalize the install, sign the final profile, and reboot once more."
  else
    finalize_message="No first-boot finalization is required."
  fi

  ui_heading "Install completed" "$(printf 'Host: %s\nInstalled output: %s\nFinal output: %s\nCanonical repo: /etc/nixos\nReceipt: /var/lib/config-nix/install-receipt.json\n%s\n\nBefore rebooting, remove the ISO or move the disk above the ISO in firmware boot order.' \
    "$host" "$initial_output" "$final_output" "$finalize_message")"

  ui_note "Generated files are staged in /etc/nixos. Commit them from the installed system after the first successful boot."
}

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
    --age-key-file)
      age_key_file="$2"
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
    --non-interactive)
      non_interactive=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

require_tools=(age-keygen disko findmnt git jq lsblk mkpasswd nix sops tar nixos-generate-config nixos-install)
for tool in "${require_tools[@]}"; do
  command -v "$tool" >/dev/null 2>&1 || die "required command not found: $tool"
done

if [[ "$non_interactive" -eq 0 && -t 0 && -t 1 ]]; then
  interactive_mode=1
fi

if git rev-parse --show-toplevel >/dev/null 2>&1; then
  invoked_from_git_checkout=1
fi

if [[ "$interactive_mode" -eq 1 ]]; then
  if ! ui_supports_tui; then
    die "interactive mode requires gum and fzf; run through nix run or pass --non-interactive"
  fi
fi

preflight_checks
if [[ "$interactive_mode" -eq 1 ]]; then
  ui_heading "Preparing installer environment" "Checking UEFI mode and preparing a writable repository checkout."
fi

repo_root="$(prepare_install_repo_root "$repo_arg")"
if [[ -z "$repo_arg" && "$invoked_from_git_checkout" -eq 0 && "$repo_root" == /tmp/config-nix-install.* ]]; then
  register_cleanup_path "$repo_root"
fi

show_preflight_summary
select_host_if_needed
load_host_context
select_disk_if_needed
resolve_secret_inputs
confirm_install_with_ui
run_install
show_completion_receipt
