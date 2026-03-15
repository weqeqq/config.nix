{ lib }:
{ hostName, hostVars }:
let
  secureBootEnabled = (((hostVars.boot or { }).secureBoot or { }).enable or false);
  deferredFeatures = lib.optionals secureBootEnabled [ "secure-boot" ];
  installOutput = "${hostName}-install";
  finalOutput = hostName;
  needsFinalize = deferredFeatures != [ ];
in
{
  inherit deferredFeatures finalOutput installOutput needsFinalize;
  initialOutput = if needsFinalize then installOutput else finalOutput;
}
