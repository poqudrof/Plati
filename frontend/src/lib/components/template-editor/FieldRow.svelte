<script lang="ts">
  import type { Snippet } from 'svelte';

  let { label, description = '', children }: {
    label: string;
    description?: string;
    children: Snippet;
  } = $props();
</script>

<div class="space-y-1">
  <label class="block text-sm font-medium text-gray-700">{label}</label>
  {#if description}
    <p class="text-xs text-gray-400">{description}</p>
  {/if}
  {@render children()}
</div>
