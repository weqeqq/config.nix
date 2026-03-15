{ lib, ... }:
let
  commonSecretFile = ../../secrets/common.yaml;
  hasCommonSecret = builtins.pathExists commonSecretFile;
in
{
  sops = {
    age = {
      keyFile = "/var/lib/sops-nix/key.txt";
      generateKey = false;
    };
    defaultSopsFormat = "yaml";
  }
  // lib.optionalAttrs hasCommonSecret {
    defaultSopsFile = commonSecretFile;
  };

  systemd.tmpfiles.rules = [
    "d /var/lib/sops-nix 0700 root root -"
  ];
}
