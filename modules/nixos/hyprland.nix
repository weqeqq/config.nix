{ lib, pkgs, ... }:
let
  tuigreet = lib.getExe pkgs.tuigreet;
  defaultSession = lib.escapeShellArg "Hyprland";
in
{
  programs.hyprland = {
    enable = true;
    withUWSM = true;
    xwayland.enable = true;
  };

  programs.dconf.enable = true;

  services.greetd = {
    enable = true;
    settings = {
      default_session = {
        user = "greeter";
        command = "${tuigreet} --time --remember --remember-user-session --cmd ${defaultSession}";
      };
    };
  };

  xdg.portal = {
    enable = true;
    extraPortals = with pkgs; [
      xdg-desktop-portal-gtk
      xdg-desktop-portal-hyprland
    ];
  };
}
