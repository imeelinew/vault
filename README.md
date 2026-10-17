# vault

Fast, reversible video hiding for macOS, played through IINA. It is **not encryption** — the format uses a public fixed key and only prevents accidental discovery.

```sh
v lock [directory]    # hide videos recursively
v [directory]         # browse; arrows, Enter, q
v unlock [directory]  # restore all videos
```

The default directory is the current directory. Supported: MP4, MKV, MOV, M4V, AVI, WebM, WMV, FLV, MPG, MPEG, TS, 3GP. `.DS_Store` is ignored; symlinks are rejected.

In the browser, `/` filters by name, Esc clears it, and `p` toggles previews. Ghostty and Kitty show a thumbnail when the window is at least 96 columns wide; previews are disabled inside tmux. `v lock` caches each video's midpoint frame with FFmpeg in a hidden `.vault-thumbnails` directory, so browsing never runs FFmpeg.

Vault files are hidden `.vault-*.vlt` files beside the originals. Playback serves decrypted byte ranges through a temporary loopback URL; no plaintext video is written to disk.

## Recovery

If `v` is lost, reinstall it and run `v unlock /path/to/videos`. Format: `VAULTV1\n` (8 bytes), AES-CTR IV (16), big-endian plaintext size (8), big-endian encrypted filename length (2), SHA-256 plaintext digest (32), encrypted UTF-8 filename, then encrypted video. The AES-256 key is `SHA-256("vault-v1-reversible-video-obfuscation")`; filename AES-CTR uses the same IV with its first byte XORed with `0x80`.

## Build

```sh
go build -o v .
```
