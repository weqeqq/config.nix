{
  description = "Single-flake NixOS + Home Manager configuration with disko and sops-nix";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

    home-manager = {
      url = "github:nix-community/home-manager";
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
      mkHost = import ./lib/mkHost.nix {
        inherit inputs hostMeta;
      };
      mkHome = import ./lib/mkHome.nix {
        inherit inputs hostMeta;
      };
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
          ${commonBody}

          ${stripSharedPrelude (builtins.readFile scriptPath)}
        '';
      nixosConfigurations = builtins.listToAttrs (
        map (hostName: {
          name = hostName;
          value = mkHost {
            inherit hostName;
            system = hostMeta.${hostName}.system or "x86_64-linux";
          };
        }) hostNames
      );
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
        inherit hostMeta hostNames;
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
              pkgs.gitMinimal
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
        in
        {
          inherit diskoCli diskoInstall;

          install-host = pkgs.writeShellApplication {
            name = "install-host";
            runtimeInputs = [
              pkgs.age
              pkgs.coreutils
              diskoCli
              pkgs.findutils
              pkgs.gitMinimal
              pkgs.gnugrep
              pkgs.gnused
              pkgs.jq
              pkgs.nix
              pkgs.sops
              pkgs.ssh-to-age
              pkgs.util-linux
              pkgs.whois
            ];
            text = mkPackagedScriptText ./scripts/install-host.sh;
          };

          rekey-host = pkgs.writeShellApplication {
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
        }
      );

      apps = forAllSystems (system: {
        install-host = {
          type = "app";
          program = "${self.packages.${system}.install-host}/bin/install-host";
        };

        rekey-host = {
          type = "app";
          program = "${self.packages.${system}.rekey-host}/bin/rekey-host";
        };
      });

      checks = forAllSystems (system:
        let
          hostChecks = builtins.listToAttrs (
            map (hostName: {
              name = "system-${hostName}";
              value = self.nixosConfigurations.${hostName}.config.system.build.toplevel;
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
        in
        hostChecks // homeChecks
      );
    };
}
