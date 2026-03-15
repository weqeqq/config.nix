{
  description = "Single-flake NixOS + Home Manager configuration with disko and sops-nix";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    lanzaboote = {
      url = "github:nix-community/lanzaboote/v0.4.3";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    disko = {
      url = "github:nix-community/disko";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    sops-nix = {
      url = "github:Mic92/sops-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = inputs@{ self, nixpkgs, ... }:
    let
      lib = nixpkgs.lib;
      systems = [ "x86_64-linux" ];
      forAllSystems = lib.genAttrs systems;
      hostsDir = ./hosts;
      hostNames =
        lib.sort lib.lessThan
          (builtins.attrNames
            (lib.filterAttrs
              (name: kind: kind == "directory" && name != "_template")
              (builtins.readDir hostsDir)));
      hostMeta = builtins.listToAttrs (
        map (hostName: {
          name = hostName;
          value = import (hostsDir + "/${hostName}/vars.nix");
        }) hostNames
      );
      mkInstallPlan = import ./lib/mkInstallPlan.nix {
        inherit lib;
      };
      installPlan = builtins.listToAttrs (
        map (hostName: {
          name = hostName;
          value = mkInstallPlan {
            inherit hostName;
            hostVars = hostMeta.${hostName};
          };
        }) hostNames
      );
      mkHost = import ./lib/mkHost.nix {
        inherit inputs hostMeta installPlan;
      };
      mkHome = import ./lib/mkHome.nix {
        inherit inputs hostMeta;
      };
      bootstrapRepoUrl = "https://github.com/weqeqq/config.nix.git";
      bootstrapRepoRev = if self ? rev then self.rev else "";
      stripSharedPrelude = scriptText:
        lib.replaceStrings
          [
            ''#!/usr/bin/env bash

set -euo pipefail

script_dir="$(dirname -- "''${BASH_SOURCE[0]}")"
script_dir="$(cd -- "$script_dir" && pwd -P)"
# shellcheck source=./common.sh
source "$script_dir/common.sh"

''
          ]
          [ "" ]
          scriptText;
      mkPackagedScriptText = scriptPath:
        let
          commonBody = lib.replaceStrings
            [
              "#!/usr/bin/env bash\n\n"
            ]
            [ "" ]
            (builtins.readFile ./scripts/common.sh);
        in
        ''
          export CONFIG_NIX_BOOTSTRAP_REPO_URL=${lib.escapeShellArg bootstrapRepoUrl}
          export CONFIG_NIX_BOOTSTRAP_REV=${lib.escapeShellArg bootstrapRepoRev}
          export CONFIG_NIX_FLAKE_SOURCE=${lib.escapeShellArg self.outPath}

          ${commonBody}

          ${stripSharedPrelude (builtins.readFile scriptPath)}
        '';
      finalNixosConfigurations = builtins.listToAttrs (
        map (hostName: {
          name = hostName;
          value = mkHost {
            inherit hostName;
            system = hostMeta.${hostName}.system or "x86_64-linux";
          };
        }) hostNames
      );
      installNixosConfigurations = builtins.listToAttrs (
        map (hostName: {
          name = "${hostName}-install";
          value = mkHost {
            inherit hostName;
            system = hostMeta.${hostName}.system or "x86_64-linux";
            phase = "install";
          };
        }) hostNames
      );
      nixosConfigurations = finalNixosConfigurations // installNixosConfigurations;
      homeConfigurations = builtins.listToAttrs (
        map (hostName:
          let
            vars = hostMeta.${hostName};
          in
          {
            name = "${vars.user.name}@${hostName}";
            value = mkHome {
              inherit hostName;
              system = vars.system or "x86_64-linux";
            };
          }) hostNames
      );
    in
    {
      inherit homeConfigurations nixosConfigurations;

      lib = {
        inherit hostMeta hostNames installPlan;
      };

      formatter = forAllSystems (system:
        nixpkgs.legacyPackages.${system}.alejandra
      );

      devShells = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.age
              pkgs.alejandra
              inputs.disko.packages.${system}.disko
              pkgs.fzf
              pkgs.gitMinimal
              pkgs.gum
              pkgs.gnused
              pkgs.jq
              pkgs.nix
              pkgs.sops
              pkgs.ssh-to-age
              pkgs.whois
            ];
          };
        }
      );

      packages = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          diskoCli = inputs.disko.packages.${system}.disko;
          diskoInstall = inputs.disko.packages.${system}.disko-install;
          installHostPackage = pkgs.writeShellApplication {
            name = "install-host";
            runtimeInputs = [
              pkgs.age
              pkgs.coreutils
              diskoCli
              pkgs.findutils
              pkgs.fzf
              pkgs.gitMinimal
              pkgs.gum
              pkgs.gnugrep
              pkgs.gnused
              pkgs.gnutar
              pkgs.jq
              pkgs.nix
              pkgs.sops
              pkgs.ssh-to-age
              pkgs.util-linux
              pkgs.whois
            ];
            text = mkPackagedScriptText ./scripts/install-host.sh;
          };
          finalizeHostPackage = pkgs.writeShellApplication {
            name = "finalize-host";
            runtimeInputs = [
              pkgs.coreutils
              pkgs.findutils
              pkgs.gnugrep
              pkgs.gnused
              pkgs.jq
              pkgs.nix
              pkgs.sbctl
              pkgs.systemd
              pkgs.util-linux
            ];
            text = mkPackagedScriptText ./scripts/finalize-host.sh;
          };
          rekeyHostPackage = pkgs.writeShellApplication {
            name = "rekey-host";
            runtimeInputs = [
              pkgs.age
              pkgs.coreutils
              pkgs.gitMinimal
              pkgs.gnugrep
              pkgs.gnused
              pkgs.jq
              pkgs.nix
              pkgs.sops
              pkgs.ssh-to-age
            ];
            text = mkPackagedScriptText ./scripts/rekey-host.sh;
          };
        in
        {
          default = installHostPackage;
          inherit diskoCli diskoInstall;
          finalize-host = finalizeHostPackage;
          install-host = installHostPackage;
          rekey-host = rekeyHostPackage;
        }
      );

      apps = forAllSystems (system:
        let
          finalizeHostApp = {
            type = "app";
            program = "${self.packages.${system}.finalize-host}/bin/finalize-host";
            meta.description = "Finalize a deferred config.nix host install";
          };
          installHostApp = {
            type = "app";
            program = "${self.packages.${system}.install-host}/bin/install-host";
            meta.description = "Install a config.nix host onto a target disk";
          };
          rekeyHostApp = {
            type = "app";
            program = "${self.packages.${system}.rekey-host}/bin/rekey-host";
            meta.description = "Rekey sops secrets for a config.nix host";
          };
        in
        {
          default = installHostApp;
          finalize-host = finalizeHostApp;
          install-host = installHostApp;
          rekey-host = rekeyHostApp;
        }
      );

      checks = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          hostChecks = builtins.listToAttrs (
            map (hostName: {
              name = "system-${hostName}";
              value = self.nixosConfigurations.${hostName}.config.system.build.toplevel;
            }) hostNames
          );
          installHostChecks = builtins.listToAttrs (
            map (hostName: {
              name = "system-${hostName}-install";
              value = self.nixosConfigurations."${hostName}-install".config.system.build.toplevel;
            }) hostNames
          );
          homeChecks = builtins.listToAttrs (
            map (hostName:
              let
                vars = hostMeta.${hostName};
              in
              {
                name = "home-${vars.user.name}-${hostName}";
                value = self.homeConfigurations."${vars.user.name}@${hostName}".activationPackage;
              }) hostNames
          );
          installPlanChecks = {
            install-plan-secure-boot =
              let
                plan = mkInstallPlan {
                  hostName = "secure-host";
                  hostVars.boot.secureBoot.enable = true;
                };
              in
              assert plan.needsFinalize;
              assert plan.initialOutput == "secure-host-install";
              assert plan.finalOutput == "secure-host";
              assert plan.installOutput == "secure-host-install";
              assert plan.deferredFeatures == [ "secure-boot" ];
              pkgs.runCommand "install-plan-secure-boot" { } ''
                touch "$out"
              '';
            install-plan-direct =
              let
                plan = mkInstallPlan {
                  hostName = "plain-host";
                  hostVars = { };
                };
              in
              assert (!plan.needsFinalize);
              assert plan.initialOutput == "plain-host";
              assert plan.finalOutput == "plain-host";
              assert plan.installOutput == "plain-host-install";
              assert plan.deferredFeatures == [ ];
              pkgs.runCommand "install-plan-direct" { } ''
                touch "$out"
              '';
          };
        in
        hostChecks // installHostChecks // homeChecks // installPlanChecks
      );
    };
}
