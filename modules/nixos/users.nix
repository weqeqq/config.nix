{ lib, hostName, hostVars, ... }:
let
  userName = hostVars.user.name;
  hostSecretFile = ../../secrets/hosts + "/${hostName}.yaml";
  hasPasswordSecret = builtins.pathExists hostSecretFile;
  sshKeys = hostVars.user.openssh.authorizedKeys;
in
{
  users.mutableUsers = false;

  users.users.root = {
    hashedPassword = "!";
    openssh.authorizedKeys.keys = sshKeys;
  };

  users.users.${userName} =
    {
      isNormalUser = true;
      description = hostVars.user.description;
      extraGroups = lib.unique ([ "wheel" "networkmanager" ] ++ hostVars.user.extraGroups);
      openssh.authorizedKeys.keys = sshKeys;
    }
    // lib.optionalAttrs hasPasswordSecret {
      hashedPasswordFile = "/run/secrets-for-users/user-password-hash";
    }
    // lib.optionalAttrs (!hasPasswordSecret) {
      hashedPassword = "!";
    };
}
