# NixOS Config

Single-flake NixOS + Home Manager repository for a desktop host with `NVIDIA`, `Hyprland`, `disko` and `sops-nix`.

## What is in this repo

- One root `flake.nix`.
- `nixosConfigurations.<host>` for full system builds.
- `homeConfigurations."<user>@<host>"` for standalone Home Manager builds.
- `disko` layout per host.
- `sops-nix` integration with encrypted secrets stored in the same repository.
- `nix run github:weqeqq/config.nix` for a minimal fullscreen Go installer from the official minimal ISO.

The current real hosts are:

- `desktop`: the main bare-metal profile with `NVIDIA`;
- `vm-test`: a VM-friendly smoke-test profile with `qemu-guest` and without `NVIDIA`.

The structure is already ready for more hosts and more users.

## Repository layout

```text
.
├── flake.nix
├── lib/
├── modules/
├── hosts/
│   ├── desktop/
│   └── _template/
├── homes/
│   ├── weqeq/
│   └── _template/
├── scripts/
├── secrets/
└── .github/workflows/
```

## Before the first install

Edit [hosts/desktop/vars.nix](/home/weqeq/Projects/config.nix/hosts/desktop/vars.nix) and [hosts/vm-test/vars.nix](/home/weqeq/Projects/config.nix/hosts/vm-test/vars.nix):

- replace `ownerAgeRecipients` with your real public `age` recipient;
- replace the placeholder SSH public key in `user.openssh.authorizedKeys`;
- change hostname, locale, timezone or username if needed.
- set `boot.secureBoot.enable` to the final desired state; the installer will pick an install-safe profile automatically when Secure Boot must be deferred.

Useful commands for the owner key:

```bash
ssh-to-age < ~/.ssh/id_ed25519.pub
```

or, if you already use an age key:

```bash
age-keygen -y ~/.config/sops/age/keys.txt
```

## Install on a new machine

1. Boot the official NixOS minimal ISO in UEFI mode.
2. Bring up networking.
3. Run the installer app.

```bash
nix --extra-experimental-features 'nix-command flakes' run github:weqeqq/config.nix
```

The installer opens a full-screen Go TUI. It is keyboard-first, centered, and intentionally minimal.

Primary keys:

- `Tab` and `Shift-Tab`: move focus between fields
- arrow keys: navigate host and disk lists
- `Enter`: continue
- `Esc`: go back on non-destructive steps
- `q`: quit before the install starts
- `l`: toggle phases vs raw logs during install

The wizard asks only for install-time inputs:

- host profile
- target disk
- age key path only when an existing encrypted host secret cannot be decrypted automatically
- initial password only when the host secret does not already decrypt
- final destructive confirmation

The default install screen shows only large phases. Raw command output is hidden unless you toggle it explicitly.

What the installer does:

- bootstraps a writable git checkout automatically if you launch it from `github:...`;
- evaluates `lib.installPlan.<host>` and picks either `<host>` or `<host>-install` automatically;
- wipes and partitions the disk with `disko`;
- mounts the target filesystem under `/mnt`;
- generates `hosts/<host>/hardware-configuration.nix`;
- creates `/var/lib/sops-nix/key.txt` on the target system;
- writes `secrets/hosts/<host>.age.pub` into the repo;
- regenerates `.sops.yaml`;
- prompts for the initial user password and stores its hash encrypted in `secrets/hosts/<host>.yaml`;
- stages generated install files in git;
- copies the resulting full git checkout into `/mnt/etc/nixos`;
- writes `/var/lib/config-nix/install-receipt.json`;
- writes a first-boot finalization marker if deferred features exist;
- runs `nixos-install` from `path:/mnt/etc/nixos`.

If `secrets/hosts/<host>.yaml` already exists and `sops` can decrypt it through `--age-key-file` or `SOPS_AGE_KEY_FILE`, the installer reuses the existing secret and skips the password prompt.

For `vm-test` the flow is the same, but the target profile enables `qemu-guest` integration and skips all `NVIDIA` settings.

## Secure Boot

Secure Boot support is implemented as a two-phase flow:

1. Keep `boot.secureBoot.enable = true` in the host vars from the start if that is your desired final state.
2. The installer will install `<host>-install`, which keeps `systemd-boot` active and leaves Secure Boot deferred.
3. On the first real boot, `config-nix-finalize.service` runs before `greetd`, creates keys under `/var/lib/sbctl`, builds the signed final profile with `lanzaboote`, enrolls keys with `sbctl`, and reboots.
4. The next boot should land in the final signed profile.

If the finalizer fails, the machine stays bootable in the install-safe profile and you can rerun it manually:

```bash
sudo bash /etc/nixos/scripts/finalize-host.sh --host desktop --repo /etc/nixos
```

The canonical repo on the installed machine is `/etc/nixos`, and it is a real git checkout with the generated install files already staged. That is also the path the finalizer uses for all rebuilds. Finalizer state is written to `/var/lib/config-nix/finalize-status.json`.

After the first successful boot, commit the generated files:

- `hosts/desktop/hardware-configuration.nix`
- `hosts/vm-test/hardware-configuration.nix` if you installed `vm-test`
- `secrets/hosts/desktop.age.pub`
- `secrets/hosts/vm-test.age.pub` if you installed `vm-test`
- `secrets/hosts/desktop.yaml`
- `secrets/hosts/vm-test.yaml` if you installed `vm-test`
- `.sops.yaml`

## Day-2 usage

Rebuild the current system:

```bash
sudo nixos-rebuild switch --flake /etc/nixos#desktop
```

Or for the VM host:

```bash
sudo nixos-rebuild switch --flake /etc/nixos#vm-test
```

Build only the Home Manager config:

```bash
nix build /etc/nixos#homeConfigurations."weqeq@desktop".activationPackage
```

Update inputs:

```bash
nix flake update /etc/nixos
```

Review the staged install-generated changes:

```bash
git -C /etc/nixos status
```

## Where to add packages

System packages shared by every machine:

- edit [modules/nixos/base.nix](/home/weqeq/Projects/config.nix/modules/nixos/base.nix)

System packages only for one machine:

- edit the relevant host file, for example [hosts/desktop/packages.nix](/home/weqeq/Projects/config.nix/hosts/desktop/packages.nix) or [hosts/vm-test/packages.nix](/home/weqeq/Projects/config.nix/hosts/vm-test/packages.nix)

User packages:

- edit [homes/weqeq/packages.nix](/home/weqeq/Projects/config.nix/homes/weqeq/packages.nix)

## Secrets workflow

Rekey one host after changing recipients or copying the repo to a fresh machine:

```bash
nix --extra-experimental-features 'nix-command flakes' run /etc/nixos#rekey-host -- --host desktop --repo /etc/nixos
```

Edit a host secret:

```bash
sops secrets/hosts/desktop.yaml
```

Shared secrets can live in `secrets/common.yaml`. Host-specific secrets live in `secrets/hosts/<host>.yaml`.

## Adding another host

1. Copy `hosts/_template` to `hosts/<new-host>`.
2. Copy `homes/_template` to `homes/<new-user>` if you need a new user.
3. Update `vars.nix`, `disko.nix` and package files.
4. Run:

```bash
nix --extra-experimental-features 'nix-command flakes' run github:weqeqq/config.nix
```

Host discovery is automatic. You do not need to edit `flake.nix` when adding a new host directory.

## CI

GitHub Actions runs `nix flake check` on push and pull requests.
