{ lib }:
sharedSettings:
let
  secureBootEnabled = (((sharedSettings.boot or { }).secureBoot or { }).enable or false);
  deferredFeatures = lib.optionals secureBootEnabled [ "secure-boot" ];
  installOutput = "default-install";
  finalOutput = "default";
  needsFinalize = deferredFeatures != [ ];
in
{
  inherit deferredFeatures finalOutput installOutput needsFinalize;
  initialOutput = if needsFinalize then installOutput else finalOutput;
}
