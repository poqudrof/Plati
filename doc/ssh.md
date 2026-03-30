# SSH Keys

## Overview

Plati injects your SSH public keys into instances at creation time via cloud-init. This means you can `ssh` directly into any instance using your private key — no passwords, no manual setup.

## Adding Your Keys

Go to **Settings → SSH Keys** and paste your public key(s).

```bash
# Generate a key if you don't have one
ssh-keygen -t ed25519 -C "you@example.com"

# Copy the public key
cat ~/.ssh/id_ed25519.pub
```

You can add multiple keys (e.g. laptop, desktop, CI).

## When Keys Are Injected

| Event | Keys injected? |
|-------|----------------|
| Instance **created** | Yes — all your current keys |
| Instance started / stopped | No |
| Instance **rebuilt** | Yes — all your current keys at rebuild time |
| Instance deleted | N/A |

Keys are written once during cloud-init on first boot. If you add a new key after an instance already exists, it will **not** appear automatically — you need to rebuild the instance (Settings → Rebuild). Rebuilding recreates the container but preserves your `/workspace` volume.

## How It Works Internally

When you create (or rebuild) an instance, the backend:

1. Fetches all SSH keys stored for your user account.
2. Appends them to the template's cloud-init YAML as `ssh_authorized_keys`.
3. Passes the combined config to Incus, which runs cloud-init on first boot.

The resulting cloud-init block looks like:

```yaml
ssh_authorized_keys:
  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5... you@laptop
  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5... you@desktop
```

You do **not** need to configure SSH keys in your template — Plati handles this automatically.

## Connecting to an Instance

Once the instance is running:

```bash
ssh ubuntu@<instance-ip>
```

The default user depends on the template image (typically `ubuntu` for Ubuntu images). The instance IP is shown on the instance detail page.

## Key Scope

- Keys are **per user** — each user manages their own keys independently.
- Keys are not shared between users.
- Deleting a key from Settings does not remove it from already-running instances (it was injected at creation time). It will be absent from any new or rebuilt instances.

## Troubleshooting

**Can't connect after adding a new key?**
The instance was created before the key was added. Rebuild the instance to re-inject all current keys.

**Permission denied (publickey)?**
- Confirm the key in Settings matches the private key you're using locally.
- Check you're using the correct username for the image.
- Verify the instance is in `Running` state, not `Sleeping`.
