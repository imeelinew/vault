# vault

Fast, reversible video hiding for macOS. `v` uses IINA for playback. It is **not secure encryption**: the format has a public, fixed key and is meant to prevent accidental discovery and exposure.

```sh
v lock [directory]    # hide videos recursively
v [directory]         # browse; arrows, Enter, q
v unlock [directory]  # restore all videos
```

The default directory is the current directory. Supported: MP4, MKV, MOV, M4V, AVI, WebM, WMV, FLV, MPG, MPEG, TS, 3GP. Other files cause an error (`.DS_Store` is ignored). Symlinks are rejected.

In the browser, press `/` to filter by video or folder name (case-insensitive). Enter confirms the filter; Enter again plays the selection. Esc clears the filter. Backspace edits and Ctrl-U clears the search text.

Ghostty and Kitty show a thumbnail on the right when the window is wide enough (96 columns). Requires `ffmpeg`; `p` toggles previews. Titles wrap beside the image. Thumbnails are generated in memory, never saved; switching videos cancels old work and releases the previous image. Exiting clears terminal images and restores the previous screen. Previews are disabled inside tmux.

Vault files are hidden `.vault-*.vlt` files next to the originals. Folder names remain visible. Playback serves decrypted byte ranges only through a temporary loopback URL; no plaintext video is written by `v`. Closing IINA stops the local server. IINA itself controls its own playback cache.

## Recovery

If `v` is deleted, reinstall from this public repository and run `v unlock /path/to/videos`. The format is documented here so it can be recovered independently: `VAULTV1\n` (8 bytes), AES-CTR IV (16), big-endian plaintext size (8), big-endian encrypted filename length (2), SHA-256 plaintext digest (32), encrypted UTF-8 filename, then encrypted video. The AES-256 key is `SHA-256("vault-v1-reversible-video-obfuscation")`; filename AES-CTR uses the same IV with its first byte XORed with `0x80`. Video AES-CTR uses the stored IV. No password or external key is required.

## Build

```sh
go build -o v .
```
