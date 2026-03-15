{ lib, hostName, ... }:
let
  hostSecretFile = /. + "${toString ../../secrets/hosts}/${hostName}.yaml";
  hasHostSecret = builtins.pathExists hostSecretFile;
in
{
  systemd.tmpfiles.rules = [
    "d /var/lib/sops-nix 0700 root root -"
  ];

  sops = {
    age = {
      keyFile = "/var/lib/sops-nix/key.txt";
      generateKey = false;
    };
    defaultSopsFormat = "yaml";
  }
  // lib.optionalAttrs hasHostSecret {
    defaultSopsFile = hostSecretFile;

    secrets.user-password-hash = {
      key = "userPasswordHash";
      sopsFile = hostSecretFile;
      neededForUsers = true;
    };
  };
}
