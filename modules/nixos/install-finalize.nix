{ config, inputs, lib, pkgs, phase, ... }:
let
  installPlan = config.configNix.installPlan;
  repoPath = "/etc/nixos";
  stateDir = "/var/lib/config-nix";
  finalizeMarker = "${stateDir}/finalize-pending";
  finalizeTool = "${inputs.self.packages.${pkgs.system}.config-nix-tools}/bin/finalize-system";
  runFinalize = phase == "install" && installPlan.needsFinalize;
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

    serviceConfig = {
      Environment = [ "CONFIG_NIX_LOCAL_STATE_DIR=${repoPath}/local" ];
      ExecStart = "${finalizeTool} --repo ${lib.escapeShellArg repoPath} --marker-path ${lib.escapeShellArg finalizeMarker}";
      Type = "oneshot";
      TimeoutStartSec = "30min";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };

  systemd.services.greetd = lib.mkIf (runFinalize && config.services.greetd.enable) {
    wants = [ "config-nix-finalize.service" ];
    after = [ "config-nix-finalize.service" ];
  };
}
