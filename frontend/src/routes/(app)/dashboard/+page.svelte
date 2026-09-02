<script lang="ts">
  import { instances, admin } from '$lib/api';
  import type { Instance, AdminInstance, User } from '$lib/api/types';
  import InstanceCard from '$lib/components/InstanceCard.svelte';
  import DuplicateInstanceButton from '$lib/components/DuplicateInstanceButton.svelte';
  import { addNotification } from '$lib/stores/notifications';
  import { currentUser } from '$lib/stores/auth';
  import { browser } from '$app/environment';

  let instanceList: Instance[] = $state([]);
  let allInstances: AdminInstance[] = $state([]);
  let users: User[] = $state([]);
  let loading = $state(true);
  let scope: 'mine' | 'all' = $state('mine');

  let isAdmin = $derived($currentUser?.role === 'admin');

  const statusColors: Record<string, string> = {
    running:  'bg-green-100 text-green-800',
    stopped:  'bg-gray-100 text-gray-800',
    creating: 'bg-yellow-100 text-yellow-800',
    error:    'bg-red-100 text-red-800',
  };

  async function load() {
    loading = true;
    try {
      instanceList = await instances.list();
      // Admins can duplicate into any account, so the user list is needed for
      // the picker on their own cards too — not just in the "All users" view.
      if (isAdmin) {
        [allInstances, users] = await Promise.all([admin.instances.list(), admin.users.list()]);
      }
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
    <h1 class="page-title">{scope === 'all' ? 'All Workspaces' : 'My Workspaces'}</h1>
    <a href="/instances/new" class="btn-primary">
      New Instance
    </a>
  </div>

  {#if isAdmin}
    <div class="flex gap-6 border-b mb-6">
      <button onclick={() => (scope = 'mine')} class={scope === 'mine' ? 'tab-active' : 'tab-inactive'}>
        Mine ({instanceList.length})
      </button>
      <button onclick={() => (scope = 'all')} class={scope === 'all' ? 'tab-active' : 'tab-inactive'}>
        All users ({allInstances.length})
      </button>
    </div>
  {/if}

  {#if loading}
    <div class="flex justify-center py-12">
      <div class="spinner"></div>
    </div>
  {:else if scope === 'all'}
    {#if allInstances.length === 0}
      <div class="text-center py-12 card-static">
        <p class="text-gray-500">No workspaces on the platform yet</p>
      </div>
    {:else}
      <div class="card-static overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="text-left text-xs uppercase tracking-wider text-gray-400 border-b">
            <tr>
              <th class="px-4 py-3 font-semibold">Workspace</th>
              <th class="px-4 py-3 font-semibold">Owner</th>
              <th class="px-4 py-3 font-semibold">Template</th>
              <th class="px-4 py-3 font-semibold">Status</th>
              <th class="px-4 py-3 font-semibold text-right">Duplicate</th>
            </tr>
          </thead>
          <tbody>
            {#each allInstances as inst (inst.id)}
              <tr class="border-b last:border-0">
                <td class="px-4 py-3">
                  <a href="/instances/{inst.id}" class="font-medium text-primary-dark hover:underline">
                    {inst.name}
                  </a>
                  <p class="text-xs text-gray-400 font-mono">{inst.incus_name}</p>
                </td>
                <td class="px-4 py-3">
                  <p class="text-gray-900">{inst.user_name || inst.user_email}</p>
                  {#if inst.user_name}
                    <p class="text-xs text-gray-400">{inst.user_email}</p>
                  {/if}
                </td>
                <td class="px-4 py-3 text-gray-500">{inst.template_name || '—'}</td>
                <td class="px-4 py-3">
                  <span class="px-2 py-0.5 rounded-full text-xs font-medium {statusColors[inst.status] ?? 'bg-gray-100'}">
                    {inst.status}
                  </span>
                </td>
                <td class="px-4 py-3">
                  <div class="flex justify-end">
                    <DuplicateInstanceButton
                      instanceId={inst.id}
                      {users}
                      defaultUserId={inst.user_id}
                      onDone={load} />
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {:else if instanceList.length === 0}
    <div class="text-center py-12 card-static">
      <p class="text-gray-500 mb-4">No workspaces yet</p>
      <a href="/instances/new" class="link">Create your first instance</a>
    </div>
  {:else}
    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      {#each instanceList as inst (inst.id)}
        <InstanceCard instance={inst} {users} onRefresh={load} />
      {/each}
    </div>
  {/if}
</div>
