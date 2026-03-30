<script lang="ts">
  import { browser } from '$app/environment';
  import { page } from '$app/stores';
  import { instances, admin } from '$lib/api';
  import type { Instance, Server, InstanceSecret, IncusDetail } from '$lib/api/types';
  import { goto } from '$app/navigation';
  import { addNotification } from '$lib/stores/notifications';
  import Terminal from '$lib/components/Terminal.svelte';
  import { currentUser } from '$lib/stores/auth';

  let instance: Instance | null = $state(null);
  let server = $state<Server | null>(null);
  let incusDetail: IncusDetail | null = $state(null);
  let loading = $state(true);
  let actionLoading = $state(false);

  let instanceSecrets: InstanceSecret[] = $state([]);
  let newSecretName = $state('');
  let newSecretValue = $state('');

  let id = $derived(Number($page.params.id));
  let isAdmin = $derived($currentUser?.role === 'admin');
  let incusUIUrl = $derived(server ? `${server.endpoint}/ui` : null);

  async function load() {
    loading = true;
    try {
      instance = await instances.get(id);
      instanceSecrets = await instances.listSecrets(id);
      if ($currentUser?.role === 'admin' && instance) {
        const servers = await admin.servers.list();
        server = servers.find(s => s.id === instance!.server_id) ?? null;
        incusDetail = await admin.instances.incusInfo(id).catch(() => null);
      }
    } catch (e: any) {
      addNotification('error', 'Instance not found');
      goto('/dashboard');
    } finally {
      loading = false;
    }
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
</script>

{#if loading}
  <div class="flex justify-center py-12">
    <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
  </div>
{:else if instance}
  <div>
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold">{instance.name}</h1>
      <div class="flex items-center gap-3">
        <span class="px-3 py-1 rounded-full text-sm font-medium
          {instance.status === 'running' ? 'bg-green-100 text-green-800' : ''}
          {instance.status === 'stopped' ? 'bg-gray-100 text-gray-800' : ''}
          {instance.status === 'creating' ? 'bg-yellow-100 text-yellow-800' : ''}
          {instance.status === 'error' ? 'bg-red-100 text-red-800' : ''}
        ">
          {instance.status}
        </span>
      </div>
    </div>

    <div class="bg-white rounded-lg border p-6 space-y-6">
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
          <p class="text-sm text-gray-500 mb-2">Terminal</p>
          <Terminal instanceId={id} />
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

    {#if isAdmin && incusDetail}
      <div class="bg-white rounded-lg border p-6 mt-6">
        <div class="flex justify-between items-center mb-4">
          <h3 class="text-lg font-semibold">Incus Detail</h3>
          {#if incusUIUrl}
            <a href={incusUIUrl} target="_blank" rel="noopener noreferrer"
              class="px-3 py-1 rounded text-sm font-medium bg-indigo-50 text-indigo-700 border border-indigo-200 hover:bg-indigo-100">
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

          <div class="border-t pt-4">
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
      </div>
    {/if}

    <div class="bg-white rounded-lg border p-6 mt-6">
      <h3 class="text-lg font-semibold mb-1">Instance Secrets</h3>
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
        <button onclick={addSecret} class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700 text-sm">
          Add Secret
        </button>
      </div>
    </div>
  </div>
{/if}
