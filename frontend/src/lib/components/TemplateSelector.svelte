<script lang="ts">
  import type { Template } from '$lib/api/types';

  let { templates = [], selected = $bindable<number | null>(null) }: { templates: Template[]; selected?: number | null } = $props();
</script>

<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
  {#each templates as tmpl}
    <button
      onclick={() => selected = tmpl.id}
      class="p-4 rounded-lg border-2 text-left transition-colors
        {selected === tmpl.id ? 'border-primary bg-primary-50' : 'border-gray-200 hover:border-gray-300'}"
    >
      <h4 class="font-medium">{tmpl.name}</h4>
      <p class="text-sm text-gray-500 mt-1">{tmpl.description}</p>
      <p class="text-xs text-gray-400 mt-2">Image: {tmpl.image}</p>
      <div class="flex flex-wrap items-center gap-1.5 mt-2">
        {#if tmpl.persistence_mode === 'ephemeral'}
          <span class="text-xs px-1.5 py-0.5 rounded bg-amber-50 text-amber-700 border border-amber-200">Ephemeral</span>
          <span class="text-xs text-gray-400">No persistent storage</span>
        {:else}
          {@const persistDirs = (() => { try { return JSON.parse(tmpl.persistence_dirs || '[]') as {path: string; size: string; pool?: string}[]; } catch { return []; } })()}
          <span class="text-xs px-1.5 py-0.5 rounded bg-green-50 text-green-700 border border-green-200">Persistent</span>
          {#each persistDirs as dir}
            <span class="text-xs text-gray-500 font-mono">{dir.path} ({dir.size})</span>
          {/each}
        {/if}
      </div>
    </button>
  {/each}
</div>
