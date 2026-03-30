<script lang="ts">
  import { currentUser, isLoading } from '$lib/stores/auth';
  import { goto } from '$app/navigation';
  import type { Snippet } from 'svelte';
  import { browser } from '$app/environment';

  let { children }: { children: Snippet } = $props();

  $effect(() => {
    if (browser && !$isLoading && !$currentUser) {
      goto('/login');
    }
  });
</script>

{#if !$isLoading && $currentUser}
  {@render children()}
{:else if $isLoading}
  <div class="flex justify-center py-12">
    <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
  </div>
{/if}
