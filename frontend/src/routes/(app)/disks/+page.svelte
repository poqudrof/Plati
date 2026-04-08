<script lang="ts">
  import { browser } from '$app/environment';
  import { disks } from '$lib/api';
  import type { DiskInfo } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let diskList: DiskInfo[] = $state([]);
  let loading = $state(true);

  async function loadDisks() {
    loading = true;
    try {
      diskList = (await disks.list()) ?? [];
    } catch (e: any) {
      addNotification('error', 'Failed to load disks: ' + e.message);
    } finally {
      loading = false;
    }
  }

  let totalSizeGB = $derived(diskList.reduce((sum, d) => sum + d.size_gb, 0));

  function statusColor(status: string): string {
    switch (status) {
      case 'running': return 'text-green-700 bg-green-50';
      case 'stopped': return 'text-gray-600 bg-gray-100';
      case 'creating': return 'text-blue-600 bg-blue-50';
      case 'error': return 'text-red-600 bg-red-50';
      default: return 'text-gray-500 bg-gray-50';
    }
  }

  if (browser) loadDisks();
</script>

<div>
  <div class="mb-6">
    <h1 class="text-2xl font-bold">My Disks</h1>
    <p class="text-sm text-gray-500 mt-1">
      {diskList.length} volume{diskList.length !== 1 ? 's' : ''} &middot; {totalSizeGB} GB total
    </p>
  </div>

  {#if loading}
    <p class="text-gray-500">Loading disks...</p>
  {:else if diskList.length === 0}
    <p class="text-gray-500">No volumes found. Volumes are created when you launch an instance with persistent storage.</p>
  {:else}
    <div class="overflow-x-auto">
      <table class="min-w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Volume</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Instance</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Mount Path</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Size</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Pool</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Server</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Instance Status</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Created</th>
          </tr>
        </thead>
        <tbody class="divide-y">
          {#each diskList as disk}
            <tr class="hover:bg-gray-50">
              <td class="px-3 py-2 font-mono text-xs">{disk.volume_name}</td>
              <td class="px-3 py-2">
                <a href="/instances/{disk.instance_id}" class="text-primary hover:underline">{disk.instance_name}</a>
              </td>
              <td class="px-3 py-2 font-mono text-xs">{disk.mount_path}</td>
              <td class="px-3 py-2">{disk.size_gb} GB</td>
              <td class="px-3 py-2">{disk.pool}</td>
              <td class="px-3 py-2">{disk.server_name}</td>
              <td class="px-3 py-2">
                <span class="px-2 py-0.5 rounded text-xs {statusColor(disk.status)}">{disk.status}</span>
              </td>
              <td class="px-3 py-2 text-xs text-gray-500">{new Date(disk.created_at).toLocaleDateString()}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
