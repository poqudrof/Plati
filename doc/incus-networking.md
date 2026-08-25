# Incus Container Networking — Diagnostics & Fixes

This document captures the diagnostic process and known fixes for container networking
issues on a host running both Incus and Docker.

## Architecture

```
container (10.98.141.x)
    │
    │ eth0 (veth pair)
    ▼
incusbr0 (10.98.141.1/24)   ← Incus managed bridge, dnsmasq for DHCP/DNS
    │
    │ NAT masquerade (inet incus table, pstrt.incusbr0 chain)
    ▼
eth0 (host outbound interface, e.g. 51.75.195.135)
    │
    ▼
internet
```

Incus manages its own nftables table (`inet incus`) with four chains per bridge:

| Chain | Hook | Purpose |
|-------|------|---------|
| `pstrt.incusbr0` | postrouting/srcnat | Masquerade container IPs to host IP |
| `fwd.incusbr0` | forward/filter | Accept forwarded traffic in/out of bridge |
| `in.incusbr0` | input/filter | Accept DNS, DHCP, ICMP from containers |
| `out.incusbr0` | output/filter | Accept DNS, DHCP, ICMP to containers |

## Diagnostic flowchart

### Step 1 — can the container reach the bridge gateway?

```bash
sudo incus exec <name> -- ping -c2 10.98.141.1
```

- **No replies** → bridge or profile not set up. See [Missing bridge](#missing-bridge).
- **Replies** → container networking is fine up to the host. Continue to step 2.

### Step 2 — can the container reach the internet?

```bash
sudo incus exec <name> -- ping -c2 1.1.1.1
```

- **"Network is unreachable"** → container has no IP or no default route.
  Check: `sudo incus exec <name> -- ip addr && ip route`
- **No replies (timeout)** → packets leave the container but are dropped on the host.
  Continue to step 3.
- **Replies** → networking is working.

### Step 3 — check nftables rules

```bash
# Verify Incus masquerade rule exists
sudo nft list table inet incus | grep -A4 "pstrt"
```

Expected:
```
chain pstrt.incusbr0 {
    type nat hook postrouting priority srcnat; policy accept;
    ip saddr 10.98.141.0/24 ip daddr != 10.98.141.0/24 masquerade
```

If the chain is missing, restart Incus: `sudo systemctl restart incus`

### Step 4 — check FORWARD chain policy

```bash
sudo nft list table ip filter | grep -A3 "chain FORWARD"
```

- **`policy accept`** → forwarding is open. Jump to [RPF check](#rp_filter-reverse-path-filtering).
- **`policy drop`** → Docker is blocking Incus traffic. See [Docker conflict](#docker-forward-chain-conflict).

### Step 5 — confirm masquerade rule is being hit

Run a ping in the background and watch the packet counter:

```bash
sudo incus exec <name> -- ping -c5 1.1.1.1 &
sudo nft -a list table inet incus | grep -A4 "pstrt"
```

If the counter on the masquerade rule doesn't increment, the packet is being dropped
before it reaches postrouting — likely a FORWARD chain issue.

---

## Known issues and fixes

### Missing bridge

`incus admin init --minimal` skips network setup. Create the bridge manually:

```bash
incus network create incusbr0
incus profile device add default eth0 nic nictype=bridged parent=incusbr0
```

Verify:
```bash
incus profile show default     # must have eth0 device
incus network show incusbr0    # must show ipv4.nat: "true"
```

### IP forwarding disabled

```bash
sysctl net.ipv4.ip_forward   # must be 1
```

Fix persistently:

```bash
echo "net.ipv4.ip_forward=1" | sudo tee /etc/sysctl.d/99-incus.conf
sudo sysctl -p /etc/sysctl.d/99-incus.conf
```

### Docker FORWARD chain conflict

When Docker is running alongside Incus, Docker registers a `FORWARD` chain in the
`ip filter` table with `policy drop`. Both Docker's and Incus's forward chains register
at the same nftables hook priority (`filter = 0`). If Docker's chain was registered
first, it evaluates before Incus's and drops all non-Docker bridge traffic.

**Diagnosis:**

```bash
sudo nft list table ip filter | grep -A3 "chain FORWARD"
# policy drop → Docker conflict confirmed
```

**Fix — insert rules into DOCKER-USER:**

Docker preserves the `DOCKER-USER` chain across daemon restarts. Rules added here are
evaluated before Docker's drop policy:

```bash
sudo iptables -I DOCKER-USER -i incusbr0 -j ACCEPT
sudo iptables -I DOCKER-USER -o incusbr0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
```

**Persist across reboots:**

```bash
sudo sh -c 'iptables-save > /etc/iptables/iptables.rules'
sudo systemctl enable --now iptables
```

> **Important:** Only run `iptables-save` while Docker is running and its chains are
> intact. If you save when Docker's chains (`DOCKER-FORWARD`, `DOCKER-USER`, etc.)
> are missing, Docker will fail to create networks on next startup with:
> `iptables: No chain/target/match by that name`. Fix: `sudo systemctl restart docker`
> to recreate the chains, re-add the Incus rules above, then re-save.

> **Note:** Restarting the Docker daemon resets its `FORWARD` chain to `policy drop`
> and re-registers its chains, which can re-break Incus networking. The `DOCKER-USER`
> rules survive a Docker restart; a full host reboot requires the `iptables` service
> to restore them.

### rp_filter (Reverse Path Filtering)

If `eth0` or `incusbr0` have `rp_filter = 1` (strict mode), the kernel drops packets
whose reverse path doesn't match the incoming interface — which breaks NAT'd container
traffic.

```bash
sysctl net.ipv4.conf.eth0.rp_filter
sysctl net.ipv4.conf.incusbr0.rp_filter
```

Fix (set to loose mode):

```bash
sudo sysctl -w net.ipv4.conf.eth0.rp_filter=2
sudo sysctl -w net.ipv4.conf.incusbr0.rp_filter=2
```

Persist:

```bash
cat <<'EOF' | sudo tee /etc/sysctl.d/99-incus-rpf.conf
net.ipv4.conf.eth0.rp_filter=2
net.ipv4.conf.incusbr0.rp_filter=2
EOF
sudo sysctl -p /etc/sysctl.d/99-incus-rpf.conf
```

---

## Full reset checklist

If networking is completely broken and you want to start clean:

```bash
# 1. Delete and recreate the bridge
incus network delete incusbr0
incus network create incusbr0 ipv4.address=10.100.0.1/24 ipv4.nat=true ipv6.address=none

# 2. Re-attach to default profile (skip if device already removed)
incus profile device remove default eth0 2>/dev/null || true
incus profile device add default eth0 nic nictype=bridged parent=incusbr0

# 3. Restart Incus to re-register all nftables chains
sudo systemctl restart incus

# 4. Re-add DOCKER-USER rules if Docker is present
sudo iptables -I DOCKER-USER -i incusbr0 -j ACCEPT
sudo iptables -I DOCKER-USER -o incusbr0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT

# 5. Smoke test
incus launch images:ubuntu/24.04/cloud net-test
incus exec net-test -- ping -c2 1.1.1.1
incus delete net-test --force
```
