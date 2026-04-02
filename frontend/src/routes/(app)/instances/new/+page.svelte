<script lang="ts">
  import { browser } from '$app/environment';
  import { templates as templatesApi, instances, users } from '$lib/api';
  import type { Template, UserPreferences } from '$lib/api/types';
  import TemplateSelector from '$lib/components/TemplateSelector.svelte';
  import { goto } from '$app/navigation';
  import { addNotification } from '$lib/stores/notifications';

  let templateList: Template[] = $state([]);
  let selectedTemplate: number | null = $state(null);
  let name = $state('');
  let loading = $state(false);
  let showAdvanced = $state(false);
  let prefs: UserPreferences | null = $state(null);
  let sshModeOverride = $state('');
  let tailscaleModeOverride = $state('');

  if (browser) {
    (async () => {
      templateList = await templatesApi.list();
      prefs = await users.preferences();
    })();
  }

  function selectedTemplateMeta(): Template | undefined {
    return templateList.find(t => t.id === selectedTemplate);
  }

  async function create() {
    if (!name || !selectedTemplate) return;
    loading = true;
    try {
      const opts: { ssh_key_mode?: string; tailscale_mode?: string } = {};
      if (sshModeOverride) opts.ssh_key_mode = sshModeOverride;
      if (tailscaleModeOverride) opts.tailscale_mode = tailscaleModeOverride;
      const inst = await instances.create(name, selectedTemplate, Object.keys(opts).length ? opts : undefined);
      addNotification('success', 'Instance creation started');
      goto(`/instances/${inst.id}`);
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      loading = false;
    }
  }
</script>

<div>
  <h1 class="text-2xl font-bold mb-6">Create New Instance</h1>

  <div class="bg-white rounded-lg border p-6 space-y-6">
    <div>
      <label for="name" class="block text-sm font-medium text-gray-700 mb-1">Instance Name</label>
      <input
        id="name"
        bind:value={name}
        placeholder="my-workspace"
        class="w-full px-4 py-2 border rounded-lg"
        required
      />
    </div>

    <div>
      <p class="block text-sm font-medium text-gray-700 mb-3">Select Template</p>
      <TemplateSelector templates={templateList} bind:selected={selectedTemplate} />
    </div>

    <!-- Advanced section -->
    <div class="border-t pt-4">
      <button
        type="button"
        onclick={() => showAdvanced = !showAdvanced}
        class="text-sm text-gray-500 hover:text-gray-700 flex items-center gap-1"
      >
        <span>{showAdvanced ? '▾' : '▸'}</span>
        Advanced options
      </button>

      {#if showAdvanced}
      <div class="mt-4 space-y-4 pl-3 border-l-2 border-gray-100">
        <div>
          <p class="text-sm font-medium text-gray-700 mb-1">SSH Key Mode override</p>
          <p class="text-xs text-gray-500 mb-2">
            Default: <code class="bg-gray-100 px-1 rounded">{prefs?.ssh_key_mode ?? '…'}</code>
          </p>
          <select bind:value={sshModeOverride} class="px-3 py-1.5 border rounded text-sm">
            <option value="">Use preference</option>
            <option value="plati">plati — inject admin-managed key</option>
            <option value="personal">personal — inject your personal key</option>
          </select>
        </div>

        {#if selectedTemplateMeta()}
        <div>
          <p class="text-sm font-medium text-gray-700 mb-1">Tailscale auth key override</p>
          <p class="text-xs text-gray-500 mb-2">
            Default: <code class="bg-gray-100 px-1 rounded">{prefs?.tailscale_mode ?? '…'}</code>
          </p>
          <select bind:value={tailscaleModeOverride} class="px-3 py-1.5 border rounded text-sm">
            <option value="">Use preference</option>
            <option value="plati">plati — use platform key</option>
            <option value="personal">personal — use my TAILSCALE_AUTH_KEY secret</option>
          </select>
        </div>
        {/if}
      </div>
      {/if}
    </div>

    <button
      onclick={create}
      disabled={loading || !name || !selectedTemplate}
      class="px-6 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:opacity-50"
    >
      {loading ? 'Creating...' : 'Create Instance'}
    </button>
  </div>
</div>
