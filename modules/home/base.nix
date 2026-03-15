{ lib, hmIntegrated ? false, sharedSettings, ... }:
{
  home = {
    stateVersion = sharedSettings.homeStateVersion;
  }
  // lib.optionalAttrs (!hmIntegrated) {
    username = sharedSettings.user.name;
    homeDirectory = "/home/${sharedSettings.user.name}";
  };

  programs.home-manager.enable = true;
}
