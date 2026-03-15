{ lib, hostVars, ... }:
let
  graphicsCfg = hostVars.graphics or { };
  nvidiaCfg = graphicsCfg.nvidia or { };
  useNvidia = nvidiaCfg.enable or false;
in
{
  hardware = {
    graphics = {
      enable = true;
      enable32Bit = graphicsCfg.enable32Bit or useNvidia;
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

  services.xserver.videoDrivers = lib.mkIf useNvidia [ "nvidia" ];

  environment.sessionVariables = {
    NIXOS_OZONE_WL = "1";
  } // lib.optionalAttrs useNvidia {
    LIBVA_DRIVER_NAME = "nvidia";
    __GLX_VENDOR_LIBRARY_NAME = "nvidia";
  };
}
