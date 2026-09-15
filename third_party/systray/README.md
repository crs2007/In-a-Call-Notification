# Patched systray module

This directory contains `github.com/gogpu/systray` v0.3.0 with one Windows
fix: notification icons using `NOTIFYICON_VERSION_4` include `NIF_SHOWTIP`
when they are added or updated. Without that flag, Windows suppresses the
standard hover tooltip.

Remove the root `replace` directive and this directory after the fix is
available in a released upstream version.