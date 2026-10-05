# Ventoy integration

Copy `dist/sneakernet/` to the **root of the Ventoy data partition** (the large exFAT one), next to your ISOs:

```
Ventoy/
├── ISO/...
├── sneakernet/       ← here
└── ventoy/ventoy.json (optional)
```

In the live session, open a terminal and run:

```sh
sudo sh /run/media/$USER/Ventoy/sneakernet/install.sh    # Fedora, Arch, openSUSE
sudo sh /media/$USER/Ventoy/sneakernet/install.sh        # Ubuntu, Debian, Mint
```

If the partition is not mounted, mount it yourself:

```sh
sudo mkdir -p /mnt/ventoy && sudo mount /dev/disk/by-label/Ventoy /mnt/ventoy
# while an ISO from this stick is running, Ventoy may hold the partition; then use
sudo mount /dev/mapper/sd?1 /mnt/ventoy
```

Always use `sh install.sh`. exFAT has no execute bits, so `./install.sh` will fail.

## Optional: persistence

Without persistence, an install into the live session is lost on reboot. To keep it:

1. Create a persistence image on the Ventoy partition with Ventoy's `CreatePersistentImg.sh`.
2. Merge the `persistence` block from `ventoy.json.example` into `/ventoy/ventoy.json`. If the file already exists, add the entry to its existing `persistence` array instead of replacing the file.
3. Adjust `image` to your ISO path and `backend` to your `.dat` path.

You don't need persistence if you install into a system on disk (choose "installed system on disk" in the installer).
