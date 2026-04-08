<script lang="ts">
  import { instances } from '$lib/api';
  import type { Instance } from '$lib/api/types';
  import InstanceCard from '$lib/components/InstanceCard.svelte';
  import { addNotification } from '$lib/stores/notifications';
  import { browser } from '$app/environment';

  let instanceList: Instance[] = $state([]);
  let loading = $state(true);

  async function load() {
    try {
      instanceList = await instances.list();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      loading = false;
    }
  }

  if (browser) {
    load();
  }
</script>

<div>
  <div class="flex justify-between items-center mb-6">
    <h1 class="text-2xl font-bold">My Workspaces</h1>
    <a href="/instances/new" class="btn-primary">
      New Instance
    </a>
  </div>

  {#if loading}
    <div class="flex justify-center py-12">
      <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
    </div>
  {:else if instanceList.length === 0}
    <div class="text-center py-12 bg-white rounded-lg border">
      <p class="text-gray-500 mb-4">No workspaces yet</p>
      <a href="/instances/new" class="text-primary hover:text-primary-dark">Create your first instance</a>
    </div>
  {:else}
    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      {#each instanceList as inst (inst.id)}
        <InstanceCard instance={inst} onRefresh={load} />
      {/each}
    </div>
  {/if}
</div>
