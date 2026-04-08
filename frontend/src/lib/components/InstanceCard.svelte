<script lang="ts">
  import { browser } from '$app/environment';
  import type { Instance, InstanceStats, Template } from '$lib/api/types';
  import { instances } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';
  import { goto } from '$app/navigation';

  let { instance, template, onRefresh = () => {} }: {
    instance: Instance;
    template?: Template;
    onRefresh?: () => void;
  } = $props();

  let showRebuildConfirm = $state(false);

  let loading = $state(false);
  let stats = $state<InstanceStats | null>(null);

  const statusColors: Record<string, string> = {
    running:  'bg-green-100 text-green-800',
    stopped:  'bg-gray-100 text-gray-800',
    creating: 'bg-yellow-100 text-yellow-800',
    error:    'bg-red-100 text-red-800',
  };

  // Load stats for running instances
  $effect(() => {
    if (browser && instance.status === 'running') {
      instances.stats(instance.id).then(s => { stats = s; }).catch(() => {});
    }
  });

  async function action(fn: () => Promise<unknown>, msg: string) {
    loading = true;
    try {
      await fn();
      addNotification('success', msg);
      onRefresh();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      loading = false;
    }
  }

  async function duplicate() {
    loading = true;
    try {
      const newInst = await instances.duplicate(instance.id);
      addNotification('success', `Duplicating as "${newInst.name}" — this may take a minute`);
      onRefresh();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      loading = false;
    }
  }

  async function deleteInstance() {
    if (!confirm(`Delete "${instance.name}"?\n\nThis permanently destroys the instance and all workspace data. This cannot be undone.`)) return;
    loading = true;
    try {
      await instances.delete(instance.id);
      addNotification('success', 'Instance deleted');
      onRefresh();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      loading = false;
    }
  }
</script>

<div class="bg-white rounded-lg border p-5 space-y-4 flex flex-col">

  <!-- Header: name + status + View -->
  <div class="flex items-start justify-between gap-3">
    <div class="min-w-0">
      <h3 class="font-semibold text-gray-900 truncate">{instance.name}</h3>
      <p class="text-xs text-gray-400 font-mono mt-0.5 truncate">{instance.incus_name}</p>
    </div>
    <div class="flex items-center gap-2 shrink-0">
      <span class="px-2 py-0.5 rounded-full text-xs font-medium {statusColors[instance.status] ?? 'bg-gray-100'}">
        {instance.status}
      </span>
      <a href="/instances/{instance.id}"
        class="px-3 py-1.5 bg-primary text-white rounded text-sm font-medium hover:bg-primary-dark">
        View →
      </a>
    </div>
  </div>

  <!-- Workspace stats (running instances only) -->
  {#if instance.status === 'running'}
    <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-gray-600 min-h-[1.25rem]">
      {#if stats}
        {#if stats.has_git}
          <span class="flex items-center gap-1.5">
            <span class="w-2 h-2 rounded-full bg-yellow-400 inline-block"></span>
            {stats.git_modified} modified {stats.git_modified === 1 ? 'file' : 'files'}
          </span>
        {/if}
        {#if stats.disk_used && stats.disk_used !== 'unknown'}
          <span class="flex items-center gap-1.5">
            <span class="w-2 h-2 rounded-full bg-blue-400 inline-block"></span>
            {stats.disk_used} workspace
          </span>
        {/if}
        {#if !stats.has_git && (!stats.disk_used || stats.disk_used === 'unknown')}
          <span class="text-gray-400 text-xs">No workspace stats available</span>
        {/if}
      {:else}
        <span class="text-gray-400 text-xs animate-pulse">Loading stats…</span>
      {/if}
    </div>
  {/if}

  <!-- SSH connection string -->
  {#if instance.ip_address}
    <div class="font-mono text-xs bg-gray-50 rounded px-3 py-2 text-gray-700 select-all">
      ssh user@{instance.ip_address}
    </div>
  {/if}

  <!-- Primary actions -->
  <div class="flex gap-2">
    {#if instance.status === 'stopped'}
      <button
        onclick={() => action(() => instances.start(instance.id), 'Instance started')}
        disabled={loading}
        class="px-3 py-1.5 bg-green-600 text-white rounded text-sm hover:bg-green-700 disabled:opacity-50">
        Start
      </button>
    {/if}
    <button
      onclick={duplicate}
      disabled={loading}
      class="px-3 py-1.5 bg-gray-100 text-gray-700 rounded text-sm hover:bg-gray-200 disabled:opacity-50 border">
      Duplicate
    </button>
  </div>

  <!-- Instance management: stop / rebuild / delete with descriptions -->
  <div class="border-t pt-4 space-y-3">
    <p class="text-xs font-semibold text-gray-400 uppercase tracking-wider">Instance management</p>

    {#if instance.status === 'running'}
      <div class="flex items-start gap-3">
        <button
          onclick={() => action(() => instances.stop(instance.id), 'Instance stopped')}
          disabled={loading}
          class="px-3 py-1.5 bg-yellow-500 text-white rounded text-sm hover:bg-yellow-600 disabled:opacity-50 shrink-0">
          Stop
        </button>
        <p class="text-xs text-gray-500 leading-snug pt-1">
          Pauses the instance. All data — including the <code class="bg-gray-100 px-0.5 rounded">/workspace</code> volume — is fully preserved.
        </p>
      </div>
    {/if}

    <div class="flex items-start gap-3">
      <button
        onclick={() => {
          if (template?.persistence_mode === 'normal') {
            showRebuildConfirm = true;
          } else {
            action(() => instances.rebuild(instance.id), 'Instance rebuilt');
          }
        }}
        disabled={loading}
        class="px-3 py-1.5 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50 shrink-0">
        Rebuild
      </button>
      <p class="text-xs text-gray-500 leading-snug pt-1">
        Reinstalls the OS from the original template.
        {#if template?.persistence_mode === 'normal'}
          <strong class="text-gray-700">Persistent volumes are preserved — <code class="bg-gray-100 px-0.5 rounded">rebuild_commands</code> run instead of <code class="bg-gray-100 px-0.5 rounded">first_init_commands</code>.</strong>
        {:else}
          <strong class="text-gray-700"><code class="bg-gray-100 px-0.5 rounded">/workspace</code> is a persistent volume — your data is preserved regardless of the image.</strong>
        {/if}
      </p>
    </div>

    <div class="flex items-start gap-3">
      <button
        onclick={deleteInstance}
        disabled={loading}
        class="px-3 py-1.5 bg-red-600 text-white rounded text-sm hover:bg-red-700 disabled:opacity-50 shrink-0">
        Delete
      </button>
      <p class="text-xs text-red-600 leading-snug pt-1">
        <strong>Permanently destroys</strong> the instance and all workspace data.
        This cannot be undone.
      </p>
    </div>
  </div>

</div>

<!-- Rebuild confirmation modal (for normal persistence) -->
{#if showRebuildConfirm}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
    <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 p-6">
      <h2 class="text-lg font-bold mb-2">Rebuild "{instance.name}"?</h2>
      <div class="space-y-2 text-sm text-gray-700 mb-4">
        <p>The OS will be reinstalled from the original template image.</p>
        <p class="font-medium text-green-700">Persistent volumes (e.g. <code class="bg-gray-100 px-1 rounded">/workspace</code>) are NOT wiped — your files are safe.</p>
        <p><code class="bg-gray-100 px-1 rounded">rebuild_commands</code> will run instead of <code class="bg-gray-100 px-1 rounded">first_init_commands</code>.</p>
      </div>
      <div class="flex justify-end gap-3">
        <button onclick={() => showRebuildConfirm = false} class="px-4 py-2 border rounded hover:bg-gray-50">Cancel</button>
        <button
          onclick={() => { showRebuildConfirm = false; action(() => instances.rebuild(instance.id), 'Instance rebuilt'); }}
          class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700"
        >
          Rebuild
        </button>
      </div>
    </div>
  </div>
{/if}
