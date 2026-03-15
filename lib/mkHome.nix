{ inputs, sharedSettings }:
{ system }:
inputs.home-manager.lib.homeManagerConfiguration {
  pkgs = inputs.nixpkgs.legacyPackages.${system};

  extraSpecialArgs = {
    inherit inputs sharedSettings;
    machineState = {
      platform = {
        kind = "bare-metal";
        hypervisor = "none";
      };
      graphics = {
        vendor = "generic";
        enable32Bit = false;
        pciIds = [ ];
      };
    };
    runtimeSecrets = { };
    hmIntegrated = false;
  };

  modules = [
    ../modules/home/base.nix
    ../modules/home/desktop.nix
    ../modules/home/packages.nix
    ../home/default.nix
    ../home/packages.nix
  ];
}
