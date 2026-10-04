# Ventoy integration

Copy `dist/v2ray-kit/` to the **root of the Ventoy data partition** (the large exFAT one), next to your ISOs:

```
Ventoy/
├── ISO/...
├── v2ray-kit/        ← here
└── ventoy/ventoy.json (optional)
```

In the live session, open a terminal and run:

```sh
sudo sh /run/media/$USER/Ventoy/v2ray-kit/install.sh    # Fedora, Arch, openSUSE
sudo sh /media/$USER/Ventoy/v2ray-kit/install.sh        # Ubuntu, Debian, Mint
```

Always use `sh install.sh`. exFAT has no execute bits, so `./install.sh` will fail.

## Optional: persistence

Without persistence, an install into the live session is lost on reboot. To keep it:

1. Create a persistence image on the Ventoy partition with Ventoy's `CreatePersistentImg.sh`.
2. Merge the `persistence` block from `ventoy.json.example` into `/ventoy/ventoy.json`. If the file already exists, add the entry to its existing `persistence` array instead of replacing the file.
3. Adjust `image` to your ISO path and `backend` to your `.dat` path.

You don't need persistence if you install into a system on disk (choose "installed system on disk" in the installer).
