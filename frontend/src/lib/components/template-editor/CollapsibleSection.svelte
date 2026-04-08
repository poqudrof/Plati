<script lang="ts">
  import type { Snippet } from 'svelte';

  let { title, description = '', open = true, children }: {
    title: string;
    description?: string;
    open?: boolean;
    children: Snippet;
  } = $props();

  let expanded = $state(open);
</script>

<div class="border border-gray-200 rounded-lg bg-white">
  <button
    type="button"
    class="w-full flex items-center justify-between px-4 py-3 hover:bg-gray-50 transition-colors"
    onclick={() => expanded = !expanded}
  >
    <div class="text-left">
      <h3 class="text-sm font-semibold text-gray-900">{title}</h3>
      {#if description}
        <p class="text-xs text-gray-500 mt-0.5">{description}</p>
      {/if}
    </div>
    <svg
      class="w-4 h-4 text-gray-400 transition-transform {expanded ? 'rotate-180' : ''}"
      fill="none" viewBox="0 0 24 24" stroke="currentColor"
    >
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
    </svg>
  </button>
  {#if expanded}
    <div class="px-4 pb-4 pt-1 border-t border-gray-100">
      {@render children()}
    </div>
  {/if}
</div>
