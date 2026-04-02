#!/usr/bin/env bash
set -euo pipefail

incus profile create docker 2>/dev/null || true

incus profile edit docker <<'EOF'
name: docker
description: Enables Docker-in-Docker via security.nesting and unconfined AppArmor
config:
  security.nesting: "true"
  raw.lxc: lxc.apparmor.profile=unconfined
devices: {}
EOF

echo "docker profile ready"
