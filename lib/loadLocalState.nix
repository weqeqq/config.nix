let
  localStateDir = builtins.getEnv "CONFIG_NIX_LOCAL_STATE_DIR";

  localPath =
    name:
    let
      rawPath = "${localStateDir}/${name}";
    in
    if localStateDir != "" && builtins.pathExists rawPath then builtins.toPath rawPath else null;

  importOr =
    path: fallback:
    if path == null then fallback else import path;

  machineStatePath = localPath "machine-state.nix";
  runtimeSecretsPath = localPath "runtime-secrets.nix";
  hardwareConfigurationPath = localPath "hardware-configuration.nix";
in
{
  inherit localStateDir machineStatePath runtimeSecretsPath hardwareConfigurationPath;

  machineState = importOr machineStatePath {
    machineId = "";
    hostName = "";
    installedAt = "";
    installDisk = "";
    platform = {
      kind = "bare-metal";
      hypervisor = "none";
    };
    graphics = {
      vendor = "generic";
      enable32Bit = false;
      pciIds = [ ];
    };
  };

  runtimeSecrets = importOr runtimeSecretsPath { };
}
