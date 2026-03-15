{
  hostName = "new-host";
  system = "x86_64-linux";
  timeZone = "Europe/Moscow";
  locale = "en_US.UTF-8";
  consoleKeyMap = "us";
  systemStateVersion = "25.11";
  homeStateVersion = "25.11";

  user = {
    name = "your-user";
    description = "your-user";
    extraGroups = [ "video" "input" ];
    openssh.authorizedKeys = [
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIREPLACEMEWITHYOURPUBKEY you@example"
    ];
  };

  ownerAgeRecipients = [
    "age1replacewithyourownagepublickeyxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  ];
}
