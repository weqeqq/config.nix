{
  hostName = "vm-test";
  system = "x86_64-linux";
  timeZone = "Europe/Moscow";
  locale = "en_US.UTF-8";
  consoleKeyMap = "us";
  systemStateVersion = "25.11";
  homeStateVersion = "25.11";

  graphics = {
    enable32Bit = false;
    nvidia = {
      enable = false;
      open = false;
    };
  };

  virtualization.qemuGuest = true;

  user = {
    name = "weqeq";
    description = "weqeq";
    extraGroups = [ "video" "input" ];
    openssh.authorizedKeys = [
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPhY/3POdOP0265usWCvZebZ9a3P+6KRIFpWmQwSTjal weqeq@onafiel"
    ];
  };

  ownerAgeRecipients = [
    "age1f8yxh8nfxnxdhe0fnu2ym9nwnn38huyuc98s7m52vlsnjyfg9axqfs4ph7"
  ];
}
