<script lang="ts">
  import { browser } from '$app/environment';
  import type { Instance, InstanceStats, User } from '$lib/api/types';
  import { instances } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';
  import { goto } from '$app/navigation';
  import DuplicateInstanceButton from './DuplicateInstanceButton.svelte';

  let { instance, users = [], onRefresh = () => {} }: {
    instance: Instance;
    /** Admin only: accounts a duplicate can be assigned to. */
    users?: User[];
    onRefresh?: () => void;
  } = $props();


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
    <DuplicateInstanceButton
      instanceId={instance.id}
      {users}
      defaultUserId={instance.user_id}
      disabled={loading}
      onDone={onRefresh} />
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
          Pauses the instance. All data — including the persistent volume — is fully preserved.
        </p>
      </div>
    {/if}

    <p class="text-xs text-gray-500 leading-snug">
      Workspaces are long-lived. Deleting one is done from
      <a href="/settings" class="text-primary hover:text-primary-dark underline">Settings</a>.
    </p>
  </div>

</div>


