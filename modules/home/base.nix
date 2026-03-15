{ lib, hostVars, hmIntegrated ? false, ... }:
{
  home = {
    stateVersion = hostVars.homeStateVersion;
  }
  // lib.optionalAttrs (!hmIntegrated) {
    username = hostVars.user.name;
    homeDirectory = "/home/${hostVars.user.name}";
  };

  programs.home-manager.enable = true;
}
