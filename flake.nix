{
  description = "Single-flake NixOS + Home Manager configuration with cached machine detection";

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
      sharedSettings = import ./settings.nix;
      localState = import ./lib/loadLocalState.nix;
      mkInstallPlan = import ./lib/mkInstallPlan.nix {
        inherit lib;
      };
      installPlan = mkInstallPlan sharedSettings;
      mkSystem = import ./lib/mkSystem.nix {
        inherit inputs installPlan localState sharedSettings;
      };
      mkHome = import ./lib/mkHome.nix {
        inherit inputs sharedSettings;
      };
      bootstrapRepoUrl = "https://github.com/weqeqq/config.nix.git";
      bootstrapRepoRev = if self ? rev then self.rev else "";
      defaultSystem = sharedSettings.system or "x86_64-linux";
    in
    {
      nixosConfigurations = {
        default = mkSystem {
          system = defaultSystem;
        };
        default-install = mkSystem {
          system = defaultSystem;
          phase = "install";
        };
      };

      homeConfigurations =
        let
          config = mkHome {
            system = defaultSystem;
          };
          userName = sharedSettings.user.name;
        in
        {
          default = config;
          "${userName}" = config;
        };

      lib = {
        inherit installPlan localState sharedSettings;
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
              pkgs.go
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
          toolsRuntimePath = lib.makeBinPath [
            pkgs.age
            pkgs.coreutils
            diskoCli
            pkgs.findutils
            pkgs.gitMinimal
            pkgs.gnugrep
            pkgs.gnused
            pkgs.gnutar
            pkgs.jq
            pkgs.nix
            pkgs.nixos-install-tools
            pkgs.sops
            pkgs.sbctl
            pkgs.systemd
            pkgs.util-linux
            pkgs.whois
          ];
          toolsPackage = pkgs.buildGoModule {
            pname = "config-nix-tools";
            version = "0.1.0";
            src = ./installer;
            subPackages = [
              "cmd/install-system"
              "cmd/finalize-system"
              "cmd/rekey-system"
              "cmd/rebuild-system"
            ];
            vendorHash = "sha256-0XcMt2lu+teI2M6VTI9ia7Wg38KTDGjEUd0cw6FwNd4=";
            nativeBuildInputs = [
              pkgs.makeWrapper
            ];
            postFixup = ''
              for bin in "$out"/bin/*; do
                wrapProgram "$bin" \
                  --prefix PATH : "${toolsRuntimePath}" \
                  --set CONFIG_NIX_BOOTSTRAP_REPO_URL ${lib.escapeShellArg bootstrapRepoUrl} \
                  --set CONFIG_NIX_BOOTSTRAP_REV ${lib.escapeShellArg bootstrapRepoRev} \
                  --set CONFIG_NIX_FLAKE_SOURCE ${lib.escapeShellArg self.outPath}
              done
            '';
          };
        in
        {
          default = toolsPackage;
          config-nix-tools = toolsPackage;
          diskoCli = diskoCli;
          diskoInstall = diskoInstall;
          finalize-host = toolsPackage;
          finalize-system = toolsPackage;
          install-host = toolsPackage;
          install-system = toolsPackage;
          rebuild-system = toolsPackage;
          rekey-host = toolsPackage;
          rekey-system = toolsPackage;
        }
      );

      apps = forAllSystems (system:
        let
          toolsPath = "${self.packages.${system}.config-nix-tools}/bin";
          mkApp =
            program: description:
            {
              type = "app";
              inherit program;
              meta.description = description;
            };
        in
        {
          default = mkApp "${toolsPath}/install-system" "Install the shared config.nix profile onto a target disk";
          finalize-host = mkApp "${toolsPath}/finalize-system" "Finalize a deferred config.nix install";
          finalize-system = mkApp "${toolsPath}/finalize-system" "Finalize a deferred config.nix install";
          install-host = mkApp "${toolsPath}/install-system" "Install the shared config.nix profile onto a target disk";
          install-system = mkApp "${toolsPath}/install-system" "Install the shared config.nix profile onto a target disk";
          rebuild-system = mkApp "${toolsPath}/rebuild-system" "Rebuild the current config.nix system using local machine state";
          rekey-host = mkApp "${toolsPath}/rekey-system" "Rekey shared config.nix secrets";
          rekey-system = mkApp "${toolsPath}/rekey-system" "Rekey shared config.nix secrets";
        }
      );

      checks = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          installPlanChecks = {
            install-plan-secure-boot =
              let
                plan = mkInstallPlan {
                  boot.secureBoot.enable = true;
                };
              in
              assert plan.needsFinalize;
              assert plan.initialOutput == "default-install";
              assert plan.finalOutput == "default";
              assert plan.installOutput == "default-install";
              assert plan.deferredFeatures == [ "secure-boot" ];
              pkgs.runCommand "install-plan-secure-boot" { } ''
                touch "$out"
              '';
            install-plan-direct =
              let
                plan = mkInstallPlan { };
              in
              assert (!plan.needsFinalize);
              assert plan.initialOutput == "default";
              assert plan.finalOutput == "default";
              assert plan.installOutput == "default-install";
              assert plan.deferredFeatures == [ ];
              pkgs.runCommand "install-plan-direct" { } ''
                touch "$out"
              '';
          };
        in
        {
          home-default = self.homeConfigurations.default.activationPackage;
          system-default = self.nixosConfigurations.default.config.system.build.toplevel;
          system-default-install = self.nixosConfigurations.default-install.config.system.build.toplevel;
        }
        // installPlanChecks
      );
    };
}
