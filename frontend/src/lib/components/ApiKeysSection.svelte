<script lang="ts">
  import type { ApiKey } from '$lib/api/types';
  import { users } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';

  let apiKeys: ApiKey[] = $state([]);
  let busy = $state(false);
  let revealedKey: string | null = $state(null);

  async function loadApiKeys() {
    try { apiKeys = await users.listApiKeys(); }
    catch (e: any) { addNotification('error', e.message); }
  }

  async function regenerate(id: number) {
    if (!confirm('Regenerate this key? Any script or agent using the current key stops working immediately.')) return;
    busy = true;
    try {
      const result = await users.regenerateApiKey(id);
      revealedKey = result.key;
      await loadApiKeys();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      busy = false;
    }
  }

  function copyKey() {
    if (!revealedKey) return;
    navigator.clipboard.writeText(revealedKey);
    addNotification('success', 'API key copied');
  }

  loadApiKeys();
</script>

<!-- ─── API Keys ─────────────────────────────────────────────── -->
<div class="card-static p-6">

  <div class="flex items-start gap-3 mb-5">
    <div class="bg-primary-50 rounded-xl p-2.5 shrink-0">
      <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6M15.5 7.5l3 3L22 7l-3-3"/>
      </svg>
    </div>
    <div>
      <h3 class="text-base font-bold text-gray-900">API Keys</h3>
      <p class="text-sm text-gray-500 leading-relaxed">
        Used by scripts or agents to call the Plati API as you (<code class="bg-gray-100 px-1 rounded font-mono text-xs">Authorization: Bearer &lt;key&gt;</code>).
        Only an admin can create one — ask them if you don't have one yet. You can regenerate it here at any time.
      </p>
    </div>
  </div>

  <div class="space-y-2">
    {#each apiKeys as key}
      <div class="flex justify-between items-center px-3 py-2.5 bg-gray-50 rounded-lg border border-gray-200">
        <div class="min-w-0 flex-1">
          <span class="text-sm font-medium text-gray-800">{key.name}</span>
          <span class="text-xs text-gray-400 ml-2">{new Date(key.created_at).toLocaleDateString()}</span>
          <p class="font-mono text-xs text-gray-400 mt-0.5">{key.key_prefix}…</p>
          <p class="text-xs text-gray-400 mt-0.5">
            {key.last_used_at ? `Last used ${new Date(key.last_used_at).toLocaleString()}` : 'Never used'}
          </p>
        </div>
        <button onclick={() => regenerate(key.id)} disabled={busy} class="btn-outline btn-sm ml-3 shrink-0 disabled:opacity-50">
          Regenerate
        </button>
      </div>
    {/each}
    {#if apiKeys.length === 0}
      <p class="text-sm text-gray-400 py-2">No API key yet — ask an admin to create one for you.</p>
    {/if}
  </div>
</div>

<!-- Regenerated key reveal modal -->
{#if revealedKey}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
    <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4">
      <div class="p-6">
        <h2 class="text-lg font-bold mb-1">New API key</h2>
        <p class="text-sm text-amber-700 bg-amber-50 border border-amber-200 rounded p-3 mb-4">
          Save this now — it won't be shown again.
        </p>
        <div class="flex items-center gap-2">
          <code class="text-xs bg-gray-100 px-2 py-1 rounded font-mono truncate flex-1">{revealedKey}</code>
          <button onclick={copyKey} class="text-xs text-primary hover:underline shrink-0">Copy</button>
        </div>
        <div class="flex justify-end mt-6 pt-4 border-t">
          <button onclick={() => revealedKey = null} class="btn-primary">Done</button>
        </div>
      </div>
    </div>
  </div>
{/if}
