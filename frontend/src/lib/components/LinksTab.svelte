<script lang="ts">
  import { browser } from '$app/environment';
  import { instances } from '$lib/api';
  import type { InstanceLinksResult, InstanceLinksUpdate, ResolvedLink, CustomLink } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  interface Props {
    instanceId: number;
    instanceStatus: string;
  }

  let { instanceId, instanceStatus }: Props = $props();

  let result = $state<InstanceLinksResult | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let error = $state('');

  // Add form
  let newLabel = $state('');
  let newUrl = $state('');
  let newPinned = $state(true);

  // Inline edit of one custom link, by its index in settings.custom
  let editIndex = $state(-1);
  let editLabel = $state('');
  let editUrl = $state('');

  let sshxRetry: ReturnType<typeof setTimeout> | null = null;

  // Reloaded when the status changes: every built-in URL depends on it.
  $effect(() => {
    if (browser && instanceId) {
      instanceStatus;
      load();
    }
    return () => { if (sshxRetry) clearTimeout(sshxRetry); };
  });

  async function load() {
    if (sshxRetry) { clearTimeout(sshxRetry); sshxRetry = null; }
    error = '';
    try {
      result = await instances.links(instanceId);
      // sshx prints its session URL a few seconds after the service starts.
      if (instanceStatus === 'running' &&
          result.links.some(l => l.kind === 'sshx' && l.active && !l.url)) {
        sshxRetry = setTimeout(load, 5000);
      }
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function update(patch: InstanceLinksUpdate, msg?: string): Promise<boolean> {
    saving = true;
    try {
      result = await instances.updateLinks(instanceId, patch);
      if (msg) addNotification('success', msg);
      return true;
    } catch (e: any) {
      addNotification('error', e.message);
      return false;
    } finally {
      saving = false;
    }
  }

  // The custom list is always sent whole, so every edit starts from the stored one.
  function customList(): CustomLink[] {
    return (result?.settings.custom ?? []).map(c => ({ ...c }));
  }

  function togglePin(link: ResolvedLink) {
    const pinned = !link.pinned;
    if (link.kind === 'custom') {
      const list = customList();
      list[link.index].pinned = pinned;
      update({ custom: list });
    } else {
      update({ [link.kind]: pinned });
    }
  }

  async function addLink() {
    if (!newUrl.trim()) return;
    const ok = await update(
      { custom: [...customList(), { label: newLabel, url: newUrl, pinned: newPinned }] },
      'Link added');
    if (ok) { newLabel = ''; newUrl = ''; newPinned = true; }
  }

  function startEdit(link: ResolvedLink) {
    editIndex = link.index;
    editLabel = link.label;
    editUrl = link.url;
  }

  async function saveEdit() {
    const list = customList();
    list[editIndex] = { ...list[editIndex], label: editLabel, url: editUrl };
    if (await update({ custom: list }, 'Link saved')) editIndex = -1;
  }

  function removeLink(link: ResolvedLink) {
    if (!confirm(`Remove the link "${link.label}"?`)) return;
    update({ custom: customList().filter((_, i) => i !== link.index) }, 'Link removed');
  }

  let builtins = $derived(result?.links.filter(l => l.kind !== 'custom') ?? []);
  let customs = $derived(result?.links.filter(l => l.kind === 'custom') ?? []);
  let pinnedCount = $derived(
    result?.links.filter(l => l.pinned && (l.kind !== 'hostname' || l.url)).length ?? 0
  );

  const kindLabels: Record<string, string> = {
    hostname: 'HTTPS',
    openvscode: 'IDE',
    sshx: 'Terminal',
    custom: 'Link',
  };
</script>

{#snippet pinButton(link: ResolvedLink)}
  <button
    onclick={() => togglePin(link)}
    disabled={saving}
    title={link.pinned ? 'Unpin from the dashboard card' : 'Pin to the dashboard card'}
    class="px-2 py-1 rounded text-xs font-medium border shrink-0 disabled:opacity-50
      {link.pinned ? 'bg-primary text-white border-primary hover:bg-primary-dark' : 'text-gray-500 hover:bg-gray-50'}">
    {link.pinned ? '📌 Pinned' : 'Pin'}
  </button>
{/snippet}

{#snippet linkTarget(link: ResolvedLink)}
  <div class="flex-1 min-w-0">
    {#if link.url}
      <a href={link.url} target="_blank" rel="noopener noreferrer"
        class="block truncate text-primary hover:text-primary-dark hover:underline" title={link.url}>
        {link.label} ↗
      </a>
      <p class="text-xs text-gray-400 font-mono truncate">{link.url}</p>
    {:else}
      <p class="truncate text-gray-600">{link.label}</p>
    {/if}
    {#if link.note}
      <p class="text-xs text-gray-400">{link.note}</p>
    {/if}
  </div>
{/snippet}

<div class="p-6 space-y-6">
  <div>
    <h3 class="text-base font-semibold">Links</h3>
    <p class="text-sm text-gray-500 mt-1">
      Keep the links you use with this machine. Pinned links are listed on its dashboard card
      ({pinnedCount} shown there right now).
    </p>
  </div>

  {#if loading}
    <p class="text-sm text-gray-400 animate-pulse">Loading links…</p>
  {:else if error}
    <p class="text-sm text-red-600">{error}</p>
  {:else if result}

    <!-- Built-in links -->
    <section class="space-y-2">
      <p class="text-xs font-semibold text-gray-400 uppercase tracking-wider">Built-in</p>
      <ul class="divide-y border rounded">
        {#each builtins as link (link.kind)}
          <li class="flex items-center gap-3 px-3 py-2 text-sm">
            <span class="w-2 h-2 rounded-full shrink-0 {link.url && link.active ? 'bg-green-500' : 'bg-gray-300'}"></span>
            <span class="text-[10px] uppercase tracking-wide text-gray-400 w-14 shrink-0">{kindLabels[link.kind]}</span>
            {@render linkTarget(link)}
            {@render pinButton(link)}
          </li>
        {/each}
      </ul>
      <p class="text-xs text-gray-400">
        The HTTPS link uses the machine's Tailscale name and appears on the card only while
        something serves port 443 (Tailscale Serve or a local listener).
      </p>
    </section>

    <!-- Custom links -->
    <section class="space-y-2">
      <p class="text-xs font-semibold text-gray-400 uppercase tracking-wider">My links</p>
      {#if customs.length > 0}
        <ul class="divide-y border rounded">
          {#each customs as link (link.index)}
            <li class="flex items-center gap-3 px-3 py-2 text-sm">
              {#if editIndex === link.index}
                <input bind:value={editLabel} placeholder="Label"
                  class="w-1/4 min-w-0 border rounded px-2 py-1 text-sm" />
                <input bind:value={editUrl} placeholder="https://…" type="url"
                  class="flex-1 min-w-0 border rounded px-2 py-1 text-sm font-mono" />
                <button onclick={saveEdit} disabled={saving}
                  class="px-2 py-1 bg-primary text-white rounded text-xs hover:bg-primary-dark disabled:opacity-50">Save</button>
                <button onclick={() => editIndex = -1} disabled={saving}
                  class="px-2 py-1 border rounded text-xs text-gray-600 hover:bg-gray-50">Cancel</button>
              {:else}
                {@render linkTarget(link)}
                {@render pinButton(link)}
                <button onclick={() => startEdit(link)} disabled={saving}
                  class="px-2 py-1 border rounded text-xs text-gray-600 hover:bg-gray-50 shrink-0">Edit</button>
                <button onclick={() => removeLink(link)} disabled={saving}
                  class="px-2 py-1 border rounded text-xs text-red-600 hover:bg-red-50 shrink-0">Remove</button>
              {/if}
            </li>
          {/each}
        </ul>
      {:else}
        <p class="text-sm text-gray-400">No links noted yet.</p>
      {/if}

      <form onsubmit={(e) => { e.preventDefault(); addLink(); }}
        class="flex flex-wrap items-center gap-2 pt-2">
        <input bind:value={newLabel} placeholder="Label (optional)"
          class="w-40 min-w-0 border rounded px-2 py-1.5 text-sm" />
        <input bind:value={newUrl} placeholder="https://…" type="url" required
          class="flex-1 min-w-[12rem] border rounded px-2 py-1.5 text-sm font-mono" />
        <label class="flex items-center gap-1.5 text-sm text-gray-600">
          <input type="checkbox" bind:checked={newPinned} class="rounded" />
          Pin
        </label>
        <button type="submit" disabled={saving || !newUrl.trim()}
          class="px-3 py-1.5 bg-primary text-white rounded text-sm hover:bg-primary-dark disabled:opacity-50">
          Add link
        </button>
      </form>
    </section>
  {/if}
</div>
