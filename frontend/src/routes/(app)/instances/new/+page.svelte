<script lang="ts">
  import { browser } from '$app/environment';
  import { templates as templatesApi, instances } from '$lib/api';
  import type { Template } from '$lib/api/types';
  import TemplateSelector from '$lib/components/TemplateSelector.svelte';
  import { goto } from '$app/navigation';
  import { addNotification } from '$lib/stores/notifications';

  let templateList: Template[] = $state([]);
  let selectedTemplate: number | null = $state(null);
  let name = $state('');
  let loading = $state(false);

  if (browser) {
    (async () => {
      templateList = await templatesApi.list();
    })();
  }

  async function create() {
    if (!name || !selectedTemplate) return;
    loading = true;
    try {
      await instances.create(name, selectedTemplate);
      addNotification('success', 'Instance created');
      goto('/dashboard');
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

    <button
      onclick={create}
      disabled={loading || !name || !selectedTemplate}
      class="px-6 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:opacity-50"
    >
      {loading ? 'Creating...' : 'Create Instance'}
    </button>
  </div>
</div>
