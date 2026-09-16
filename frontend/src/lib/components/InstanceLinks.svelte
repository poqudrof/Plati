<script lang="ts">
  import { browser } from '$app/environment';
  import type { Instance, InstanceLinksResult } from '$lib/api/types';
  import { instances } from '$lib/api';

  let { instance, compact = false }: {
    instance: Instance;
    /** Inline chips with no header, for the admin "All users" table. */
    compact?: boolean;
  } = $props();

  let result = $state<InstanceLinksResult | null>(null);
  let sshxRetry: ReturnType<typeof setTimeout> | null = null;

  // Pinned links only. The hostname link is pinned by default, so it is kept only when it
  // can actually be opened — otherwise every card would carry a dead "nothing on 443" row.
  let pinned = $derived(
    result?.links.filter(l => l.pinned && (l.kind !== 'hostname' || l.url)) ?? []
  );

  async function load() {
    if (sshxRetry) { clearTimeout(sshxRetry); sshxRetry = null; }
    try {
      result = await instances.links(instance.id);
      // sshx prints its session URL a few seconds after the service starts: poll until
      // it shows up rather than leave the card saying "waiting" until the next reload.
      if (instance.status === 'running' &&
          result.links.some(l => l.kind === 'sshx' && l.pinned && l.active && !l.url)) {
        sshxRetry = setTimeout(load, 5000);
      }
    } catch { result = null; }
  }

  // Reloaded when the status changes: every URL but the custom ones depends on it.
  $effect(() => {
    if (browser) {
      instance.status;
      load();
    }
    return () => { if (sshxRetry) clearTimeout(sshxRetry); };
  });

  const kindLabels: Record<string, string> = {
    hostname: 'HTTPS',
    openvscode: 'IDE',
    sshx: 'Terminal',
    custom: 'Link',
  };
</script>

{#if compact}
  {#if pinned.length > 0}
    <div class="flex flex-wrap gap-1.5">
      {#each pinned as link}
        {#if link.url}
          <a href={link.url} target="_blank" rel="noopener noreferrer" title={link.url}
            class="inline-flex items-center gap-1 max-w-[12rem] px-2 py-0.5 rounded border text-xs text-primary-dark hover:bg-gray-50">
            <span class="w-1.5 h-1.5 rounded-full shrink-0 {link.active ? 'bg-green-500' : 'bg-gray-300'}"></span>
            <span class="truncate">{link.label}</span>
          </a>
        {:else}
          <span title={link.note} class="inline-flex items-center gap-1 max-w-[12rem] px-2 py-0.5 rounded border text-xs text-gray-400">
            <span class="w-1.5 h-1.5 rounded-full shrink-0 bg-gray-300"></span>
            <span class="truncate">{link.label}</span>
          </span>
        {/if}
      {/each}
    </div>
  {:else if result}
    <span class="text-gray-300">—</span>
  {/if}
{:else}
<div class="space-y-2">
  <div class="flex items-center justify-between">
    <p class="text-xs font-semibold text-gray-400 uppercase tracking-wider">Links</p>
    <a href="/instances/{instance.id}?tab=links" class="text-xs text-primary hover:text-primary-dark">
      Manage
    </a>
  </div>

  {#if pinned.length > 0}
    <ul class="divide-y border rounded">
      {#each pinned as link}
        <li class="flex items-center gap-2 px-3 py-2 text-sm">
          <span class="w-2 h-2 rounded-full shrink-0 {link.url && link.active ? 'bg-green-500' : 'bg-gray-300'}"></span>
          <span class="text-[10px] uppercase tracking-wide text-gray-400 w-14 shrink-0">{kindLabels[link.kind]}</span>
          {#if link.url}
            <a href={link.url} target="_blank" rel="noopener noreferrer"
              class="flex-1 min-w-0 truncate text-primary hover:text-primary-dark hover:underline"
              title={link.url}>{link.label} ↗</a>
          {:else}
            <span class="flex-1 min-w-0 truncate text-gray-500">{link.label}</span>
          {/if}
          {#if link.note}
            <span class="text-xs text-gray-400 shrink-0">{link.note}</span>
          {/if}
        </li>
      {/each}
    </ul>
  {:else if result}
    <p class="text-xs text-gray-400">No pinned links.</p>
  {/if}
</div>
{/if}
