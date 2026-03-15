# NixOS Config

Single-flake NixOS + Home Manager repository for a desktop host with `NVIDIA`, `Hyprland`, `disko` and `sops-nix`.

## What is in this repo

- One root `flake.nix`.
- `nixosConfigurations.<host>` for full system builds.
- `homeConfigurations."<user>@<host>"` for standalone Home Manager builds.
- `disko` layout per host.
- `sops-nix` integration with encrypted secrets stored in the same repository.
- `nix run .#install-host` for local installation from the official minimal ISO.

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
3. Clone this repository.
4. Run the installer app:

```bash
nix --extra-experimental-features 'nix-command flakes' run .#install-host -- --host desktop --disk /dev/disk/by-id/YOUR-DISK
```

For a VM install test, use the dedicated host:

```bash
nix --extra-experimental-features 'nix-command flakes' run .#install-host -- --host vm-test --disk /dev/vda
```

What the installer does:

- wipes and partitions the disk with `disko`;
- mounts the target filesystem under `/mnt`;
- generates `hosts/<host>/hardware-configuration.nix`;
- creates `/var/lib/sops-nix/key.txt` on the target system;
- writes `secrets/hosts/<host>.age.pub` into the repo;
- regenerates `.sops.yaml`;
- prompts for the initial user password and stores its hash encrypted in `secrets/hosts/<host>.yaml`;
- runs `nixos-install --flake .#<host>`.

For `vm-test` the flow is the same, but the target profile enables `qemu-guest` integration and skips all `NVIDIA` settings.

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
sudo nixos-rebuild switch --flake .#desktop
```

Or for the VM host:

```bash
sudo nixos-rebuild switch --flake .#vm-test
```

Build only the Home Manager config:

```bash
nix build .#homeConfigurations."weqeq@desktop".activationPackage
```

Update inputs:

```bash
nix flake update
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
nix --extra-experimental-features 'nix-command flakes' run .#rekey-host -- --host desktop
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
nix --extra-experimental-features 'nix-command flakes' run .#install-host -- --host <new-host> --disk /dev/disk/by-id/YOUR-DISK
```

Host discovery is automatic. You do not need to edit `flake.nix` when adding a new host directory.

## CI

GitHub Actions runs `nix flake check` on push and pull requests.
