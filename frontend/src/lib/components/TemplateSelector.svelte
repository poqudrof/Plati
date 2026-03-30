<script lang="ts">
  import type { Template } from '$lib/api/types';

  let { templates = [], selected = $bindable<number | null>(null) }: { templates: Template[]; selected?: number | null } = $props();
</script>

<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
  {#each templates as tmpl}
    <button
      onclick={() => selected = tmpl.id}
      class="p-4 rounded-lg border-2 text-left transition-colors
        {selected === tmpl.id ? 'border-indigo-500 bg-indigo-50' : 'border-gray-200 hover:border-gray-300'}"
    >
      <h4 class="font-medium">{tmpl.name}</h4>
      <p class="text-sm text-gray-500 mt-1">{tmpl.description}</p>
      <p class="text-xs text-gray-400 mt-2">Image: {tmpl.image}</p>
    </button>
  {/each}
</div>
