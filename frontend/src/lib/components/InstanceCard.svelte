<script lang="ts">
  import type { Instance } from '$lib/api/types';
  import { instances } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';

  let { instance, onRefresh = () => {} }: { instance: Instance; onRefresh?: () => void } = $props();

  let loading = $state(false);

  const statusColors: Record<string, string> = {
    running: 'bg-green-100 text-green-800',
    stopped: 'bg-gray-100 text-gray-800',
    creating: 'bg-yellow-100 text-yellow-800',
    error: 'bg-red-100 text-red-800'
  };

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

<div class="bg-white rounded-lg shadow-sm border p-6">
  <div class="flex justify-between items-start mb-4">
    <div>
      <h3 class="text-lg font-semibold">
        <a href="/instances/{instance.id}" class="hover:text-indigo-600">{instance.name}</a>
      </h3>
      <p class="text-sm text-gray-500">
        {instance.incus_name}
      </p>
    </div>
    <span class="px-2 py-1 rounded-full text-xs font-medium {statusColors[instance.status] || 'bg-gray-100'}">
      {instance.status}
    </span>
  </div>

  {#if instance.ip_address}
    <div class="mb-4 p-3 bg-gray-50 rounded font-mono text-sm">
      ssh user@{instance.ip_address}
    </div>
  {/if}

  <div class="flex gap-2">
    {#if instance.status === 'stopped'}
      <button
        onclick={() => action(() => instances.start(instance.id), 'Instance started')}
        disabled={loading}
        class="px-3 py-1.5 bg-green-600 text-white rounded text-sm hover:bg-green-700 disabled:opacity-50"
      >Start</button>
    {/if}
    {#if instance.status === 'running'}
      <button
        onclick={() => action(() => instances.stop(instance.id), 'Instance stopped')}
        disabled={loading}
        class="px-3 py-1.5 bg-yellow-600 text-white rounded text-sm hover:bg-yellow-700 disabled:opacity-50"
      >Stop</button>
    {/if}
    <button
      onclick={() => action(() => instances.rebuild(instance.id), 'Instance rebuilt')}
      disabled={loading}
      class="px-3 py-1.5 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50"
    >Rebuild</button>
    <button
      onclick={() => { if (confirm('Delete this instance?')) action(() => instances.delete(instance.id), 'Instance deleted') }}
      disabled={loading}
      class="px-3 py-1.5 bg-red-600 text-white rounded text-sm hover:bg-red-700 disabled:opacity-50"
    >Delete</button>
  </div>
</div>
