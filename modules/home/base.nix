{ hostVars, ... }:
{
  home.username = hostVars.user.name;
  home.homeDirectory = "/home/${hostVars.user.name}";
  home.stateVersion = hostVars.homeStateVersion;

  programs.home-manager.enable = true;
}
