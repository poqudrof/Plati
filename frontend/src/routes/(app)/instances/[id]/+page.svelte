<script lang="ts">
  import { browser } from '$app/environment';
  import { page } from '$app/stores';
  import { instances, admin, templates } from '$lib/api';
  import type { Instance, Server, InstanceSecret, InstanceAuthorizedKey, IncusDetail, IncusConfigUpdate, Template, SshxStatusResult, TailscaleServeResult, TailscaleStatusResult, InstanceStorageInfo } from '$lib/api/types';
  import { goto } from '$app/navigation';
  import { addNotification } from '$lib/stores/notifications';
  import Terminal from '$lib/components/Terminal.svelte';
  import StorageTab from '$lib/components/StorageTab.svelte';
  import StatusTab from '$lib/components/StatusTab.svelte';
  import ResourcesTab from '$lib/components/ResourcesTab.svelte';
  import LinksTab from '$lib/components/LinksTab.svelte';
  import DangerZone from '$lib/components/DangerZone.svelte';
  import { currentUser } from '$lib/stores/auth';

  let instance = $state<Instance | null>(null);
  let template = $state<Template | null>(null);
  let server = $state<Server | null>(null);
  let incusDetail: IncusDetail | null = $state(null);
  let loading = $state(true);
  let actionLoading = $state(false);

  let configForm = $state<IncusConfigUpdate>({
    limits_cpu: '',
    limits_memory: '',
    limits_memory_swap: '',
    limits_processes: '',
    boot_autostart: '',
    boot_autostart_delay: '',
    security_nesting: '',
    security_privileged: ''
  });
  let configSaving = $state(false);

  let instanceSecrets: InstanceSecret[] = $state([]);
  let newSecretName = $state('');
  let newSecretValue = $state('');

  let sshxUrl = $state('');
  let sshxLoading = $state(false);
  let sshxRefreshTimer: ReturnType<typeof setTimeout> | null = null;

  // sshx is built in-house, so an instance can be running an older binary than the one the
  // server currently ships. update_available is decided server-side on the sha256, not on
  // the version string — a local rebuild does not necessarily move that string.
  let sshxStatus = $state<SshxStatusResult | null>(null);
  let sshxUpdating = $state(false);

  async function loadSshxStatus() {
    // Same guard as the other per-instance probes: the endpoint execs into the container,
    // so it answers 503 on a stopped one. A stopped instance therefore shows the tab only
    // when its template ships sshx — there is no way to look inside it.
    if (!browser || !instance || instance.status !== 'running') return;
    try {
      sshxStatus = await instances.sshxStatus(id);
    } catch {
      sshxStatus = null; // stopped mid-flight, or the endpoint is unavailable
    }
  }

  async function updateSshx() {
    sshxUpdating = true;
    try {
      const wasInstalled = sshxStatus?.installed ?? false;
      sshxStatus = await instances.sshxUpdate(id);
      if (!sshxStatus.service_active) {
        addNotification('error', 'SSHX was deployed but the service is not running');
      } else if (sshxStatus.update_available) {
        addNotification('error', sshxStatus.run_as !== sshxStatus.expected_run_as
          ? `SSHX still runs as ${sshxStatus.run_as}, not ${sshxStatus.expected_run_as}`
          : 'SSHX still reports a different binary after the update');
      } else {
        addNotification('success', wasInstalled ? 'SSHX updated' : 'SSHX installed');
      }
      // Restarting the service starts a new collaborative session, so the old URL is dead.
      sshxUrl = '';
      loadSshxUrl();
    } catch (e) {
      addNotification('error', e instanceof Error ? e.message : 'SSHX update failed');
    } finally {
      sshxUpdating = false;
    }
  }

  // Tailscale Serve
  let tsServeStatus = $state<TailscaleServeResult | null>(null);
  let tsServePort = $state(8080);
  let tsServeLoading = $state(false);

  // Inline rename (the name doubles as the Tailscale hostname)
  let renaming = $state(false);
  let renameValue = $state('');
  let renameSaving = $state(false);
  // Renaming the Incus container needs it stopped, so it is opt-in and off by
  // default. Renaming inside Ubuntu costs nothing, so it is on by default.
  let renameContainer = $state(false);
  let renameSystemHostname = $state(true);
  // Mirrors sanitizeName() in backend/internal/services/instance_service.go
  let renamePreview = $derived(
    renameValue.trim().toLowerCase().replace(/ /g, '-').replace(/[^a-z0-9-]/g, '').slice(0, 40)
  );

  // Tailscale machine status (DNS name)
  let tsStatus = $state<TailscaleStatusResult | null>(null);

  // Storage info
  let storageInfo = $state<InstanceStorageInfo | null>(null);

  // Terminal user toggle (root vs unprivileged)
  let terminalUser = $state('root');

  // Authorized keys (SSH access)
  let authorizedKeys: InstanceAuthorizedKey[] = $state([]);
  let authorizedKeysLoading = $state(false);
  let installingKeyId = $state(0);

  // Tabs below the main card
  type DetailTab = 'status' | 'storage' | 'resources' | 'incus' | 'secrets' | 'ssh-keys' | 'sshx' | 'tailscale' | 'openvscode' | 'links';
  // ?tab=links opens a tab directly — the dashboard card's "Manage" link lands on Links.
  let detailTab: DetailTab = $state(
    $page.url.searchParams.get('tab') === 'links' ? 'links' : 'status'
  );

  // Creation log stream (while status === 'creating')
  type LogStep = { kind: 'step'; n: number; total: number; label: string; outputs: string[]; warning: string; expanded: boolean };
  type LogLine = { kind: 'log'; text: string };
  type LogItem = LogStep | LogLine;

  let logItems = $state<LogItem[]>([]);
  let creationLogEl: HTMLDivElement | undefined = $state();

  let id = $derived(Number($page.params.id));
  let isAdmin = $derived($currentUser?.role === 'admin');

  // An admin can open anyone's workspace, so the page must say whose it is before they
  // start or rebuild it. The owner's email comes from the admin listing, which already
  // resolves it — no extra endpoint needed.
  // Set in load(), where the instance is already resolved: a $derived guarded on
  // `instance != null` trips this file's existing narrowing problem with $state(null).
  let ownerEmail = $state('');
  let isForeign = $state(false);
  let incusUIUrl = $derived(server ? `${server.endpoint}/ui` : null);
  let includes = $derived((() => { try { return JSON.parse(template?.includes ?? '[]') as string[]; } catch { return [] as string[]; } })());
  // Shown when the template ships the mixin, or when the instance turns out to carry the
  // binary anyway: sshx can be installed into a workspace whose template does not include
  // it, and that install would otherwise have no UI at all — not even to update it.
  let hasSshx = $derived(includes.includes('sshx') || (sshxStatus?.installed ?? false));
  let hasTailscale = $derived(includes.includes('tailscale'));
  // The login user and their home, shared by the Storage, SSH and OpenVSCode tabs.
  let termUser = $derived(template?.terminal_user || 'root');
  let homeDir = $derived(termUser === 'root' ? '/root' : `/home/${termUser}`);
  let hasVSCode = $derived(includes.includes('openvscode-server'));

  async function load() {
    loading = true;
    try {
      instance = await instances.get(id);
      [instanceSecrets, template, storageInfo] = await Promise.all([
        instances.listSecrets(id),
        templates.get(instance.template_id),
        instances.volumes(id).catch(() => null)
      ]);
      if ($currentUser?.role === 'admin' && instance) {
        isForeign = instance.user_id !== $currentUser.id;
        if (isForeign) {
          const all = await admin.instances.list().catch(() => []);
          ownerEmail = all.find(i => i.id === id)?.user_email ?? '';
        }
        const servers = await admin.servers.list();
        server = servers.find(s => s.id === instance!.server_id) ?? null;
        incusDetail = await admin.instances.incusInfo(id).catch(() => null);
        if (incusDetail) {
          configForm = {
            limits_cpu: incusDetail.limits_cpu ?? '',
            limits_memory: incusDetail.limits_memory ?? '',
            limits_memory_swap: incusDetail.limits_memory_swap ?? '',
            limits_processes: incusDetail.limits_processes ?? '',
            boot_autostart: incusDetail.boot_autostart ?? '',
            boot_autostart_delay: incusDetail.boot_autostart_delay ?? '',
            security_nesting: incusDetail.security_nesting ?? '',
            security_privileged: incusDetail.security_privileged ?? ''
          };
        }
      }
      if (browser && instance?.status === 'running') {
        loadSshxUrl();
        // Probed at load, not only when the SSHX tab is clicked: the tab is what this
        // answer decides to show, so waiting for a click on it would never happen for an
        // instance whose template does not ship the mixin.
        loadSshxStatus();
        loadTsServeStatus();
        loadTsStatus();
      }
    } catch (e: any) {
      addNotification('error', 'Instance not found');
      goto('/dashboard');
    } finally {
      loading = false;
    }
  }

  async function loadAuthorizedKeys() {
    if (!browser) return;
    authorizedKeysLoading = true;
    try {
      authorizedKeys = await instances.listAuthorizedKeys(id);
    } catch (e: any) {
      addNotification('error', 'Failed to load public keys: ' + e.message);
    } finally { authorizedKeysLoading = false; }
  }

  // The owner's own keys are listed first; server admins' keys are offered separately
  // so granting an administrator access is a deliberate, clearly-labelled action.
  let ownerKeys = $derived(authorizedKeys.filter(k => !k.is_admin));
  let adminKeys = $derived(authorizedKeys.filter(k => k.is_admin));
  let showAdminKeys = $state(false);

  async function installAuthorizedKey(key: InstanceAuthorizedKey) {
    installingKeyId = key.id;
    try {
      await instances.addAuthorizedKey(id, key.id);
      addNotification('success', `Key "${key.name}" added to authorized_keys`);
      await loadAuthorizedKeys();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally { installingKeyId = 0; }
  }

  async function loadSshxUrl() {
    // Cleared before the guard, not after it: an instance that stops while the page is
    // open must cancel the pending retry, rather than leave one armed to fire into a
    // container that is no longer there.
    if (sshxRefreshTimer) { clearTimeout(sshxRefreshTimer); sshxRefreshTimer = null; }
    if (!browser || !instance || instance.status !== 'running') return;
    sshxLoading = true;
    try {
      const result = await instances.sshxUrl(id);
      sshxUrl = result.url;
      if (!sshxUrl) sshxRefreshTimer = setTimeout(loadSshxUrl, 5000);
    } catch { /* 503 = stopped mid-flight */ } finally { sshxLoading = false; }
  }

  async function loadTsServeStatus() {
    if (!browser || !instance || instance.status !== 'running') return;
    try {
      tsServeStatus = await instances.tailscaleServeStatus(id);
    } catch { tsServeStatus = null; }
  }

  async function loadTsStatus() {
    if (!browser || !instance || instance.status !== 'running') return;
    try { tsStatus = await instances.tailscaleStatus(id); } catch { tsStatus = null; }
  }

  function startRename() {
    if (!instance) return;
    renameValue = instance.name;
    renameContainer = false;
    renameSystemHostname = true;
    renaming = true;
  }

  // The container name is derived from the owner and the sanitized name, exactly
  // as the backend builds it — shown so the change is visible before saving.
  let renameIncusPreview = $derived(
    instance && renamePreview ? `plati-${instance.user_id}-${renamePreview}` : ''
  );
  let renameIncusChanges = $derived(
    !!instance && !!renameIncusPreview && renameIncusPreview !== instance.incus_name
  );

  async function saveRename() {
    if (!instance || !renamePreview) { renaming = false; return; }
    if (renameValue === instance.name && !renameContainer && !renameSystemHostname) { renaming = false; return; }
    if (renameContainer && instance.status === 'running' &&
        !confirm('Renaming the container stops the instance and starts it again. Running processes and terminal sessions will be lost. Continue?')) {
      return;
    }
    renameSaving = true;
    try {
      const result = await instances.rename(id, renameValue.trim(), {
        rename_container: renameContainer,
        rename_system_hostname: renameSystemHostname
      });
      instance = result;
      renaming = false;
      if (renameContainer && !result.container_renamed) {
        addNotification('error', `Renamed, but the container was not: ${result.container_rename_error ?? 'unknown error'}`);
      } else {
        const applied = [`tailnet hostname ${renamePreview}`];
        if (result.container_renamed) applied.push(`container ${result.incus_name}`);
        if (result.system_hostname) applied.push(`Ubuntu hostname ${result.system_hostname}`);
        addNotification('success',
          `Renamed — ${applied.join(', ')}` + (result.restarted ? ' (instance restarted)' : ''));
      }
      // A requested Ubuntu rename that came back with nothing did not reach the
      // instance — say so rather than letting the success message imply it did.
      if (renameSystemHostname && !result.system_hostname) {
        addNotification('error', 'The hostname inside Ubuntu could not be applied — the instance must be running');
      }
      loadTsStatus();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally { renameSaving = false; }
  }

  // Tailscale can be unusable three ways: never installed, daemon down, or logged out
  // (no auth key at create time). All three are fixed by re-applying the mixin.
  let tsInstalling = $state(false);
  let tsNeedsInstall = $derived(
    instance?.status === 'running' && hasTailscale &&
    (!tsStatus || !tsStatus.installed || !tsStatus.daemon_active || !tsStatus.connected)
  );
  let tsProblem = $derived(
    !tsStatus || !tsStatus.installed ? 'Tailscale is not installed in this instance.'
    : !tsStatus.daemon_active ? 'Tailscale is installed but tailscaled is not running.'
    : 'Tailscale is installed but this machine is not logged in to the tailnet.'
  );

  async function installTailscale() {
    tsInstalling = true;
    try {
      tsStatus = await instances.tailscaleInstall(id);
      if (tsStatus.connected) {
        addNotification('success', `Tailscale connected as ${tsStatus.dns_name}`);
      } else if (!tsStatus.installed) {
        addNotification('error', 'Tailscale could not be installed — see the panel for details');
      } else if (!tsStatus.daemon_active) {
        addNotification('error', 'tailscaled did not start — see the panel for details');
      } else {
        // The auth key is the usual culprit: single-use keys are spent by the
        // first machine, so a duplicated instance cannot log in with the same one.
        addNotification('error', 'The auth key was refused — see the panel for what Tailscale said');
      }
    } catch (e: any) {
      addNotification('error', e.message);
    } finally { tsInstalling = false; }
  }

  async function startTsServe() {
    if (!tsServePort || tsServePort <= 0) return;
    tsServeLoading = true;
    try {
      tsServeStatus = await instances.tailscaleServe(id, tsServePort);
      addNotification('success', 'Tailscale Serve started');
    } catch (e: any) {
      addNotification('error', e.message);
    } finally { tsServeLoading = false; }
  }

  async function stopTsServe() {
    tsServeLoading = true;
    try {
      await instances.tailscaleServeOff(id);
      tsServeStatus = { status: 'off', url: '', port: 0 };
      addNotification('success', 'Tailscale Serve stopped');
    } catch (e: any) {
      addNotification('error', e.message);
    } finally { tsServeLoading = false; }
  }

  async function startVsCodeServe() {
    tsServeLoading = true;
    try {
      tsServeStatus = await instances.tailscaleServe(id, 3463);
      addNotification('success', 'Tailscale Serve started on port 3463');
    } catch (e: any) {
      addNotification('error', e.message);
    } finally { tsServeLoading = false; }
  }

  async function addSecret() {
    if (!newSecretName || !newSecretValue) return;
    try {
      await instances.createSecret(id, newSecretName, newSecretValue);
      newSecretName = '';
      newSecretValue = '';
      instanceSecrets = await instances.listSecrets(id);
      addNotification('success', 'Secret added — rebuild to apply');
    } catch (e: any) {
      addNotification('error', e.message);
    }
  }

  async function removeSecret(secretId: number) {
    try {
      await instances.deleteSecret(id, secretId);
      instanceSecrets = await instances.listSecrets(id);
      addNotification('success', 'Secret removed — rebuild to apply');
    } catch (e: any) {
      addNotification('error', e.message);
    }
  }

  async function action(fn: () => Promise<unknown>, msg: string) {
    actionLoading = true;
    try {
      await fn();
      addNotification('success', msg);
      await load();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      actionLoading = false;
    }
  }

  async function applyIncusConfig() {
    configSaving = true;
    try {
      await admin.instances.updateIncusConfig(id, configForm);
      addNotification('success', 'Config applied');
      incusDetail = await admin.instances.incusInfo(id).catch(() => null);
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      configSaving = false;
    }
  }

  function formatBytes(bytes: number): string {
    if (!bytes) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
  }

  $effect(() => {
    if (browser && id) {
      load();
    }
  });

  function parseLogLine(raw: string): void {
    // Strip \r\n / ANSI terminal codes from the end
    const line = raw.replace(/\r?\n$/, '').replace(/\x1b\[[0-9;]*m/g, '').replace(/\r/g, '');
    if (!line) return;

    if (line.startsWith('STEP:')) {
      // Format: STEP:N/M:label text
      const rest = line.slice(5);
      const slashIdx = rest.indexOf('/');
      const colonIdx = rest.indexOf(':', slashIdx + 1);
      const n = parseInt(rest.slice(0, slashIdx));
      const total = parseInt(rest.slice(slashIdx + 1, colonIdx));
      const label = rest.slice(colonIdx + 1);
      logItems.push({ kind: 'step', n, total, label, outputs: [], warning: '', expanded: false });
    } else if (line.startsWith('OUT:')) {
      const text = line.slice(4);
      const last = logItems[logItems.length - 1];
      if (last?.kind === 'step') {
        last.outputs.push(text);
      }
    } else if (line.startsWith('WARN:')) {
      const text = line.slice(5);
      const last = logItems[logItems.length - 1];
      if (last?.kind === 'step') {
        last.warning = text;
      } else {
        logItems.push({ kind: 'log', text: '⚠ ' + text });
      }
    } else {
      logItems.push({ kind: 'log', text: line });
    }

    // Auto-scroll
    setTimeout(() => {
      if (creationLogEl) creationLogEl.scrollTop = creationLogEl.scrollHeight;
    }, 0);
  }

  // While the instance is being created: stream the creation log and poll for completion.
  $effect(() => {
    if (!browser || instance?.status !== 'creating') return;

    logItems = [];

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}/api/v1/instances/${id}/creation-stream`);
    ws.onmessage = (e) => parseLogLine(e.data);

    const timer = setInterval(async () => {
      try {
        const updated = await instances.get(id);
        if (updated.status !== 'creating') {
          clearInterval(timer);
          await load();
        }
      } catch { /* ignore transient errors */ }
    }, 3000);

    return () => {
      ws.close();
      clearInterval(timer);
    };
  });
</script>

{#if loading}
  <div class="flex justify-center py-12">
    <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
  </div>
{:else if instance}
  <div>
    <div class="flex justify-between items-center mb-6">
      {#if renaming}
        <div class="flex items-start gap-2">
          <div>
            <!-- svelte-ignore a11y_autofocus -->
            <input
              class="text-2xl font-bold border-b-2 border-primary focus:outline-none bg-transparent"
              bind:value={renameValue}
              disabled={renameSaving}
              onkeydown={(e) => { if (e.key === 'Enter') saveRename(); if (e.key === 'Escape') renaming = false; }}
              autofocus
            />
            <p class="text-xs text-gray-500 mt-1">
              Tailnet hostname: <code class="bg-gray-100 px-1 rounded">{renamePreview || '—'}</code>
            </p>
            <label class="flex items-start gap-2 mt-2 text-xs text-gray-600">
              <input type="checkbox" bind:checked={renameSystemHostname}
                disabled={renameSaving || instance.status !== 'running'} class="mt-0.5" />
              <span>
                Rename the hostname inside Ubuntu
                {#if instance.status !== 'running'}
                  <span class="block text-gray-400">Needs the instance running.</span>
                {:else}
                  <span class="block text-gray-400">
                    Sets <code class="bg-gray-100 px-1 rounded">/etc/hostname</code> and the
                    <code class="bg-gray-100 px-1 rounded">/etc/hosts</code> entry, and applies it live — no reboot.
                  </span>
                {/if}
              </span>
            </label>
            <label class="flex items-start gap-2 mt-2 text-xs text-gray-600">
              <input type="checkbox" bind:checked={renameContainer} disabled={renameSaving} class="mt-0.5" />
              <span>
                Rename the Incus container too:
                <code class="bg-gray-100 px-1 rounded">{instance.incus_name}</code>
                {#if renameIncusChanges}→ <code class="bg-gray-100 px-1 rounded">{renameIncusPreview}</code>{/if}
                {#if renameContainer}
                  <span class="block text-amber-700 mt-1">
                    {#if instance.status === 'running'}
                      The instance is stopped and started again — running processes and terminal sessions are lost.
                    {:else}
                      The instance is stopped, so it renames directly.
                    {/if}
                    Storage volumes keep their current names.
                  </span>
                {/if}
              </span>
            </label>
          </div>
          <button
            class="px-3 py-1 text-sm bg-primary text-white rounded disabled:opacity-50"
            onclick={saveRename}
            disabled={renameSaving || !renamePreview}
          >{renameSaving ? 'Saving…' : 'Save'}</button>
          <button
            class="px-3 py-1 text-sm text-gray-600 hover:text-gray-900"
            onclick={() => renaming = false}
            disabled={renameSaving}
          >Cancel</button>
        </div>
      {:else}
        <div class="flex items-center gap-2">
          <h1 class="text-2xl font-bold">{instance.name}</h1>
          <button
            class="text-gray-400 hover:text-gray-700 text-sm"
            onclick={startRename}
            title="Rename (also changes the Tailscale hostname)"
            aria-label="Rename instance"
          >&#9998;</button>
        </div>
      {/if}
      <span class="px-3 py-1 rounded-full text-sm font-medium
        {instance.status === 'running' ? 'bg-green-100 text-green-800' : ''}
        {instance.status === 'stopped' ? 'bg-gray-100 text-gray-800' : ''}
        {instance.status === 'creating' ? 'bg-yellow-100 text-yellow-800' : ''}
        {instance.status === 'error' ? 'bg-red-100 text-red-800' : ''}
      ">
        {instance.status}
      </span>
    </div>

    <!-- Creation log: shown while instance is initialising -->
    {#if instance.status === 'creating'}
      <div class="bg-gray-950 rounded-lg border border-gray-800 overflow-hidden mb-6">
        <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-800">
          <div class="w-2 h-2 rounded-full bg-yellow-400 animate-pulse"></div>
          <span class="text-sm font-medium text-yellow-300">Creating instance…</span>
          <span class="text-xs text-gray-500 ml-auto font-mono">{instance.incus_name}</span>
        </div>
        <div
          bind:this={creationLogEl}
          class="p-4 font-mono text-xs leading-relaxed overflow-y-auto"
          style="min-height: 260px; max-height: 520px;"
        >
          {#if logItems.length === 0}
            <span class="text-gray-500">Waiting for creation log…</span>
          {:else}
            {#each logItems as item}
              {#if item.kind === 'log'}
                <div class="text-green-400 whitespace-pre-wrap py-px">{item.text}</div>
              {:else}
                <div class="my-0.5">
                  <button
                    class="w-full text-left flex items-center gap-2 px-1 py-0.5 rounded hover:bg-gray-900 group"
                    onclick={() => { if (item.outputs.length > 0) item.expanded = !item.expanded; }}
                  >
                    <span class="text-gray-600 shrink-0">[{item.n}/{item.total}]</span>
                    <span class="text-cyan-300 flex-1 truncate">{item.label}</span>
                    {#if item.warning}
                      <span class="text-yellow-400 shrink-0" title={item.warning}>⚠</span>
                    {/if}
                    {#if item.outputs.length > 0}
                      <span class="text-gray-600 shrink-0 text-xs">{item.outputs.length} line{item.outputs.length !== 1 ? 's' : ''}</span>
                      <span class="text-gray-600 shrink-0">{item.expanded ? '▾' : '▸'}</span>
                    {/if}
                  </button>
                  {#if item.expanded && item.outputs.length > 0}
                    <div class="ml-8 mt-1 mb-1 pl-2 border-l border-gray-800">
                      {#each item.outputs as out}
                        <div class="text-gray-400 whitespace-pre-wrap">{out || '\u00a0'}</div>
                      {/each}
                    </div>
                  {/if}
                  {#if item.warning}
                    <div class="ml-8 text-yellow-500 py-px">{item.warning}</div>
                  {/if}
                </div>
              {/if}
            {/each}
          {/if}
        </div>
      </div>
    {/if}

    {#if isForeign}
      <div class="mb-4 p-4 rounded-md bg-amber-50 border border-amber-200">
        <p class="text-sm text-amber-900">
          <span class="font-semibold">Administrator view.</span>
          This workspace belongs to {ownerEmail || 'another user'}. Start, stop, rebuild and
          delete act on their machine, and a rebuild reinstalls their keys and secrets — not yours.
        </p>
      </div>
    {/if}

    <!-- Main card: info + terminal + actions — always on top -->
    <div class="bg-white rounded-lg border p-6 space-y-6" class:opacity-50={instance.status === 'creating'}>
      <div class="grid grid-cols-2 gap-4">
        <div>
          <p class="text-sm text-gray-500">Incus Name</p>
          <p class="font-mono">{instance.incus_name}</p>
        </div>
        <div>
          <p class="text-sm text-gray-500">Created</p>
          <p>{new Date(instance.created_at).toLocaleString()}</p>
        </div>
      </div>

      {#if instance.ip_address}
        <div>
          <p class="text-sm text-gray-500 mb-1">SSH Connection</p>
          <div class="p-3 bg-gray-50 rounded font-mono text-sm">
            ssh user@{instance.ip_address}
          </div>
        </div>
      {/if}

      {#if instance.status === 'running'}
        <div class="pt-4 border-t">
          <p class="text-sm font-medium text-gray-700 mb-1">Terminal</p>
          {#if template?.terminal_user}
            <p class="text-xs text-gray-400 mb-3">
              Two contexts available — switch between them below.
            </p>
            <div class="flex gap-2 mb-3">
              <button onclick={() => terminalUser = 'root'}
                class="px-3 py-1.5 rounded text-sm font-mono font-semibold transition-colors
                  {terminalUser === 'root'
                    ? 'bg-amber-100 text-amber-800 border border-amber-300'
                    : 'bg-gray-100 text-gray-500 hover:bg-gray-200'}">
                root
              </button>
              <button onclick={() => terminalUser = template?.terminal_user ?? ''}
                class="px-3 py-1.5 rounded text-sm font-mono font-semibold transition-colors
                  {terminalUser === template?.terminal_user
                    ? 'bg-green-100 text-green-800 border border-green-300'
                    : 'bg-gray-100 text-gray-500 hover:bg-gray-200'}">
                {template.terminal_user}
              </button>
            </div>
            {#key terminalUser}
              <Terminal instanceId={id} user={terminalUser} />
            {/key}
          {:else}
            <p class="text-xs text-gray-400 mb-3">
              Connecting as <span class="font-mono text-amber-600">root</span>.
              Set <code class="bg-gray-100 px-1 rounded">terminal_user</code> in the template to also expose a user terminal.
            </p>
            <Terminal instanceId={id} user="root" />
          {/if}
        </div>
      {/if}

      <div class="flex gap-3 pt-4 border-t">
        {#if instance.status === 'stopped'}
          <button onclick={() => action(() => instances.start(id), 'Started')} disabled={actionLoading}
            class="px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700 disabled:opacity-50">Start</button>
        {/if}
        {#if instance.status === 'running'}
          <button onclick={() => action(() => instances.stop(id), 'Stopped')} disabled={actionLoading}
            class="px-4 py-2 bg-yellow-600 text-white rounded hover:bg-yellow-700 disabled:opacity-50">Stop</button>
        {/if}
        <!-- Rebuild and Delete deliberately do not sit next to Start/Stop: user instances
             are long-lived and both actions are destructive. They live in the Danger zone
             at the bottom of the Status tab, behind a typed-name confirmation. -->
      </div>
    </div>

    <!-- Tabbed detail panel -->
    <div class="bg-white rounded-lg border mt-6 overflow-hidden">

      <!-- Tab bar -->
      <div class="flex flex-wrap border-b">
        <button
          onclick={() => detailTab = 'status'}
          class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
            {detailTab === 'status' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
        >Status</button>
        <button
          onclick={() => detailTab = 'storage'}
          class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
            {detailTab === 'storage' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
        >Storage</button>
        <button
          onclick={() => detailTab = 'resources'}
          class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
            {detailTab === 'resources' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
        >Resources</button>
        {#if isAdmin}
          <button
            onclick={() => detailTab = 'incus'}
            class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
              {detailTab === 'incus' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
          >Incus Detail</button>
        {/if}
        <button
          onclick={() => detailTab = 'secrets'}
          class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
            {detailTab === 'secrets' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
        >Secrets</button>
        <button
          onclick={() => { detailTab = 'ssh-keys'; loadAuthorizedKeys(); }}
          class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
            {detailTab === 'ssh-keys' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
        >SSH Access</button>
        {#if hasSshx}
          <button
            onclick={() => { detailTab = 'sshx'; loadSshxStatus(); }}
            class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
              {detailTab === 'sshx' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
          >SSHX</button>
        {/if}
        {#if hasTailscale}
          <button
            onclick={() => detailTab = 'tailscale'}
            class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
              {detailTab === 'tailscale' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
          >Tailscale</button>
        {/if}
        {#if hasVSCode}
          <button
            onclick={() => detailTab = 'openvscode'}
            class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
              {detailTab === 'openvscode' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
          >OpenVSCode</button>
        {/if}
        <button
          onclick={() => detailTab = 'links'}
          class="px-3 sm:px-5 py-2.5 sm:py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors
            {detailTab === 'links' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
        >Links</button>
      </div>

      <!-- ── Status tab ── -->
      {#if detailTab === 'status'}
        <StatusTab
          instanceId={id}
          instanceStatus={instance?.status ?? 'stopped'}
          createdAt={instance.created_at}
        />
        <!-- Rebuild and Delete live here rather than beside Start/Stop: both are
             destructive, and an instance is long-lived. Delete is also reachable from
             Settings, but only for your own — an admin needs it here. -->
        <div class="px-6 pb-6">
          <DangerZone
            instanceId={id}
            instanceName={instance.name}
            {isForeign}
            {ownerEmail}
            onDone={load}
          />
        </div>
      {/if}

      <!-- ── Storage tab ── -->
      {#if detailTab === 'storage'}
        {#if storageInfo}
          <StorageTab
            instanceId={id}
            instanceStatus={instance?.status ?? 'stopped'}
            storageVolumes={storageInfo.volumes}
            persistenceMode={storageInfo.persistence_mode}
            {homeDir}
          />
        {:else}
          <div class="p-6">
            <p class="text-sm text-gray-500">Storage information not available.</p>
          </div>
        {/if}
      {/if}

      <!-- ── Resources tab ── -->
      {#if detailTab === 'resources'}
        <ResourcesTab
          instanceId={id}
          instanceStatus={instance?.status ?? 'stopped'}
          canEdit={isAdmin}
          onSaved={load}
        />
      {/if}

      <!-- ── Incus Detail tab ── -->
      {#if detailTab === 'incus' && isAdmin}
        <div class="p-6">
          {#if incusDetail}
            <div class="flex justify-between items-center mb-4">
              <h3 class="text-base font-semibold">Incus Detail</h3>
              {#if incusUIUrl}
                <a href={incusUIUrl} target="_blank" rel="noopener noreferrer"
                  class="px-3 py-1 rounded text-sm font-medium bg-primary-50 text-primary-dark border border-primary/20 hover:bg-primary-50">
                  Open Incus UI ↗
                </a>
              {/if}
            </div>

            <div class="grid grid-cols-2 gap-4 mb-4">
              <div>
                <p class="text-xs text-gray-500 uppercase tracking-wide">Type</p>
                <p class="font-mono text-sm">{incusDetail.type || '—'}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500 uppercase tracking-wide">Architecture</p>
                <p class="font-mono text-sm">{incusDetail.architecture || '—'}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500 uppercase tracking-wide">CPU Limit</p>
                <p class="font-mono text-sm">{incusDetail.limits_cpu || 'unlimited'}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500 uppercase tracking-wide">Memory Limit</p>
                <p class="font-mono text-sm">{incusDetail.limits_memory || 'unlimited'}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500 uppercase tracking-wide">Profiles</p>
                <p class="font-mono text-sm">{incusDetail.profiles?.join(', ') || '—'}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500 uppercase tracking-wide">Processes</p>
                <p class="font-mono text-sm">{incusDetail.processes ?? '—'}</p>
              </div>
            </div>

            {#if instance.status === 'running'}
              <div class="border-t pt-4 mb-4">
                <p class="text-xs text-gray-500 uppercase tracking-wide mb-2">Resource Usage</p>
                <div class="grid grid-cols-2 gap-4">
                  <div>
                    <p class="text-xs text-gray-500">CPU time</p>
                    <p class="font-mono text-sm">{(incusDetail.cpu_usage_ns / 1e9).toFixed(2)}s</p>
                  </div>
                  <div>
                    <p class="text-xs text-gray-500">Memory</p>
                    <p class="font-mono text-sm">{formatBytes(incusDetail.memory_usage)} / peak {formatBytes(incusDetail.memory_peak)}</p>
                  </div>
                  {#each Object.entries(incusDetail.disk_usage ?? {}) as [dev, usage]}
                    <div>
                      <p class="text-xs text-gray-500">Disk ({dev})</p>
                      <p class="font-mono text-sm">{formatBytes(usage)}</p>
                    </div>
                  {/each}
                </div>
              </div>

              <div class="border-t pt-4 mb-4">
                <p class="text-xs text-gray-500 uppercase tracking-wide mb-2">Network</p>
                <div class="space-y-3">
                  {#each Object.entries(incusDetail.network ?? {}).filter(([iface]) => iface !== 'lo') as [iface, net]}
                    <div class="p-3 bg-gray-50 rounded">
                      <p class="text-sm font-medium mb-1">{iface}</p>
                      <div class="grid grid-cols-2 gap-2 text-xs font-mono text-gray-600">
                        <span>↓ {formatBytes(net.rx_bytes)}</span>
                        <span>↑ {formatBytes(net.tx_bytes)}</span>
                      </div>
                      <div class="mt-1 space-y-0.5">
                        {#each net.addresses.filter(a => a.scope === 'global') as addr}
                          <p class="text-xs font-mono text-gray-500">{addr.address} ({addr.family})</p>
                        {/each}
                      </div>
                    </div>
                  {/each}
                </div>
              </div>
            {/if}

            <div class="border-t pt-4">
              <h4 class="text-sm font-semibold text-gray-700 mb-3">Edit Config</h4>
              <p class="text-xs text-gray-500 mb-3">
                CPU and memory are also on the Resources tab, with the template's values for
                comparison; edits here are equivalent and equally persistent. The other keys
                below live only in Incus and are reverted by a rebuild.
              </p>
              <div class="grid grid-cols-2 gap-3">
                <div>
                  <label for="cfg-limits-cpu" class="block text-xs text-gray-500 mb-1">limits.cpu</label>
                  <input id="cfg-limits-cpu" bind:value={configForm.limits_cpu} placeholder="e.g. 2 or 1-4"
                    class="w-full px-2 py-1.5 border rounded text-sm font-mono" />
                </div>
                <div>
                  <label for="cfg-limits-memory" class="block text-xs text-gray-500 mb-1">limits.memory</label>
                  <input id="cfg-limits-memory" bind:value={configForm.limits_memory} placeholder="e.g. 4GB"
                    class="w-full px-2 py-1.5 border rounded text-sm font-mono" />
                </div>
                <div>
                  <label for="cfg-limits-processes" class="block text-xs text-gray-500 mb-1">limits.processes</label>
                  <input id="cfg-limits-processes" bind:value={configForm.limits_processes} placeholder="integer or empty"
                    class="w-full px-2 py-1.5 border rounded text-sm font-mono" />
                </div>
                <div>
                  <label for="cfg-boot-delay" class="block text-xs text-gray-500 mb-1">boot.autostart.delay (seconds)</label>
                  <input id="cfg-boot-delay" bind:value={configForm.boot_autostart_delay} placeholder="integer or empty"
                    class="w-full px-2 py-1.5 border rounded text-sm font-mono" />
                </div>
                <div>
                  <label for="cfg-memory-swap" class="block text-xs text-gray-500 mb-1">limits.memory.swap</label>
                  <select id="cfg-memory-swap" bind:value={configForm.limits_memory_swap}
                    class="w-full px-2 py-1.5 border rounded text-sm">
                    <option value="">inherit</option>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </select>
                </div>
                <div>
                  <label for="cfg-boot-autostart" class="block text-xs text-gray-500 mb-1">boot.autostart</label>
                  <select id="cfg-boot-autostart" bind:value={configForm.boot_autostart}
                    class="w-full px-2 py-1.5 border rounded text-sm">
                    <option value="">inherit</option>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </select>
                </div>
                <div>
                  <label for="cfg-security-nesting" class="block text-xs text-gray-500 mb-1">security.nesting</label>
                  <select id="cfg-security-nesting" bind:value={configForm.security_nesting}
                    class="w-full px-2 py-1.5 border rounded text-sm">
                    <option value="">inherit</option>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </select>
                </div>
                <div>
                  <label for="cfg-security-privileged" class="block text-xs text-gray-500 mb-1">security.privileged</label>
                  <select id="cfg-security-privileged" bind:value={configForm.security_privileged}
                    class="w-full px-2 py-1.5 border rounded text-sm">
                    <option value="">inherit</option>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </select>
                  {#if configForm.security_privileged === 'true'}
                    <p class="text-xs text-red-600 mt-1">⚠ Removes container isolation — use with caution</p>
                  {/if}
                </div>
              </div>
              <p class="text-xs text-gray-400 mt-2">Empty value removes the key from Incus config (reverts to profile default).</p>
              <button onclick={applyIncusConfig} disabled={configSaving}
                class="mt-3 px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm">
                {configSaving ? 'Applying…' : 'Apply'}
              </button>
            </div>
          {:else}
            <p class="text-sm text-gray-500">Incus detail not available.</p>
          {/if}
        </div>
      {/if}

      <!-- ── Secrets tab ── -->
      {#if detailTab === 'secrets'}
        <div class="p-6">
          <h3 class="text-base font-semibold mb-1">Instance Secrets</h3>
          <p class="text-sm text-gray-500 mb-4">
            Encrypted env vars specific to this instance. Override global secrets with the same name.
            Changes take effect after a <strong>rebuild</strong>.
          </p>

          <div class="space-y-2 mb-4">
            {#each instanceSecrets as secret}
              <div class="flex justify-between items-center p-3 bg-gray-50 rounded">
                <span class="font-mono text-sm">{secret.name}</span>
                <button onclick={() => removeSecret(secret.id)} class="text-red-600 hover:text-red-800 text-sm">Remove</button>
              </div>
            {/each}
            {#if instanceSecrets.length === 0}
              <p class="text-gray-500 text-sm">No instance-specific secrets configured.</p>
            {/if}
          </div>

          <div class="border-t pt-4 space-y-2">
            <input bind:value={newSecretName} placeholder="SECRET_NAME" class="w-full px-3 py-2 border rounded text-sm" />
            <input bind:value={newSecretValue} type="password" placeholder="Secret value" class="w-full px-3 py-2 border rounded text-sm" />
            <button onclick={addSecret} class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark text-sm">
              Add Secret
            </button>
          </div>
        </div>
      {/if}

      <!-- ── SSH Access tab ── -->
      {#if detailTab === 'ssh-keys'}
        <div class="p-6 space-y-4">
          <div>
            <h3 class="text-base font-semibold mb-1">SSH Access</h3>
            <p class="text-sm text-gray-500">
              Public keys registered for this instance's owner. Adding a key writes it to
              <code class="bg-gray-100 px-1 rounded">authorized_keys</code> in the running instance
              (root and the terminal user), so you can connect right away without a rebuild.
            </p>
          </div>

          {#if instance.ip_address}
            <div class="p-3 bg-gray-50 rounded font-mono text-sm">
              ssh {template?.terminal_user || 'root'}@{instance.ip_address}
            </div>
          {/if}

          {#if authorizedKeysLoading}
            <p class="text-sm text-gray-500">Loading keys…</p>
          {:else}
            {#if ownerKeys.length === 0}
              <p class="text-sm text-gray-500">
                No public key registered. Add one from your profile — or ask an admin to add it in
                <span class="font-medium">Admin → Users → Keys</span>.
              </p>
            {:else}
              <div class="space-y-2">
                {#each ownerKeys as key}
                  <div class="flex items-center gap-3 p-3 bg-gray-50 rounded border">
                    <div class="min-w-0 flex-1">
                      <p class="text-sm font-medium">{key.name}</p>
                      <code class="block text-xs text-gray-500 font-mono truncate">{key.public_key}</code>
                    </div>
                    {#if key.present}
                      <span class="text-xs text-green-700 bg-green-100 rounded px-2 py-1 shrink-0">Installed</span>
                    {:else}
                      <button
                        onclick={() => installAuthorizedKey(key)}
                        disabled={instance.status !== 'running' || installingKeyId === key.id}
                        class="px-3 py-1.5 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm whitespace-nowrap shrink-0"
                      >
                        {installingKeyId === key.id ? 'Adding…' : `Add ${key.name}'s key`}
                      </button>
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}

            <div class="border-t pt-4">
              {#if !showAdminKeys}
                <button
                  onclick={() => showAdminKeys = true}
                  class="px-3 py-1.5 border rounded hover:bg-gray-50 text-sm"
                >
                  Add another user's key
                </button>
                <p class="text-xs text-gray-400 mt-2">
                  Grant a server administrator SSH access to this instance — useful when asking for help.
                </p>
              {:else}
                <div class="flex items-center justify-between mb-2">
                  <h4 class="text-sm font-medium">Server administrators</h4>
                  <button onclick={() => showAdminKeys = false} class="text-xs text-gray-400 hover:text-gray-600">Hide</button>
                </div>
                {#if adminKeys.length === 0}
                  <p class="text-sm text-gray-500">No administrator has registered a public key.</p>
                {:else}
                  <div class="space-y-2">
                    {#each adminKeys as key}
                      <div class="flex items-center gap-3 p-3 bg-gray-50 rounded border">
                        <div class="min-w-0 flex-1">
                          <p class="text-sm font-medium">
                            {key.owner_name || key.owner_email}
                            <span class="text-xs font-normal text-gray-500">— {key.name}</span>
                          </p>
                          <code class="block text-xs text-gray-500 font-mono truncate">{key.public_key}</code>
                        </div>
                        {#if key.present}
                          <span class="text-xs text-green-700 bg-green-100 rounded px-2 py-1 shrink-0">Installed</span>
                        {:else}
                          <button
                            onclick={() => installAuthorizedKey(key)}
                            disabled={instance.status !== 'running' || installingKeyId === key.id}
                            class="px-3 py-1.5 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm whitespace-nowrap shrink-0"
                          >
                            {installingKeyId === key.id ? 'Adding…' : 'Add key'}
                          </button>
                        {/if}
                      </div>
                    {/each}
                  </div>
                {/if}
              {/if}
            </div>

            {#if instance.status !== 'running'}
              <p class="text-xs text-gray-400">Start the instance to install a key into authorized_keys.</p>
            {/if}
          {/if}
        </div>
      {/if}

      <!-- ── SSHX tab ── -->
      {#if detailTab === 'sshx'}
        <div class="p-6 space-y-6">
          <div>
            <h3 class="text-base font-semibold mb-4">SSHX Collaborative Terminal</h3>
            {#if sshxUrl}
              <div class="flex items-center gap-2">
                <div class="flex-1 p-3 bg-gray-50 rounded font-mono text-sm break-all">{sshxUrl}</div>
                <button onclick={() => navigator.clipboard.writeText(sshxUrl).then(() => addNotification('success', 'Copied'))}
                  class="px-3 py-2 border rounded hover:bg-gray-50 text-sm whitespace-nowrap">Copy</button>
                <a href={sshxUrl} target="_blank" rel="noopener noreferrer"
                  class="px-3 py-2 bg-primary text-white rounded hover:bg-primary-dark text-sm whitespace-nowrap">
                  Open ↗
                </a>
              </div>
            {:else if instance?.status !== 'running'}
              <!-- Nothing is being probed here: both sshx calls bail out on a stopped
                   instance. The spinner below would claim otherwise, and would spin for
                   as long as the page stayed open. -->
              <p class="text-sm text-gray-500">
                The instance is stopped. SSHX runs inside it, so there is no session to
                join — start the instance to get a collaborative terminal link.
              </p>
            {:else}
              <div class="flex items-center gap-2 text-sm text-gray-500">
                <div class="animate-spin rounded-full h-4 w-4 border-b-2 border-primary shrink-0"></div>
                <span>Waiting for SSHX service to start…</span>
              </div>
            {/if}
          </div>

          <!-- Build. sshx is maintained in-house: publishing a new one means replacing the
               binary the sshx mixin ships, and every instance can then pull it from here. -->
          <div class="pt-4 border-t">
            <h4 class="text-sm font-semibold mb-3">Build</h4>
            {#if sshxStatus}
              <div class="flex items-center gap-3 flex-wrap">
                <span class="inline-flex items-center gap-2 text-sm text-gray-600">
                  <span class="inline-block w-2 h-2 rounded-full shrink-0 {sshxStatus.service_active ? 'bg-green-500' : 'bg-red-500'}"></span>
                  {sshxStatus.installed ? (sshxStatus.version || 'installed') : 'not installed'}
                </span>
                {#if sshxStatus.installed_hash}
                  <span class="font-mono text-xs text-gray-500" title="sha256 of the binary in this instance">
                    {sshxStatus.installed_hash.slice(0, 12)}
                  </span>
                {/if}
                {#if sshxStatus.run_as}
                  <span class="text-xs {sshxStatus.run_as !== sshxStatus.expected_run_as ? 'text-amber-700' : 'text-gray-500'}"
                    title="Account the sshx service runs as — the shell anyone with the link gets">
                    runs as <span class="font-mono">{sshxStatus.run_as}</span>
                  </span>
                {/if}
                {#if sshxStatus.update_available}
                  <span class="px-2 py-0.5 rounded text-xs bg-amber-50 text-amber-700 border border-amber-200">
                    {!sshxStatus.installed ? 'missing'
                      : sshxStatus.installed_hash === sshxStatus.available_hash ? `should run as ${sshxStatus.expected_run_as}`
                      : 'update available'}
                  </span>
                {:else}
                  <span class="text-xs text-gray-500">up to date</span>
                {/if}
                <button
                  onclick={updateSshx}
                  disabled={sshxUpdating || !sshxStatus.update_available}
                  class="px-3 py-2 bg-primary text-white rounded hover:bg-primary-dark text-sm whitespace-nowrap disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {sshxUpdating ? 'Deploying…' : sshxStatus.installed ? 'Update SSHX' : 'Install SSHX'}
                </button>
              </div>
              {#if sshxStatus.available_hash}
                <p class="text-xs text-gray-500 mt-2">
                  Server ships <span class="font-mono">{sshxStatus.available_hash.slice(0, 12)}</span>
                  — compared by hash, since an in-house rebuild need not change the version string.
                </p>
              {/if}
            {:else}
              <p class="text-sm text-gray-500">Build information unavailable — the instance must be running.</p>
            {/if}
          </div>
        </div>
      {/if}

      <!-- ── Tailscale tab ── -->
      {#if detailTab === 'tailscale'}
        <div class="p-6 space-y-6">
          <div>
            <h3 class="text-base font-semibold mb-3">Machine</h3>
            {#if tsStatus?.connected && tsStatus.dns_name}
              <div class="flex items-center gap-2 mb-3">
                <span class="inline-block w-2 h-2 rounded-full bg-green-500 shrink-0"></span>
                <span class="text-sm text-gray-600">Connected</span>
              </div>
              <div class="space-y-2">
                {#if tsStatus.machine_name}
                  <div>
                    <p class="text-xs text-gray-500 mb-1">Machine name</p>
                    <div class="flex items-center gap-2">
                      <div class="flex-1 p-2 bg-gray-50 rounded font-mono text-sm">{tsStatus.machine_name}</div>
                      <button onclick={() => navigator.clipboard.writeText(tsStatus?.machine_name ?? '').then(() => addNotification('success', 'Copied'))}
                        class="px-3 py-2 border rounded hover:bg-gray-50 text-sm whitespace-nowrap">Copy</button>
                    </div>
                  </div>
                {/if}
                <div>
                  <p class="text-xs text-gray-500 mb-1">Magic DNS name</p>
                  <div class="flex items-center gap-2">
                    <div class="flex-1 p-2 bg-gray-50 rounded font-mono text-sm break-all">{tsStatus.dns_name}</div>
                    <button onclick={() => navigator.clipboard.writeText(tsStatus?.dns_name ?? '').then(() => addNotification('success', 'Copied'))}
                      class="px-3 py-2 border rounded hover:bg-gray-50 text-sm whitespace-nowrap">Copy</button>
                  </div>
                </div>
                {#if hasVSCode}
                  <div class="pt-2 border-t">
                    <p class="text-xs text-gray-500 mb-2">Services</p>
                    <a href={`http://${tsStatus.dns_name}:3463`} target="_blank" rel="noopener noreferrer"
                      class="inline-flex items-center gap-2 px-3 py-2 bg-blue-50 text-blue-700 border border-blue-200 rounded hover:bg-blue-100 text-sm font-mono">
                      {tsStatus.dns_name}:3463 ↗
                    </a>
                  </div>
                {/if}
              </div>
            {:else}
              <div class="flex items-center gap-2">
                <span class="inline-block w-2 h-2 rounded-full bg-gray-400 shrink-0"></span>
                <span class="text-sm text-gray-500">Not connected</span>
              </div>
            {/if}

            {#if tsNeedsInstall}
              <div class="mt-4 p-3 bg-amber-50 border border-amber-200 rounded">
                <p class="text-sm text-amber-800">{tsProblem}</p>
                <p class="text-xs text-amber-700 mt-1">
                  Installs the tailscale mixin and logs the machine in with the current auth key.
                </p>
                {#if tsStatus?.login_output}
                  <p class="text-xs font-semibold text-amber-800 mt-3">What Tailscale said</p>
                  <pre class="mt-1 p-2 bg-white/70 border border-amber-200 rounded text-xs text-gray-800 whitespace-pre-wrap break-words max-h-48 overflow-y-auto">{tsStatus.login_output}</pre>
                  <p class="text-xs text-amber-700 mt-2">
                    A key rejected here is usually single-use and already spent by another machine —
                    a duplicated instance needs a <b>reusable</b> auth key. An admin sets it in
                    <a href="/admin" class="underline">Admin → Settings</a>.
                  </p>
                {/if}
                <button onclick={installTailscale} disabled={tsInstalling}
                  class="mt-3 px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm">
                  {tsInstalling ? 'Installing…' : (tsStatus?.installed ? 'Reinstall Tailscale' : 'Install Tailscale')}
                </button>
              </div>
            {/if}
          </div>

          <div class="border-t pt-6">
            <h3 class="text-base font-semibold mb-3">Tailscale Serve</h3>
            {#if tsServeStatus?.status === 'active' && tsServeStatus.url}
              <div class="flex items-center gap-2 mb-3">
                <span class="inline-block w-2 h-2 rounded-full bg-green-500 shrink-0"></span>
                <span class="text-sm text-gray-600">Serving port {tsServeStatus.port}</span>
              </div>
              <div class="flex items-center gap-2">
                <div class="flex-1 p-3 bg-gray-50 rounded font-mono text-sm break-all">{tsServeStatus.url}</div>
                <a href={tsServeStatus.url} target="_blank" rel="noopener noreferrer"
                  class="px-3 py-2 bg-primary text-white rounded hover:bg-primary-dark text-sm whitespace-nowrap">
                  Open ↗
                </a>
                <button onclick={() => navigator.clipboard.writeText(tsServeStatus?.url ?? '').then(() => addNotification('success', 'Copied'))}
                  class="px-3 py-2 border rounded hover:bg-gray-50 text-sm whitespace-nowrap">Copy</button>
              </div>
              <button onclick={stopTsServe} disabled={tsServeLoading}
                class="mt-3 px-4 py-2 bg-red-600 text-white rounded hover:bg-red-700 disabled:opacity-50 text-sm">
                {tsServeLoading ? 'Stopping…' : 'Stop Serving'}
              </button>
            {:else}
              <div class="flex items-center gap-2">
                <input type="number" bind:value={tsServePort} min="1" max="65535" placeholder="Port"
                  class="w-24 px-3 py-2 border rounded text-sm font-mono" />
                <button onclick={startTsServe} disabled={tsServeLoading}
                  class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm">
                  {tsServeLoading ? 'Starting…' : 'Expose Port'}
                </button>
              </div>
              <p class="text-xs text-gray-400 mt-2">Make a local port accessible via your Tailscale network.</p>
            {/if}
          </div>
        </div>
      {/if}

      <!-- ── Links tab ── -->
      {#if detailTab === 'links'}
        <LinksTab instanceId={id} instanceStatus={instance?.status ?? 'stopped'} />
      {/if}

      <!-- ── OpenVSCode tab ── -->
      {#if detailTab === 'openvscode'}
        {@const vsHost = (tsStatus?.connected && tsStatus.dns_name) ? tsStatus.dns_name : instance.ip_address}
        <div class="p-6 space-y-4">
          <h3 class="text-base font-semibold">OpenVSCode Server</h3>

          {#if vsHost}
            <!-- Direct link — always shown -->
            <div>
              <p class="text-xs text-gray-500 mb-2">Open in browser</p>
              <div class="flex items-center gap-2">
                <div class="flex-1 p-2 bg-gray-50 rounded font-mono text-sm break-all">http://{vsHost}:3463/?folder={homeDir}</div>
                <a href={`http://${vsHost}:3463/?folder=${encodeURIComponent(homeDir)}`} target="_blank" rel="noopener noreferrer"
                  class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm whitespace-nowrap">
                  Open ↗
                </a>
              </div>
            </div>

            <!-- VSCode Desktop deep link — only when we have a DNS name for SSH -->
            {#if tsStatus?.connected && tsStatus.dns_name}
              <div>
                <p class="text-xs text-gray-500 mb-2">Open in VSCode Desktop</p>
                <a href={`vscode://vscode-remote/ssh-remote+${termUser}@${tsStatus.dns_name}${homeDir}`}
                  class="inline-flex items-center gap-2 px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark text-sm">
                  Open in VSCode Desktop
                </a>
                <p class="text-xs text-gray-400 mt-1">Connects via SSH to <span class="font-mono">{termUser}@{tsStatus.dns_name}</span> and opens <span class="font-mono">{homeDir}</span></p>
                <p class="text-xs text-gray-400 mt-1">Opens in existing window? Set <span class="font-mono">"window.openFoldersInNewWindow": "on"</span> in VS Code settings.</p>
              </div>
            {/if}
          {:else}
            <p class="text-sm text-gray-500">Instance not running or no address available.</p>
          {/if}
        </div>
      {/if}

    </div>
  </div>
{/if}
