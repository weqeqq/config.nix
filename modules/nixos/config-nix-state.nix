{ installPlan, lib, machineState, phase, runtimeSecrets, sharedSettings, ... }:
{
  options.configNix = {
    installPlan = lib.mkOption {
      type = lib.types.attrs;
      readOnly = true;
      default = installPlan;
    };
    machineState = lib.mkOption {
      type = lib.types.attrs;
      readOnly = true;
      default = machineState;
    };
    phase = lib.mkOption {
      type = lib.types.str;
      readOnly = true;
      default = phase;
    };
    runtimeSecrets = lib.mkOption {
      type = lib.types.attrs;
      readOnly = true;
      default = runtimeSecrets;
    };
    sharedSettings = lib.mkOption {
      type = lib.types.attrs;
      readOnly = true;
      default = sharedSettings;
    };
  };
}
