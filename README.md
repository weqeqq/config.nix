# NixOS Config

Single-flake NixOS + Home Manager repo with one shared profile, cached machine detection, `Hyprland`, `disko`, `sops-nix`, and an install TUI written in Go.

## Model

- Shared declarative settings live in [settings.nix](/home/weqeq/Projects/config.nix/settings.nix).
- The installer detects whether the target is a VM, has `NVIDIA`, has `AMD`, or should stay generic.
- That detected state is cached locally in `/etc/nixos/local/` and is reused on rebuilds.
- Runtime user credentials are derived into `/etc/nixos/local/runtime-secrets.nix`.
- The repo itself stays machine-agnostic. There are no per-host config directories.

Main flake outputs:

- `nixosConfigurations.default`
- `nixosConfigurations.default-install`
- `homeConfigurations.default`
- `packages.x86_64-linux.install-system`
- `packages.x86_64-linux.finalize-system`
- `packages.x86_64-linux.rebuild-system`
- `packages.x86_64-linux.rekey-system`

## Layout

```text
.
├── flake.nix
├── settings.nix
├── disko.nix
├── home/
├── lib/
├── modules/
├── installer/
├── secrets/
└── .github/workflows/
```

## Before first install

Edit [settings.nix](/home/weqeq/Projects/config.nix/settings.nix):

- `ownerAgeRecipients`
- `user.openssh.authorizedKeys`
- `user.name`, locale, timezone, hostname prefix if needed
- `boot.secureBoot.enable` to the final desired state

Useful owner key commands:

```bash
ssh-to-age < ~/.ssh/id_ed25519.pub
```

or:

```bash
age-keygen -y ~/.config/sops/age/keys.txt
```

## Install

Boot the official NixOS minimal ISO in UEFI mode, bring up networking, then run:

```bash
nix --extra-experimental-features 'nix-command flakes' run github:weqeqq/config.nix
```

The Go TUI asks only for runtime install data:

- target disk
- age key path only if an existing encrypted shared user secret cannot be decrypted automatically
- initial user password only if `secrets/user.yaml` does not already decrypt
- `cryptroot` passphrase
- final destructive confirmation

The installer automatically:

- bootstraps or reuses a writable git checkout
- detects platform and graphics
- writes `/etc/nixos/local/machine-state.nix`
- writes `/etc/nixos/local/hardware-configuration.nix`
- writes `/etc/nixos/local/runtime-secrets.nix`
- renders `.sops.yaml`
- updates or creates `secrets/user.yaml`
- copies a full git checkout to `/etc/nixos`
- installs `default` or `default-install` depending on deferred features

Local machine state is intentionally untracked. Generated secret changes and `.sops.yaml` stay visible in `git status`.

## Secure Boot

Secure Boot is two-phase:

1. Keep `boot.secureBoot.enable = true` in [settings.nix](/home/weqeq/Projects/config.nix/settings.nix).
2. The installer chooses `default-install`.
3. First boot runs `config-nix-finalize.service`.
4. The finalizer creates keys if needed, builds the final signed profile, enrolls keys, and reboots.

Manual retry:

```bash
sudo /run/current-system/sw/bin/finalize-system --repo /etc/nixos
```

Finalization state is written under `/var/lib/config-nix/`.

## Day-2 usage

Rebuild the installed system:

```bash
sudo /run/current-system/sw/bin/rebuild-system
```

Pass a different `nixos-rebuild` action if needed:

```bash
sudo /run/current-system/sw/bin/rebuild-system -- boot
```

Build Home Manager only:

```bash
nix build /etc/nixos#homeConfigurations.default.activationPackage
```

Update inputs:

```bash
nix flake update /etc/nixos
```

Review repo changes:

```bash
git -C /etc/nixos status
```

## Packages and config

Shared system packages and defaults:

- [modules/nixos/base.nix](/home/weqeq/Projects/config.nix/modules/nixos/base.nix)
- [modules/nixos/graphics.nix](/home/weqeq/Projects/config.nix/modules/nixos/graphics.nix)
- [modules/nixos/hyprland.nix](/home/weqeq/Projects/config.nix/modules/nixos/hyprland.nix)

Shared Home Manager config:

- [home/default.nix](/home/weqeq/Projects/config.nix/home/default.nix)
- [home/packages.nix](/home/weqeq/Projects/config.nix/home/packages.nix)
- [modules/home/base.nix](/home/weqeq/Projects/config.nix/modules/home/base.nix)

Disk layout:

- [disko.nix](/home/weqeq/Projects/config.nix/disko.nix)

## Secrets

Shared secrets:

- [secrets/common.yaml](/home/weqeq/Projects/config.nix/secrets/common.yaml)
- `secrets/user.yaml`

Edit them with `sops`:

```bash
sops /etc/nixos/secrets/user.yaml
```

Rekey:

```bash
nix run /etc/nixos#rekey-system -- --repo /etc/nixos
```

## Notes

- The installer detects hardware once and caches it; rebuilds do not redetect automatically.
- If hardware changes later, regenerate the local state by reinstalling or by extending the Go tooling with an explicit refresh flow.
- GitHub Actions runs `nix flake check`.
