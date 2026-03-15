{ ... }:
{
  boot = {
    initrd.systemd.enable = true;
    loader = {
      efi.canTouchEfiVariables = true;
      systemd-boot = {
        enable = true;
        configurationLimit = 10;
      };
    };
    supportedFilesystems = [ "btrfs" ];
    tmp.cleanOnBoot = true;
  };
}
