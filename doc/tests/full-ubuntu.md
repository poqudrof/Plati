# Full Ubuntu Image + Mixins UI Test

End-to-end checklist for verifying that the Ubuntu template and all its mixins work correctly from first create through rebuild. Run this against a live dev or staging stack with a real Incus backend.

## Prerequisites

- Dev stack running (`make dev` or Docker Compose)
- Incus reachable from the backend (TLS cert configured)
- A Tailscale auth key configured in Admin → Settings → Tailscale Key
- An SSH key assigned to your test user (Admin → SSH Keys)
- The `dist/` assets for mixins present under `config/templates/mixins/dist/` (tailscale installer, sshx binary, openvscode-server tarball)

---

## Part 1 — Mixins UI (Admin Template Debug Page)

### 1.1 Import the Ubuntu template

1. Go to **Admin → Templates**.
2. Paste the contents of `config/templates/ubuntu.yaml` into the import textarea.
3. Click **Import**. Confirm `Ubuntu` appears in the template list with slug `ubuntu`.

### 1.2 Open the debug page

1. Click the template row to expand it, then click **Debug**.
2. Confirm the page loads with three sections: **Instance**, **Runner**, **Editor**.

### 1.3 Inspect the mixin list (Editor tab)

1. Switch to the **Editor** tab.
2. Under **Includes / Mixins**, verify all four mixins are checked: `tailscale`, `sshx`, `docker`, `openvscode-server`.
3. Click each mixin name to expand its details. Confirm:
   - **tailscale**: shows `post_create_commands` (iptables install, `tailscale-install.sh`, systemctl enable/start, `tailscale up`)
   - **sshx**: shows `post_create_commands` (daemon-reload, enable sshx, start sshx)
   - **docker**: shows `post_create_commands` (apt install docker-ce, fuse-overlayfs daemon.json, systemctl start)
   - **openvscode-server**: shows `post_create_commands` (tar extract to `/opt/openvscode-server`, systemctl enable/start)
4. Toggle a mixin off (e.g. `docker`) and back on. Confirm the includes list updates live.
5. Do **not** save — click **Reset** or navigate away without saving to leave the template unchanged.

### 1.4 Create a debug instance

1. Switch to the **Instance** tab.
2. Click **Create debug instance**.
3. The status badge changes to `creating` and the creation log starts streaming.
4. Watch the log — confirm steps appear in order: base image launch → `first_init_commands` (apt-get, ssh config) → mixin `post_create_commands` for each mixin.
5. No step should show a red error badge. Warnings (yellow) on non-critical steps are acceptable.
6. When status reaches `running`, note the IP address shown.

### 1.5 Run a command via the Runner tab

1. Switch to the **Runner** tab.
2. In the command input, enter:
   ```
   systemctl is-active tailscaled sshx docker openvscode-server
   ```
3. Click **Run**. Confirm all four services report `active`.

### 1.6 OpenVSCode via Tailscale Serve (debug page)

1. Back on the **Instance** tab, find the **OpenVSCode Server** section.
2. Confirm the direct link `http://<ip>:3463` is shown.
3. Click **Serve via Tailscale**. Wait for the status badge to turn active.
4. Click **Open in VSCode ↗** — the browser tab should open the OpenVSCode web UI connected through the Tailscale serve URL.
5. Click **Stop Serve** to clean up.

### 1.7 Delete the debug instance

1. Click **Delete instance** and confirm. Status should clear and the button resets to **Create debug instance**.

---

## Part 2 — Full User Flow (Ubuntu Instance Lifecycle)

### 2.1 Create an instance

1. Navigate to the main **Instances** page as a regular user.
2. Click **New instance**, select the **Ubuntu** template.
3. Leave all fields at defaults and click **Create**.
4. The instance page opens. Confirm status is `creating` and the creation log streams.
5. All steps should complete without errors. Instance reaches `running`.

### 2.2 SSH access

```bash
ssh ubuntu@<instance-ip>
```

- Login succeeds using your assigned SSH key (no password prompt).
- `whoami` returns `ubuntu`.
- `/home/ubuntu` is the persistent volume, mounted and writable:
  ```bash
  touch ~/test-file && ls ~/test-file
  ```

### 2.3 Docker

From inside the instance:

```bash
docker run --rm hello-world
```

Expected: Docker pulls and runs `hello-world`, prints the success message. The `fuse-overlayfs` storage driver is in use — confirm with `docker info | grep 'Storage Driver'`.

### 2.4 sshx (web terminal)

1. On the instance page, click the **sshx** tab.
2. An sshx session URL should be displayed.
3. Open the URL in a browser. A web terminal connected to the instance should appear.
4. Run `hostname` — should match the instance name.

### 2.5 Tailscale

1. Click the **Tailscale** tab.
2. Status should show **Connected** with a Tailscale DNS name (e.g. `<instance>.your-tailnet.ts.net`).
3. From a device on the same tailnet, `ping <tailscale-dns-name>` should succeed.
4. SSH via Tailscale:
   ```bash
   ssh ubuntu@<tailscale-dns-name>
   ```

### 2.6 OpenVSCode Server — browser

1. Click the **OpenVSCode** tab.
2. Under **Open in browser**, the URL `http://<ip>:3463` is shown.
3. Open it — the OpenVSCode web UI loads. Open `/home/ubuntu` (the persistent volume).
4. Create a file in the editor, confirm it persists on disk.

### 2.7 OpenVSCode Server — VSCode Desktop

1. On the same **OpenVSCode** tab, under **Open in VSCode Desktop**, confirm the button is shown (requires Tailscale to be connected).
2. Click **Open in VSCode Desktop**. VSCode should open the remote SSH session to `ubuntu@<tailscale-dns-name>` at `/home/ubuntu`.
3. The Remote SSH status bar indicator in VSCode shows the host name.

### 2.8 Rebuild (home persistence)

1. Create a sentinel file:
   ```bash
   echo "rebuild-test" > ~/rebuild-marker.txt
   ```
2. On the instance page, click **Rebuild** and confirm.
3. Wait for the instance to return to `running`.
4. SSH back in and verify:
   ```bash
   cat ~/rebuild-marker.txt   # must print "rebuild-test"
   ```
5. Confirm `first_init_commands` did **not** re-run (no re-install of packages — check `/var/log/apt/history.log` timestamp or absence of a second apt-get run in the rebuild log).
6. Confirm `rebuild_commands` ran: SSH service is up (`systemctl is-active ssh` → `active`).

---

## Part 3 — Cleanup

1. Delete the instance from the instance page.
2. Confirm the instance is removed from the list.
3. If the debug instance from Part 1 was not already deleted, delete it from the admin debug page.
4. Optionally delete the Ubuntu template from Admin → Templates if it was imported only for this test.

---

## Pass Criteria

| Check | Pass condition |
|-------|---------------|
| Template import | No error, slug `ubuntu` appears |
| Mixin list in editor | All 4 mixins visible with correct commands |
| Debug instance creation | All steps green, status → `running` |
| Runner: service check | `active` for all 4 services |
| Tailscale Serve (debug) | URL opens OpenVSCode web UI |
| SSH login | Key auth, no password |
| Docker hello-world | Runs and prints success |
| sshx web terminal | URL opens, `hostname` correct |
| Tailscale connected | DNS name shown, ping succeeds |
| OpenVSCode browser | Web UI loads on port 3463 |
| OpenVSCode Desktop | VSCode Remote SSH session opens |
| Rebuild persistence | `/home/ubuntu/rebuild-marker.txt` survives |
| Cleanup | Instance and volumes deleted cleanly |
