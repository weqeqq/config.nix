{ lib, machineState, sharedSettings, ... }:
let
  graphicsState = machineState.graphics or { };
  vendor = graphicsState.vendor or "generic";
  enable32Bit = graphicsState.enable32Bit or false;
  nvidiaCfg = (sharedSettings.graphics or { }).nvidia or { };
  useNvidia = vendor == "nvidia";
  useAmd = vendor == "amd";
in
{
  hardware = {
    graphics = {
      enable = true;
      inherit enable32Bit;
    };
  } // lib.optionalAttrs useNvidia {
    nvidia = {
      modesetting.enable = true;
      powerManagement.enable = false;
      powerManagement.finegrained = false;
      open = lib.mkDefault (nvidiaCfg.open or false);
      nvidiaSettings = true;
    };
  };

  services.xserver.videoDrivers = lib.mkIf (useNvidia || useAmd) (
    if useNvidia then [ "nvidia" ] else [ "amdgpu" ]
  );

  environment.sessionVariables = {
    NIXOS_OZONE_WL = "1";
  }
  // lib.optionalAttrs useNvidia {
    LIBVA_DRIVER_NAME = "nvidia";
    __GLX_VENDOR_LIBRARY_NAME = "nvidia";
  }
  // lib.optionalAttrs useAmd {
    LIBVA_DRIVER_NAME = "radeonsi";
  };
}
