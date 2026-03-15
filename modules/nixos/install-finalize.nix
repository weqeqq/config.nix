{ config, lib, pkgs, hostInstallPlan, hostName, phase, ... }:
let
  repoPath = "/etc/nixos";
  stateDir = "/var/lib/config-nix";
  finalizeMarker = "${stateDir}/finalize-pending";
  runFinalize = phase == "install" && hostInstallPlan.needsFinalize;
in
{
  systemd.tmpfiles.rules = [
    "d ${stateDir} 0700 root root -"
  ];

  systemd.services.config-nix-finalize = lib.mkIf runFinalize {
    description = "Finalize config.nix install profile";
    wantedBy = [ "multi-user.target" ];
    after = [ "local-fs.target" ];
    before = [ "greetd.service" "display-manager.service" ];
    unitConfig.ConditionPathExists = finalizeMarker;

    path = with pkgs; [
      coreutils
      findutils
      gnugrep
      gnused
      jq
      nix
      sbctl
      systemd
      util-linux
    ];

    serviceConfig = {
      Type = "oneshot";
      TimeoutStartSec = "30min";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };

    script = ''
      exec ${pkgs.bash}/bin/bash ${repoPath}/scripts/finalize-host.sh \
        --host ${lib.escapeShellArg hostName} \
        --repo ${lib.escapeShellArg repoPath} \
        --marker-path ${lib.escapeShellArg finalizeMarker}
    '';
  };

  systemd.services.greetd = lib.mkIf (runFinalize && config.services.greetd.enable) {
    wants = [ "config-nix-finalize.service" ];
    after = [ "config-nix-finalize.service" ];
  };
}
