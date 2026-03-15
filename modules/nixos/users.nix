{ lib, config, hostVars, ... }:
let
  userName = hostVars.user.name;
  hasPasswordSecret = config.sops.secrets ? user-password-hash;
  sshKeys = hostVars.user.openssh.authorizedKeys;
in
{
  users.mutableUsers = false;

  users.users.root = {
    hashedPassword = "!";
    openssh.authorizedKeys.keys = sshKeys;
  };

  users.users.${userName} = {
    isNormalUser = true;
    description = hostVars.user.description;
    extraGroups = lib.unique ([ "wheel" "networkmanager" ] ++ hostVars.user.extraGroups);
    openssh.authorizedKeys.keys = sshKeys;
  };
}
// lib.optionalAttrs hasPasswordSecret {
  users.users.${userName}.hashedPasswordFile = config.sops.secrets.user-password-hash.path;
}
// lib.optionalAttrs (!hasPasswordSecret) {
  users.users.${userName}.hashedPassword = "!";
}
