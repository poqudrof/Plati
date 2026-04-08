<script lang="ts">
  import { browser } from '$app/environment';
  import { admin } from '$lib/api';
  import type { DiskInfo } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let diskList: DiskInfo[] = $state([]);
  let loading = $state(true);
  let groupBy: 'user' | 'server' | 'flat' = $state('user');

  async function loadDisks() {
    loading = true;
    try {
      diskList = (await admin.disks.list()) ?? [];
    } catch (e: any) {
      addNotification('error', 'Failed to load disks: ' + e.message);
    } finally {
      loading = false;
    }
  }

  let totalSizeGB = $derived(diskList.reduce((sum, d) => sum + d.size_gb, 0));

  let groupedByUser = $derived((() => {
    const map = new Map<string, DiskInfo[]>();
    for (const d of diskList) {
      const key = d.user_email;
      if (!map.has(key)) map.set(key, []);
      map.get(key)!.push(d);
    }
    return map;
  })());

  let groupedByServer = $derived((() => {
    const map = new Map<string, DiskInfo[]>();
    for (const d of diskList) {
      const key = d.server_name;
      if (!map.has(key)) map.set(key, []);
      map.get(key)!.push(d);
    }
    return map;
  })());

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
  <div class="flex items-center justify-between mb-6">
    <div>
      <h1 class="text-2xl font-bold">Disks</h1>
      <p class="text-sm text-gray-500 mt-1">
        {diskList.length} volume{diskList.length !== 1 ? 's' : ''} &middot; {totalSizeGB} GB total
      </p>
    </div>
    <div class="flex items-center gap-2">
      <span class="text-sm text-gray-500">Group by:</span>
      <select bind:value={groupBy} class="text-sm border rounded px-2 py-1">
        <option value="user">User</option>
        <option value="server">Server</option>
        <option value="flat">None</option>
      </select>
      <a href="/admin" class="text-sm text-primary hover:text-primary-dark ml-4">Back to Admin</a>
    </div>
  </div>

  {#if loading}
    <p class="text-gray-500">Loading disks...</p>
  {:else if diskList.length === 0}
    <p class="text-gray-500">No volumes found.</p>
  {:else if groupBy === 'flat'}
    <div class="overflow-x-auto">
      <table class="min-w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Volume</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">User</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Instance</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Mount Path</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Size</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Pool</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Server</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Status</th>
            <th class="px-3 py-2 text-left font-medium text-gray-600">Created</th>
          </tr>
        </thead>
        <tbody class="divide-y">
          {#each diskList as disk}
            <tr class="hover:bg-gray-50">
              <td class="px-3 py-2 font-mono text-xs">{disk.volume_name}</td>
              <td class="px-3 py-2">{disk.user_email}</td>
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
  {:else}
    {#each [...(groupBy === 'user' ? groupedByUser : groupedByServer).entries()] as [group, items]}
      {@const groupSize = items.reduce((s, d) => s + d.size_gb, 0)}
      <div class="mb-6">
        <h2 class="text-lg font-semibold mb-2">
          {group}
          <span class="text-sm font-normal text-gray-500 ml-2">
            {items.length} volume{items.length !== 1 ? 's' : ''} &middot; {groupSize} GB
          </span>
        </h2>
        <div class="overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-3 py-2 text-left font-medium text-gray-600">Volume</th>
                {#if groupBy === 'server'}
                  <th class="px-3 py-2 text-left font-medium text-gray-600">User</th>
                {/if}
                <th class="px-3 py-2 text-left font-medium text-gray-600">Instance</th>
                <th class="px-3 py-2 text-left font-medium text-gray-600">Mount Path</th>
                <th class="px-3 py-2 text-left font-medium text-gray-600">Size</th>
                <th class="px-3 py-2 text-left font-medium text-gray-600">Pool</th>
                {#if groupBy === 'user'}
                  <th class="px-3 py-2 text-left font-medium text-gray-600">Server</th>
                {/if}
                <th class="px-3 py-2 text-left font-medium text-gray-600">Status</th>
                <th class="px-3 py-2 text-left font-medium text-gray-600">Created</th>
              </tr>
            </thead>
            <tbody class="divide-y">
              {#each items as disk}
                <tr class="hover:bg-gray-50">
                  <td class="px-3 py-2 font-mono text-xs">{disk.volume_name}</td>
                  {#if groupBy === 'server'}
                    <td class="px-3 py-2">{disk.user_email}</td>
                  {/if}
                  <td class="px-3 py-2">
                    <a href="/instances/{disk.instance_id}" class="text-primary hover:underline">{disk.instance_name}</a>
                  </td>
                  <td class="px-3 py-2 font-mono text-xs">{disk.mount_path}</td>
                  <td class="px-3 py-2">{disk.size_gb} GB</td>
                  <td class="px-3 py-2">{disk.pool}</td>
                  {#if groupBy === 'user'}
                    <td class="px-3 py-2">{disk.server_name}</td>
                  {/if}
                  <td class="px-3 py-2">
                    <span class="px-2 py-0.5 rounded text-xs {statusColor(disk.status)}">{disk.status}</span>
                  </td>
                  <td class="px-3 py-2 text-xs text-gray-500">{new Date(disk.created_at).toLocaleDateString()}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>
    {/each}
  {/if}
</div>
