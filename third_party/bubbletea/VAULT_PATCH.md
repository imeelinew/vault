# Bubble Tea v2.1.0 local patch

Source: https://github.com/charmbracelet/bubbletea/tree/v2.1.0

This directory contains the original runtime Go sources, module files and MIT
license. Vault adds `View.DisableScrollOptimization` and honors it before each
terminal render. The upstream renderer always enables hard scrolling on macOS;
its full-width scroll-region and insert/delete-line commands move Kitty image
placements even when the preview text and selected video have not changed.

Vault opts out only while its preview pane is visible. Text cell diffing stays
enabled and scrolling neither retransmits nor repositions the thumbnail.
Setting the flag on each frame also covers renderer resets and preview toggles.

When upgrading, retain these two additions or replace them with the equivalent
upstream API if one becomes available. `TestPreviewScrollKeepsImageStationary`
checks the actual rendered terminal output and guards against scroll commands
and thumbnail retransmission while scrolling the library.
