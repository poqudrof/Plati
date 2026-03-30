# Incus Server Configuration for Plati

Step-by-step setup to connect Plati to a local Incus server.

## 1. Install Incus

```bash
# Arch/Manjaro
sudo pacman -S incus

# Enable and start the daemon
sudo systemctl enable --now incus
```

## 2. Initialize Incus

```bash
sudo incus admin init
```

Recommended answers:

| Prompt | Answer |
|--------|--------|
| Clustering | no |
| Configure storage pool | yes |
| Pool name | default |
| **Storage backend** | **dir** (btrfs may fail with loop devices) |
| Network bridge | yes |
| Bridge name | incusbr0 |
| IPv4 | auto |
| IPv6 | auto |
| Available over network | no (unless remote access needed) |
| Update cached images | yes |

## 3. Fix UID/GID Mapping

Required for unprivileged containers:

```bash
sudo usermod --add-subuids 1000000-1065535 --add-subgids 1000000-1065535 root
sudo systemctl restart incus
```

Verify:
```bash
grep root /etc/subuid /etc/subgid
# Should show: root:1000000:65536
```

## 4. Enable HTTPS Listener

```bash
sudo incus config set core.https_address :8443
```

## 5. Generate TLS Client Certificate

```bash
# Generate EC cert + key (PEM format)
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:secp384r1 -sha384 \
  -keyout config/plati-client.key.tmp \
  -out config/plati-client.crt \
  -nodes -days 3650 -subj "/CN=plati"

# Convert key to PEM (openssl 3.x outputs OpenSSH format by default)
openssl ec -in config/plati-client.key.tmp -out config/plati-client.key -outform PEM
rm config/plati-client.key.tmp
```

## 6. Trust the Certificate on Incus

Newer Incus versions use token-based trust:

```bash
# Generate a trust token
sudo incus config trust add plati
# Outputs a token like: eyJjbGll...

# Register the cert using the token via API
curl -sk \
  --cert config/plati-client.crt \
  --key config/plati-client.key \
  -X POST https://127.0.0.1:8443/1.0/certificates \
  -H 'Content-Type: application/json' \
  -d '{"type":"client","trust_token":"<paste-token-here>"}'
```

Verify trust:
```bash
curl -sk \
  --cert config/plati-client.crt \
  --key config/plati-client.key \
  https://127.0.0.1:8443/1.0 \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['metadata']['auth'])"
# Should print: trusted
```

## 7. Configure Plati

In `config/plati.yaml`, use **absolute paths** for TLS certs (the backend runs from `backend/` directory):

```yaml
servers:
  - name: local
    endpoint: https://127.0.0.1:8443
    tls_client_cert: "/absolute/path/to/config/plati-client.crt"
    tls_client_key: "/absolute/path/to/config/plati-client.key"
    max_instances: 50
```

## 8. Add User to incus-admin Group (optional)

To use `incus` CLI without sudo:

```bash
sudo usermod -aG incus-admin $USER
# Log out and back in, or:
newgrp incus-admin
```

## Troubleshooting

| Error | Fix |
|-------|-----|
| `tls: failed to find any PEM data in certificate input` | Key is in OpenSSH format, not PEM. Re-convert with `openssl ec -in key -out key -outform PEM` |
| `not authorized` | Client cert not trusted. Re-do step 6 |
| `certificate signed by unknown authority` | Add `InsecureSkipVerify: true` in client args (self-signed Incus cert) |
| `No root device could be found` | Default profile missing root disk. Run `incus admin init` |
| `System doesn't have a functional idmap setup` | Missing subuid/subgid. Run step 3 |
| `connection refused` on 8443 | HTTPS listener not enabled. Run step 4 |
