# Quickstart: Local Incus Setup for Plati

This guide walks through installing Incus locally (Arch Linux), generating TLS client certificates, and configuring Plati to use the local server.

## 1. Install Incus

```bash
sudo pacman -S incus
```

## 2. Start the daemon

```bash
sudo systemctl enable --now incus
```

## 3. Grant your user access

Add yourself to the `incus-admin` group so you can use `incus` without `sudo`:

```bash
sudo usermod -aG incus-admin $USER
```

Apply the new group (or log out and back in):

```bash
newgrp incus-admin
```

Verify:

```bash
incus info   # should print server config, not a permission error
```

## 4. Initialize Incus

For a minimal setup with a `dir` storage backend:

```bash
incus admin init --minimal
```

This creates the `default` profile and a `default` storage pool using a directory backend. For ZFS or Btrfs pools, run `incus admin init` interactively instead.

## 5. Enable the HTTPS listener

Plati connects to Incus over HTTPS with mutual TLS. Enable the listener:

```bash
incus config set core.https_address :8443
```

## 6. Generate TLS client certificates

Create a certificate and key for Plati to authenticate with Incus:

```bash
mkdir -p config/certs

openssl req -x509 \
  -newkey ec -pkeyopt ec_paramgen_curve:secp384r1 -sha384 \
  -keyout config/certs/client.key \
  -out config/certs/client.crt \
  -nodes \
  -subj '/CN=plati' \
  -days 3650
```

This generates:
- `config/certs/client.crt` -- X.509 certificate (EC P-384, valid 10 years)
- `config/certs/client.key` -- private key (unencrypted)

> **Security note:** The `config/certs/` directory is gitignored. Never commit these files. The private key has no passphrase (`-nodes`) because Plati loads it at startup -- protect it with filesystem permissions:
>
> ```bash
> chmod 600 config/certs/client.key
> chmod 644 config/certs/client.crt
> ```

## 7. Trust the certificate in Incus

Tell Incus to trust connections using this client certificate:

```bash
incus config trust add-certificate config/certs/client.crt
```

Verify the trust relationship:

```bash
curl -sk \
  --cert config/certs/client.crt \
  --key config/certs/client.key \
  https://127.0.0.1:8443/1.0 | jq .metadata.auth
```

Expected output: `"trusted"`

## 8. Configure Plati

In `config/plati.yaml`, set the server block:

```yaml
servers:
  - name: "local"
    endpoint: "https://127.0.0.1:8443"
    tls_client_cert: "certs/client.crt"
    tls_client_key: "certs/client.key"
    max_instances: 50
```

Cert paths are relative to the config file directory (`config/`), so `certs/client.crt` resolves to `config/certs/client.crt`. This works in both native and Docker dev environments.

## 9. Create required Incus profiles

Plati templates declare which Incus profiles they need (e.g. `default`, `docker`, `tailscale`). These profiles must exist on the Incus server before instances can be created. Run the setup scripts once as root (or with `sudo`) — they are idempotent and safe to re-run.

### docker profile (Docker-in-Docker)

Required by any template that includes Docker. Enables `security.nesting` and sets an unconfined AppArmor policy so the container can run a Docker daemon:

```bash
sudo bash scripts/setup-docker-profile.sh
```

### tailscale profile

Required by any template that includes Tailscale. Loads the `tun` kernel module and exposes `/dev/net/tun` to containers:

```bash
sudo bash scripts/setup-tailscale-profile.sh
```

The script also persists the `tun` module across reboots via `/etc/modules-load.d/tun.conf`.

### nvidia profile (optional — GPU instances only)

Only needed if you are running the `ubuntu-gpu` template on a host with an Nvidia GPU:

```bash
sudo bash scripts/setup-gpu-profile.sh          # creates profile named "nvidia" (default)
sudo bash scripts/setup-gpu-profile.sh mygpu    # optional: custom profile name
```

After running the scripts, confirm the profiles exist:

```bash
incus profile list
```

You should see `default`, `docker`, `tailscale` (and `nvidia` if applicable) in the output.

> **Troubleshooting:** If you see *"Requested profile X doesn't exist"* when creating an instance, the template's profile list includes a profile that wasn't created yet. The debug page at `/admin/templates/{id}/debug` will show a warning listing the missing profiles.

## 10. Run the dev setup

### Option A: Native (requires Go 1.23+, Node 20+)

```bash
./scripts/setup-dev.sh   # copies config, installs deps, generates JWT + AES keys
make migrate              # apply DB migrations
make dev                  # start backend + frontend
```

Backend: http://localhost:8080 -- Frontend: http://localhost:5173

### Option B: Docker (requires Docker with Compose)

The Docker dev stack handles Go and Node dependencies inside containers, but `setup-dev.sh` requires them locally. Generate the config and secrets manually:

```bash
# Copy config template
cp config/plati.yaml.example config/plati.yaml

# Generate JWT secret and encryption key
JWT_SECRET=$(openssl rand -hex 32)
ENC_KEY=$(openssl rand -hex 32)
sed -i "s/jwt_secret: \"\"/jwt_secret: \"$JWT_SECRET\"/" config/plati.yaml
sed -i "s/secret_encryption_key: \"\"/secret_encryption_key: \"$ENC_KEY\"/" config/plati.yaml
```

Edit `config/plati.yaml` with your Incus server details (step 8), run the profile setup scripts (step 9), then:

```bash
docker compose -f docker-compose.dev.yml up
```

Backend: http://localhost:8080 -- Frontend: http://localhost:5300

> **Note:** The Docker backend uses `network_mode: host`, so it can reach Incus at `127.0.0.1:8443` with the same config as native. The frontend runs on port **5300** (not 5173).

## TLS certificate details

### Why mutual TLS?

Incus uses mutual TLS (mTLS) for API authentication. Both the server and client present certificates:

- **Server certificate:** auto-generated by Incus at init, self-signed. Plati skips server cert verification (`InsecureSkipVerify: true`) since the server is local.
- **Client certificate:** generated by you (step 6), trusted by Incus (step 7). This is how Plati proves its identity to Incus.

### Certificate lifecycle

| Item | Location | Validity | Rotation |
|------|----------|----------|----------|
| Client cert | `config/certs/client.crt` | 10 years | Regenerate with step 6, re-trust with step 7 |
| Client key | `config/certs/client.key` | -- | Always paired with the cert |
| Server cert | Managed by Incus | 10 years | `incus admin recover` or reinstall |

### Revoking access

To revoke a client certificate:

```bash
# List trusted certs
incus config trust list

# Remove by fingerprint
incus config trust remove <fingerprint>
```

### Multiple servers

For remote Incus servers, add additional entries to the `servers` array in `plati.yaml`. Each server needs its own client cert trusted on that server. For remote servers you may want to use proper CA-signed certificates instead of self-signed ones.

## Troubleshooting

| Problem | Fix |
|---------|-----|
| `socket path: /var/lib/incus/unix.socket` permission error | `sudo usermod -aG incus-admin $USER` then re-login |
| `incus admin init` says already initialized | Safe to skip -- Incus is already set up |
| Plati can't connect to `127.0.0.1:8443` | Check `incus config get core.https_address` returns `:8443` |
| `certificate not trusted` | Re-run `incus config trust add-certificate config/certs/client.crt` |
| `read client cert: no such file` | Check paths in `plati.yaml` are relative to the project root |
| `Requested profile "docker" doesn't exist` | Run `sudo bash scripts/setup-docker-profile.sh` |
| `Requested profile "tailscale" doesn't exist` | Run `sudo bash scripts/setup-tailscale-profile.sh` |
| `Requested profile "nvidia" doesn't exist` | Run `sudo bash scripts/setup-gpu-profile.sh` |
| `System doesn't have a functional idmap setup` | See **idmap setup** section below |

### idmap setup

Incus unprivileged containers require a UID/GID map so the kernel can isolate container users from the host. If `/etc/subuid` and `/etc/subgid` don't have an entry for `root`, Incus cannot create any container.

Check whether the entries exist:

```bash
grep root /etc/subuid /etc/subgid
```

If either file is missing the `root` entry, add it:

```bash
echo "root:1000000:65536" | sudo tee -a /etc/subuid
echo "root:1000000:65536" | sudo tee -a /etc/subgid
```

Then restart Incus to pick up the change:

```bash
sudo systemctl restart incus
```

Verify containers can now be created:

```bash
incus launch images:ubuntu/24.04/cloud test-idmap && incus delete test-idmap --force
```

> **Note:** The range `1000000:65536` allocates 65536 UIDs starting at 1000000. This is the standard range used by Incus and does not conflict with normal host UIDs (0–65535).
