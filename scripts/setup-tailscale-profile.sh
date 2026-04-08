#!/usr/bin/env bash
# Sets up the 'tailscale' Incus profile required by the tailscale container template.
# Run once on the host before provisioning any Tailscale-based instances.

set -euo pipefail

# Ensure tun module is loaded
modprobe tun
echo tun > /etc/modules-load.d/tun.conf

# Create or overwrite the profile
incus profile create tailscale 2>/dev/null || true

incus profile edit tailscale <<'EOF'
name: tailscale
description: Grants containers access to /dev/net/tun for Tailscale kernel networking
config:
  security.nesting: "true"
devices:
  tun:
    type: unix-char
    source: /dev/net/tun
    path: /dev/net/tun
    required: "false"
EOF

echo "Profile 'tailscale' is ready."
echo ""
echo "After launching a Tailscale container, run:"
echo "  incus exec <name> -- tailscale up --auth-key=<tskey-auth-...>"
