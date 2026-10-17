# vault

**[English](README_EN.md)** · 中文

macOS 视频隐藏工具：递归隐藏视频，经 IINA 播放。**不是加密** —— 使用公开固定密钥，仅防止偶然看到。

```sh
v [目录]           # 浏览并播放
v lock [目录]      # 隐藏全部视频
v unlock [目录]    # 全部还原
```

目录默认为当前目录。

依赖 IINA（含命令行工具）；ffmpeg/ffprobe 仅用于 `v lock` 生成缩略图。支持 mp4 mkv mov m4v avi webm wmv flv mpg mpeg ts 3gp；忽略 `.DS_Store`，拒绝符号链接与非视频文件。

隐藏结果为原文件旁的 `.vault-<hex>.vlt`。播放时经本地回环地址按字节范围解密，磁盘不留明文。

浏览界面：方向键移动，`/` 搜索（Enter 应用），`Esc` 清空，`Enter` 播放，`p` 预览，`?` 快捷键，`q` 退出。缩略图缓存于 `.vault-thumbnails`，仅 Ghostty/Kitty 且宽度 ≥96 列显示，tmux 内禁用。

## 恢复

`v` 丢失时重新编译，运行 `v unlock 目录`。格式：`VAULTV1\n`(8) + AES-CTR IV(16) + 大端明文长度(8) + 大端文件名密文长度(2) + SHA-256 明文摘要(32) + 加密文件名 + 加密视频。密钥为 `SHA-256("vault-v1-reversible-video-obfuscation")`；文件名使用同一 IV 且首字节异或 `0x80`。

## 构建

```sh
go build -o v .
```
