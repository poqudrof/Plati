<script lang="ts">
  import { browser } from '$app/environment';
  import { page } from '$app/stores';
  import { instances, admin, templates } from '$lib/api';
  import type { Instance, Server, InstanceSecret, IncusDetail, IncusConfigUpdate, Template, TailscaleServeResult, TailscaleStatusResult, InstanceStorageInfo } from '$lib/api/types';
  import { goto } from '$app/navigation';
  import { addNotification } from '$lib/stores/notifications';
  import Terminal from '$lib/components/Terminal.svelte';
  import StorageTab from '$lib/components/StorageTab.svelte';
  import { currentUser } from '$lib/stores/auth';

  let instance: Instance | null = $state(null);
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

  // Tailscale Serve
  let tsServeStatus = $state<TailscaleServeResult | null>(null);
  let tsServePort = $state(8080);
  let tsServeLoading = $state(false);

  // Tailscale machine status (DNS name)
  let tsStatus = $state<TailscaleStatusResult | null>(null);

  // Storage info
  let storageInfo = $state<InstanceStorageInfo | null>(null);

  // Terminal user toggle (root vs unprivileged)
  let terminalUser = $state('root');

  // Tabs below the main card
  let detailTab: 'storage' | 'incus' | 'secrets' | 'sshx' | 'tailscale' | 'openvscode' = $state('storage');

  // Creation log stream (while status === 'creating')
  type LogStep = { kind: 'step'; n: number; total: number; label: string; outputs: string[]; warning: string; expanded: boolean };
  type LogLine = { kind: 'log'; text: string };
  type LogItem = LogStep | LogLine;

  let logItems = $state<LogItem[]>([]);
  let creationLogEl: HTMLDivElement | undefined = $state();

  let id = $derived(Number($page.params.id));
  let isAdmin = $derived($currentUser?.role === 'admin');
  let incusUIUrl = $derived(server ? `${server.endpoint}/ui` : null);
  let includes = $derived((() => { try { return JSON.parse(template?.includes ?? '[]') as string[]; } catch { return [] as string[]; } })());
  let hasSshx = $derived(includes.includes('sshx'));
  let hasTailscale = $derived(includes.includes('tailscale'));
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

  async function loadSshxUrl() {
    if (!browser || !instance || instance.status !== 'running') return;
    if (sshxRefreshTimer) { clearTimeout(sshxRefreshTimer); sshxRefreshTimer = null; }
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

  async function deleteInstance() {
    if (!confirm('Are you sure you want to delete this instance?')) return;
    await action(() => instances.delete(id), 'Instance deleted');
    goto('/dashboard');
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
      <h1 class="text-2xl font-bold">{instance.name}</h1>
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
        <button onclick={() => action(() => instances.rebuild(id), 'Rebuilt')} disabled={actionLoading}
          class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50">Rebuild</button>
        <button onclick={deleteInstance} disabled={actionLoading}
          class="px-4 py-2 bg-red-600 text-white rounded hover:bg-red-700 disabled:opacity-50">Delete</button>
      </div>
    </div>

    <!-- Tabbed detail panel -->
    <div class="bg-white rounded-lg border mt-6 overflow-hidden">

      <!-- Tab bar -->
      <div class="flex border-b">
        <button
          onclick={() => detailTab = 'storage'}
          class="px-5 py-3 text-sm font-medium border-b-2 transition-colors
            {detailTab === 'storage' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
        >Storage</button>
        {#if isAdmin}
          <button
            onclick={() => detailTab = 'incus'}
            class="px-5 py-3 text-sm font-medium border-b-2 transition-colors
              {detailTab === 'incus' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
          >Incus Detail</button>
        {/if}
        <button
          onclick={() => detailTab = 'secrets'}
          class="px-5 py-3 text-sm font-medium border-b-2 transition-colors
            {detailTab === 'secrets' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
        >Secrets</button>
        {#if hasSshx}
          <button
            onclick={() => detailTab = 'sshx'}
            class="px-5 py-3 text-sm font-medium border-b-2 transition-colors
              {detailTab === 'sshx' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
          >SSHX</button>
        {/if}
        {#if hasTailscale}
          <button
            onclick={() => detailTab = 'tailscale'}
            class="px-5 py-3 text-sm font-medium border-b-2 transition-colors
              {detailTab === 'tailscale' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
          >Tailscale</button>
        {/if}
        {#if hasVSCode}
          <button
            onclick={() => detailTab = 'openvscode'}
            class="px-5 py-3 text-sm font-medium border-b-2 transition-colors
              {detailTab === 'openvscode' ? 'border-primary text-primary-dark' : 'border-transparent text-gray-500 hover:text-gray-800'}"
          >OpenVSCode</button>
        {/if}
      </div>

      <!-- ── Storage tab ── -->
      {#if detailTab === 'storage'}
        {#if storageInfo}
          <div class="flex items-center gap-2 px-6 pt-4">
            {#if storageInfo.persistence_mode === 'ephemeral'}
              <span class="px-2 py-0.5 rounded-full text-xs font-medium bg-amber-100 text-amber-800">Ephemeral</span>
              <span class="text-sm text-gray-500">All storage is instance-local. Data is lost on rebuild or delete.</span>
            {:else}
              <span class="px-2 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800">Persistent</span>
              <span class="text-sm text-gray-500">Volumes survive rebuilds. Only delete destroys data.</span>
            {/if}
          </div>
          <StorageTab
            instanceId={id}
            instanceStatus={instance?.status ?? 'stopped'}
            storageVolumes={storageInfo.volumes}
          />
        {:else}
          <div class="p-6">
            <p class="text-sm text-gray-500">Storage information not available.</p>
          </div>
        {/if}
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

      <!-- ── SSHX tab ── -->
      {#if detailTab === 'sshx'}
        <div class="p-6">
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
          {:else}
            <div class="flex items-center gap-2 text-sm text-gray-500">
              <div class="animate-spin rounded-full h-4 w-4 border-b-2 border-primary shrink-0"></div>
              <span>Waiting for SSHX service to start…</span>
            </div>
          {/if}
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

      <!-- ── OpenVSCode tab ── -->
      {#if detailTab === 'openvscode'}
        {@const vsHost = (tsStatus?.connected && tsStatus.dns_name) ? tsStatus.dns_name : instance.ip_address}
        {@const termUser = template?.terminal_user || 'root'}
        {@const workspaceDir = (() => { try { const dirs = JSON.parse(template?.persistence_dirs ?? '[]') as {path: string}[]; return dirs[0]?.path ?? '/workspace'; } catch { return '/workspace'; } })()}
        <div class="p-6 space-y-4">
          <h3 class="text-base font-semibold">OpenVSCode Server</h3>

          {#if vsHost}
            <!-- Direct link — always shown -->
            <div>
              <p class="text-xs text-gray-500 mb-2">Open in browser</p>
              <div class="flex items-center gap-2">
                <div class="flex-1 p-2 bg-gray-50 rounded font-mono text-sm break-all">http://{vsHost}:3463</div>
                <a href={`http://${vsHost}:3463`} target="_blank" rel="noopener noreferrer"
                  class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm whitespace-nowrap">
                  Open ↗
                </a>
              </div>
            </div>

            <!-- VSCode Desktop deep link — only when we have a DNS name for SSH -->
            {#if tsStatus?.connected && tsStatus.dns_name}
              <div>
                <p class="text-xs text-gray-500 mb-2">Open in VSCode Desktop</p>
                <a href={`vscode://vscode-remote/ssh-remote+${termUser}@${tsStatus.dns_name}${workspaceDir}`}
                  class="inline-flex items-center gap-2 px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark text-sm">
                  Open in VSCode Desktop
                </a>
                <p class="text-xs text-gray-400 mt-1">Connects via SSH to <span class="font-mono">{termUser}@{tsStatus.dns_name}</span></p>
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
