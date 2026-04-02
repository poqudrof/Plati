#!/usr/bin/env bash
set -euo pipefail

# Creates an Incus profile for GPU passthrough (Nvidia).
# Run once on the Incus host before using the ubuntu-gpu template.

PROFILE_NAME="${1:-nvidia}"

if incus profile show "$PROFILE_NAME" &>/dev/null; then
  echo "Profile '$PROFILE_NAME' already exists — skipping."
  exit 0
fi

incus profile create "$PROFILE_NAME"
incus profile device add "$PROFILE_NAME" gpu gpu gputype=physical
incus profile set "$PROFILE_NAME" environment.NVIDIA_VISIBLE_DEVICES=all
incus profile set "$PROFILE_NAME" environment.NVIDIA_DRIVER_CAPABILITIES=all

echo "Profile '$PROFILE_NAME' created with GPU passthrough device."
