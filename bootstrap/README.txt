Sneakernet — connect a Linux system to your servers without internet
====================================================================

Everything needed is in this folder. Nothing is downloaded.

1. Boot any Linux ISO from this Ventoy stick (or use an installed system).

2. Open a terminal. If the Ventoy partition is not mounted yet, mount it:

     sudo mkdir -p /mnt/ventoy
     sudo mount /dev/disk/by-label/Ventoy /mnt/ventoy

   While a live ISO from this stick is running, Ventoy keeps the partition
   busy; if that mount fails, try the device mapper copy:

     sudo mount /dev/mapper/sd?1 /mnt/ventoy

3. Run the installer (use "sh", not "./" — the stick has no exec bits):

     sudo sh /mnt/ventoy/sneakernet/install.sh

   It asks a few questions, installs Xray, picks a server from
   servers.txt (in this folder) and starts it as a service. If that list is
   empty, it asks you to paste links, or you can add them later in the TUI.

   servers.txt: one share link per line (vless://, vmess://, trojan://,
   ss://, hysteria2://). You may edit it here, also from Windows.

4. Use the proxy:   SOCKS5 127.0.0.1:10808    HTTP 127.0.0.1:10809

   Manage it later (the stick can be unplugged):
     sudo sneakernet tui        search servers as you type, test their speed,
                                use the fastest, add or remove servers
     sneakernet status          what is running
     sudo sneakernet uninstall  remove everything

Live session? Everything is gone after a reboot unless Ventoy persistence
is set up for that ISO.
