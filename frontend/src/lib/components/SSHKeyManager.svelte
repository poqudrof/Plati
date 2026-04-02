<script lang="ts">
  import type { UserSSHKey, GeneratedKeyResult } from '$lib/api/types';
  import { users } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';

  let keys: UserSSHKey[] = $state([]);
  let newName = $state('');
  let loading = $state(false);
  let generated: GeneratedKeyResult | null = $state(null);

  async function load() {
    try {
      keys = await users.listUserKeys();
    } catch (e: any) {
      addNotification('error', e.message);
    }
  }

  async function generate() {
    if (!newName.trim()) return;
    loading = true;
    try {
      generated = await users.generateUserKey(newName.trim());
      newName = '';
      await load();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      loading = false;
    }
  }

  async function remove(id: number) {
    try {
      await users.deleteUserKey(id);
      await load();
      addNotification('success', 'Key removed');
    } catch (e: any) {
      addNotification('error', e.message);
    }
  }

  function copy(text: string, label: string) {
    navigator.clipboard.writeText(text);
    addNotification('success', `${label} copied`);
  }

  load();
</script>

<div>
  <h3 class="text-lg font-semibold mb-1">SSH Keys</h3>
  <p class="text-sm text-gray-500 mb-4">
    Plati generates an SSH keypair for you. Add the <strong>public key</strong> to your
    <a href="https://github.com/settings/keys" target="_blank" class="text-indigo-600 hover:underline">GitHub account</a>
    (or any git host). The private key is automatically injected into your instances so
    <code class="bg-gray-100 px-1 rounded">git</code> works out of the box.
  </p>

  <div class="space-y-3 mb-6">
    {#each keys as key}
      <div class="p-3 bg-gray-50 rounded border">
        <div class="flex justify-between items-start">
          <div class="min-w-0 flex-1">
            <span class="font-medium">{key.name}</span>
            <span class="text-xs text-gray-400 ml-2">
              {new Date(key.created_at).toLocaleDateString()}
            </span>
          </div>
          <button onclick={() => remove(key.id)} class="text-red-600 hover:text-red-800 text-sm ml-4 shrink-0">Remove</button>
        </div>
        <div class="flex items-center gap-2 mt-2">
          <code class="text-xs text-gray-500 font-mono truncate flex-1">{key.public_key.substring(0, 60)}…</code>
          <button
            onclick={() => copy(key.public_key, 'Public key')}
            class="text-xs text-indigo-600 hover:text-indigo-800 shrink-0 border border-indigo-200 rounded px-2 py-0.5"
          >
            Copy public key
          </button>
        </div>
      </div>
    {/each}
    {#if keys.length === 0}
      <p class="text-gray-500 text-sm">No keys generated yet.</p>
    {/if}
  </div>

  <div class="border-t pt-4 flex gap-3">
    <input
      bind:value={newName}
      placeholder="Key name (e.g. work laptop)"
      class="flex-1 px-3 py-2 border rounded text-sm"
      onkeydown={(e) => e.key === 'Enter' && generate()}
    />
    <button
      onclick={generate}
      disabled={loading || !newName.trim()}
      class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700 disabled:opacity-50 text-sm whitespace-nowrap"
    >
      Generate Key
    </button>
  </div>
</div>

<!-- One-time key reveal modal -->
{#if generated}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
    <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4">
      <div class="p-6">
        <h2 class="text-lg font-bold mb-1">Key generated: {generated.name}</h2>
        <p class="text-sm text-amber-700 bg-amber-50 border border-amber-200 rounded p-3 mb-4">
          Save your private key now — it won't be shown again. If you lose it, generate a new key.
        </p>

        <div class="space-y-4">
          <div>
            <div class="flex justify-between items-center mb-1">
              <label class="text-sm font-medium text-gray-700">Public key</label>
              <button onclick={() => copy(generated!.public_key, 'Public key')} class="text-xs text-indigo-600 hover:underline">Copy</button>
            </div>
            <p class="text-xs text-gray-500 mb-1">Add this to your GitHub account settings.</p>
            <textarea readonly rows="2" class="w-full px-3 py-2 border rounded font-mono text-xs bg-gray-50">{generated.public_key}</textarea>
          </div>

          <div>
            <div class="flex justify-between items-center mb-1">
              <label class="text-sm font-medium text-gray-700">Private key</label>
              <button onclick={() => copy(generated!.private_key, 'Private key')} class="text-xs text-indigo-600 hover:underline">Copy</button>
            </div>
            <p class="text-xs text-gray-500 mb-1">
              Save as <code class="bg-gray-100 px-1 rounded">~/.ssh/plati_key</code> on your laptop to SSH into instances.
            </p>
            <textarea readonly rows="6" class="w-full px-3 py-2 border rounded font-mono text-xs bg-gray-50">{generated.private_key}</textarea>
          </div>
        </div>

        <div class="flex justify-end mt-6 pt-4 border-t">
          <button onclick={() => generated = null} class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700">
            Done
          </button>
        </div>
      </div>
    </div>
  </div>
{/if}
