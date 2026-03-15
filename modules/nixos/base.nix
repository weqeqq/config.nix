{ inputs, machineState, pkgs, sharedSettings, ... }:
{
  networking.hostName =
    if (machineState.hostName or "") != "" then machineState.hostName else "${sharedSettings.hostNamePrefix}-generic";
  networking.networkmanager.enable = true;

  time.timeZone = sharedSettings.timeZone;

  i18n.defaultLocale = sharedSettings.locale;
  console.keyMap = sharedSettings.consoleKeyMap;

  nix = {
    channel.enable = false;
    settings = {
      experimental-features = [ "nix-command" "flakes" ];
      auto-optimise-store = true;
    };
    gc = {
      automatic = true;
      dates = "weekly";
      options = "--delete-older-than 14d";
    };
  };

  nixpkgs.config.allowUnfree = true;

  environment.systemPackages = with pkgs; [
    age
    curl
    git
    jq
    inputs.self.packages.${pkgs.system}.config-nix-tools
    sbctl
    sops
    ssh-to-age
    vim
  ];

  hardware.enableRedistributableFirmware = true;

  services.fwupd.enable = true;
  services.openssh.enable = true;
  services.openssh.settings = {
    PasswordAuthentication = false;
    KbdInteractiveAuthentication = false;
  };
  services.udisks2.enable = true;

  security.polkit.enable = true;

  zramSwap = {
    enable = true;
    algorithm = "zstd";
    memoryPercent = 100;
  };

  system.stateVersion = sharedSettings.systemStateVersion;
}
