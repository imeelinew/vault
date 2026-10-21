# vault

English · **[中文](README.md)**

Hides videos recursively on macOS and plays them through IINA. **Not encryption** — a public fixed key that only prevents accidental discovery.

```sh
v [directory]          # browse and play
v lock [directory]     # hide all videos
v unlock [directory]   # restore all videos
```

Directory defaults to the current one.

Browsing, locking, and restoring require Touch ID before the directory is scanned; cancel, failure, timeout, or unavailable biometrics exits. No password fallback; `v --help` is exempt. An access gate only — the file format is unchanged.

Requires IINA (with its command-line tool); ffmpeg/ffprobe are used only by `v lock` to build thumbnails. Supports mp4 mkv mov m4v avi webm wmv flv mpg mpeg ts 3gp; `.DS_Store` is ignored, symlinks and non-video files are rejected.

Locked files are hidden `.vault-<hex>.vlt` files beside the originals. Playback decrypts byte ranges over a loopback URL; no plaintext reaches disk.

Browser: arrows to move, `/` to search (Enter applies), `Esc` to clear, `Enter` to play, `p` to preview, `?` for shortcuts, `q` to quit. Thumbnails are cached in `.vault-thumbnails` and shown only in Ghostty/Kitty/Rex at ≥96 columns and ≥16 rows, never inside tmux.

## Recovery

If `v` is lost, rebuild it and run `v unlock directory`. Format: `VAULTV1\n` (8) + AES-CTR IV (16) + big-endian plaintext size (8) + big-endian encrypted filename length (2) + SHA-256 plaintext digest (32) + encrypted filename + encrypted video. The key is `SHA-256("vault-v1-reversible-video-obfuscation")`; the filename uses the same IV with its first byte XORed with `0x80`.

## Build

```sh
CGO_ENABLED=1 go build -o v .
```

Requires macOS and Xcode Command Line Tools (links LocalAuthentication).
