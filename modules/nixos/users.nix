{ lib, runtimeSecrets, sharedSettings, ... }:
let
  userName = sharedSettings.user.name;
  sshKeys = sharedSettings.user.openssh.authorizedKeys;
  passwordHash = runtimeSecrets.userPasswordHash or null;
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
      description = sharedSettings.user.description;
      extraGroups = lib.unique ([ "wheel" "networkmanager" ] ++ sharedSettings.user.extraGroups);
      openssh.authorizedKeys.keys = sshKeys;
    }
    // lib.optionalAttrs (passwordHash != null) {
      hashedPassword = passwordHash;
    }
    // lib.optionalAttrs (passwordHash == null) {
      hashedPassword = "!";
    };
}
