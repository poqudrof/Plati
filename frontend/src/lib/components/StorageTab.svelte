<script lang="ts">
  import { browser } from '$app/environment';
  import { Filemanager, Willow } from '@svar-ui/svelte-filemanager';
  import { instances } from '$lib/api';
  import type { VolumeDetail, VolumeSnapshot } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  interface Props {
    instanceId: number;
    instanceStatus: string;
    storageVolumes: VolumeDetail[];
  }

  let { instanceId, instanceStatus, storageVolumes }: Props = $props();

  // ── File Manager ──
  let fmData = $state<any[]>([]);
  let fmReady = $state(false);

  // Build initial data: volumes as root folders with lazy loading
  $effect(() => {
    if (!browser || storageVolumes.length === 0) return;
    fmData = storageVolumes.map(v => ({
      id: v.mount_path,
      type: 'folder' as const,
      lazy: true,
      date: new Date(v.created_at),
      size: v.size_gb * 1024 * 1024 * 1024,
    }));
    fmReady = true;
  });

  function initFileManager(api: any) {
    // Load directory contents on demand
    api.on('request-data', async (ev: { id: string }) => {
      if (instanceStatus !== 'running') {
        addNotification('error', 'Instance must be running to browse files');
        return;
      }
      try {
        const entries = await instances.browseDirectory(instanceId, ev.id);
        const parsed = entries.map(e => ({
          id: e.id,
          type: e.type as 'file' | 'folder',
          size: e.size,
          date: new Date(e.date * 1000),
          lazy: e.type === 'folder',
        }));
        api.exec('provide-data', { id: ev.id, data: parsed });
      } catch (e: any) {
        addNotification('error', `Failed to load: ${e.message}`);
      }
    });

    // Download file
    api.on('download-file', (ev: { id: string }) => {
      const url = instances.downloadFileUrl(instanceId, ev.id);
      window.open(url, '_self');
      return false;
    });

    // Open file = download
    api.on('open-file', (ev: { id: string }) => {
      const url = instances.downloadFileUrl(instanceId, ev.id);
      window.open(url, '_self');
    });
  }

  // ── Snapshots ──
  type VolSnap = { vol: VolumeDetail; snapshots: VolumeSnapshot[]; loading: boolean; expanded: boolean };
  let volSnaps = $state<VolSnap[]>([]);
  let newSnapName = $state('');
  let snapActionLoading = $state(false);

  $effect(() => {
    if (!browser || storageVolumes.length === 0) return;
    volSnaps = storageVolumes.map(v => ({
      vol: v,
      snapshots: [],
      loading: false,
      expanded: false,
    }));
  });

  async function loadSnapshots(idx: number) {
    const vs = volSnaps[idx];
    vs.loading = true;
    try {
      vs.snapshots = await instances.listSnapshots(instanceId, vs.vol.volume_id);
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      vs.loading = false;
    }
  }

  function toggleVolume(idx: number) {
    volSnaps[idx].expanded = !volSnaps[idx].expanded;
    if (volSnaps[idx].expanded && volSnaps[idx].snapshots.length === 0) {
      loadSnapshots(idx);
    }
  }

  function suggestName(): string {
    const d = new Date();
    const pad = (n: number) => n.toString().padStart(2, '0');
    return `snap-${d.getFullYear()}${pad(d.getMonth()+1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}`;
  }

  async function createSnapshot(idx: number) {
    const name = newSnapName || suggestName();
    snapActionLoading = true;
    try {
      await instances.createSnapshot(instanceId, volSnaps[idx].vol.volume_id, name);
      addNotification('success', `Snapshot "${name}" created`);
      newSnapName = '';
      await loadSnapshots(idx);
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      snapActionLoading = false;
    }
  }

  async function deleteSnapshot(idx: number, snapName: string) {
    if (!confirm(`Delete snapshot "${snapName}"?`)) return;
    snapActionLoading = true;
    try {
      await instances.deleteSnapshot(instanceId, volSnaps[idx].vol.volume_id, snapName);
      addNotification('success', `Snapshot "${snapName}" deleted`);
      await loadSnapshots(idx);
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      snapActionLoading = false;
    }
  }

  async function restoreSnapshot(idx: number, snapName: string) {
    if (!confirm(`Restore snapshot "${snapName}"? This will overwrite current volume data. Instance must be stopped.`)) return;
    snapActionLoading = true;
    try {
      await instances.restoreSnapshot(instanceId, volSnaps[idx].vol.volume_id, snapName);
      addNotification('success', `Snapshot "${snapName}" restored`);
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      snapActionLoading = false;
    }
  }

  function downloadDir(path: string) {
    const url = instances.downloadDirUrl(instanceId, path);
    window.open(url, '_self');
  }

  function formatSize(bytes: number): string {
    if (!bytes) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
  }
</script>

<div class="p-6 space-y-8">

  <!-- File Browser -->
  {#if instanceStatus === 'running' && fmReady}
    <div>
      <div class="flex items-center justify-between mb-3">
        <h3 class="text-base font-semibold text-gray-700">File Browser</h3>
        {#each storageVolumes as v}
          <button
            onclick={() => downloadDir(v.mount_path)}
            class="px-3 py-1.5 text-xs font-medium bg-gray-100 text-gray-700 rounded hover:bg-gray-200"
          >Download {v.mount_path} as .tar</button>
        {/each}
      </div>
      <div class="border rounded-lg overflow-hidden" style="height: 480px;">
        <Willow>
          <Filemanager
            data={fmData}
            mode="table"
            readonly={true}
            init={initFileManager}
          />
        </Willow>
      </div>
    </div>
  {:else if instanceStatus !== 'running'}
    <div class="text-sm text-gray-500 bg-gray-50 rounded-lg p-4">
      Instance must be running to browse files.
    </div>
  {:else if storageVolumes.length === 0}
    <div class="text-sm text-gray-500 bg-gray-50 rounded-lg p-4">
      No persistent volumes attached (ephemeral mode).
    </div>
  {/if}

  <!-- Volume Snapshots -->
  {#if storageVolumes.length > 0}
    <div>
      <h3 class="text-base font-semibold text-gray-700 mb-3">Volume Snapshots</h3>
      <div class="space-y-3">
        {#each volSnaps as vs, idx}
          <div class="border rounded-lg">
            <button
              onclick={() => toggleVolume(idx)}
              class="w-full flex items-center justify-between px-4 py-3 hover:bg-gray-50 transition-colors"
            >
              <div class="flex items-center gap-3">
                <span class="text-sm font-mono font-medium">{vs.vol.mount_path}</span>
                <span class="text-xs text-gray-400">{vs.vol.volume_name}</span>
                <span class="text-xs text-gray-400">{vs.vol.size_gb} GB ({vs.vol.pool})</span>
              </div>
              <span class="text-gray-400 text-xs">{vs.expanded ? '&#9660;' : '&#9654;'}</span>
            </button>

            {#if vs.expanded}
              <div class="border-t px-4 py-4 space-y-4">
                <!-- Create snapshot -->
                <div class="flex gap-2 items-center">
                  <input
                    type="text"
                    bind:value={newSnapName}
                    placeholder={suggestName()}
                    class="flex-1 px-3 py-1.5 border rounded text-sm font-mono"
                  />
                  <button
                    onclick={() => createSnapshot(idx)}
                    disabled={snapActionLoading}
                    class="px-4 py-1.5 bg-primary text-white text-sm rounded hover:bg-primary-dark disabled:opacity-50"
                  >Create Snapshot</button>
                </div>

                <!-- Snapshot list -->
                {#if vs.loading}
                  <div class="flex justify-center py-4">
                    <div class="animate-spin rounded-full h-5 w-5 border-b-2 border-primary"></div>
                  </div>
                {:else if vs.snapshots.length === 0}
                  <p class="text-sm text-gray-400">No snapshots yet.</p>
                {:else}
                  <div class="space-y-2">
                    {#each vs.snapshots as snap}
                      <div class="flex items-center justify-between px-3 py-2 bg-gray-50 rounded text-sm">
                        <div>
                          <span class="font-mono font-medium">{snap.name}</span>
                          <span class="text-xs text-gray-400 ml-2">{new Date(snap.created_at).toLocaleString()}</span>
                        </div>
                        <div class="flex gap-2">
                          <button
                            onclick={() => restoreSnapshot(idx, snap.name)}
                            disabled={snapActionLoading || instanceStatus === 'running'}
                            title={instanceStatus === 'running' ? 'Stop instance to restore' : 'Restore this snapshot'}
                            class="px-3 py-1 text-xs rounded border
                              {instanceStatus === 'running'
                                ? 'text-gray-400 border-gray-200 cursor-not-allowed'
                                : 'text-blue-600 border-blue-200 hover:bg-blue-50'}"
                          >Restore</button>
                          <button
                            onclick={() => deleteSnapshot(idx, snap.name)}
                            disabled={snapActionLoading}
                            class="px-3 py-1 text-xs rounded border text-red-600 border-red-200 hover:bg-red-50 disabled:opacity-50"
                          >Delete</button>
                        </div>
                      </div>
                    {/each}
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  {/if}

</div>
