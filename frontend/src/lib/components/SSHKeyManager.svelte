<script lang="ts">
  import type { SSHKey } from '$lib/api/types';
  import { users } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';

  // Access keys (user-provided public keys — for SSH login from laptop)
  let accessKeys: SSHKey[] = $state([]);
  let newAccessKeyName = $state('');
  let newAccessKeyValue = $state('');
  let accessKeyLoading = $state(false);
  let showAddAccessKeyForm = $state(false);

  async function loadAccessKeys() {
    try { accessKeys = await users.listSSHKeys(); }
    catch (e: any) { addNotification('error', e.message); }
  }

  async function addAccessKey() {
    if (!newAccessKeyName.trim() || !newAccessKeyValue.trim()) return;
    accessKeyLoading = true;
    try {
      await users.createSSHKey(newAccessKeyName.trim(), newAccessKeyValue.trim());
      newAccessKeyName = '';
      newAccessKeyValue = '';
      showAddAccessKeyForm = false;
      await loadAccessKeys();
      addNotification('success', 'Access key added');
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      accessKeyLoading = false;
    }
  }

  async function removeAccessKey(id: number) {
    try {
      await users.deleteSSHKey(id);
      await loadAccessKeys();
      addNotification('success', 'Access key removed');
    } catch (e: any) { addNotification('error', e.message); }
  }

  loadAccessKeys();
</script>

<!-- ─── Access Keys ──────────────────────────────────────────── -->
<div class="card-static p-6">

  <div class="flex items-start gap-3 mb-5">
    <div class="bg-primary-50 rounded-xl p-2.5 shrink-0">
      <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>
      </svg>
    </div>
    <div>
      <h3 class="text-base font-bold text-gray-900">Access Keys</h3>
      <p class="text-sm text-gray-500 leading-relaxed">Your laptop's public key — lets you SSH into your instances from your machine.</p>
    </div>
  </div>

  <!-- Diagram -->
  <div class="rounded-xl bg-gray-50 border border-gray-200 p-4 mb-6 flex items-center justify-between gap-3 text-sm">
    <!-- Laptop -->
    <div class="flex flex-col items-center gap-1.5 flex-1">
      <div class="bg-white border border-gray-200 rounded-lg px-3 py-2.5 flex flex-col items-center gap-1 w-full">
        <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#6B7280" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>
        </svg>
        <span class="font-mono text-xs text-gray-500">~/.ssh/id_rsa</span>
      </div>
      <span class="text-xs text-gray-400 font-medium">Your laptop</span>
    </div>

    <!-- Arrow -->
    <div class="flex flex-col items-center gap-0.5 shrink-0">
      <div class="flex items-center gap-1 text-primary font-semibold text-xs">
        <div class="w-6 h-px bg-primary"></div>
        <span>SSH</span>
        <div class="w-6 h-px bg-primary"></div>
        <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>
      </div>
      <span class="text-xs text-gray-400">port 22</span>
    </div>

    <!-- Instance -->
    <div class="flex flex-col items-center gap-1.5 flex-1">
      <div class="bg-white border border-gray-200 rounded-lg px-3 py-2.5 flex flex-col items-center gap-1 w-full">
        <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#6B7280" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><path d="M6 6h.01M6 18h.01"/>
        </svg>
        <span class="font-mono text-xs text-gray-500">authorized_keys</span>
      </div>
      <span class="text-xs text-gray-400 font-medium">Your instance</span>
    </div>
  </div>
  <p class="text-xs text-gray-400 -mt-4 mb-5 text-center">Your laptop's <strong class="text-gray-500">private key</strong> stays on your machine. Paste the <strong class="text-gray-500">public key</strong> below.</p>

  <!-- Key list -->
  <div class="space-y-2 mb-5">
    {#each accessKeys as key}
      <div class="flex justify-between items-center px-3 py-2.5 bg-gray-50 rounded-lg border border-gray-200">
        <div class="min-w-0 flex-1">
          <span class="text-sm font-medium text-gray-800">{key.name}</span>
          <span class="text-xs text-gray-400 ml-2">{new Date(key.created_at).toLocaleDateString()}</span>
          <p class="font-mono text-xs text-gray-400 truncate mt-0.5">{key.public_key.substring(0, 52)}…</p>
        </div>
        <button onclick={() => removeAccessKey(key.id)} class="btn-danger btn-sm ml-3 shrink-0">Remove</button>
      </div>
    {/each}
    {#if accessKeys.length === 0}
      <p class="text-sm text-gray-400 py-2">No access keys added yet.</p>
    {/if}
  </div>

  <!-- Add form -->
  <div class="border-t border-gray-100 pt-4">
    {#if accessKeys.length === 0 || showAddAccessKeyForm}
      <div class="space-y-2">
        {#if accessKeys.length > 0}
          <p class="text-xs font-medium text-gray-500 mb-2">Add another public key</p>
        {:else}
          <p class="text-xs font-medium text-gray-500 mb-2">Add a public key</p>
        {/if}
        <input
          bind:value={newAccessKeyName}
          placeholder="Name (e.g. Work laptop)"
          class="input w-full text-sm"
        />
        <textarea
          bind:value={newAccessKeyValue}
          placeholder="Paste public key — ssh-ed25519 AAAA… or ssh-rsa AAAA…"
          rows="2"
          class="input w-full font-mono text-xs resize-none"
        ></textarea>
        <div class="flex items-center gap-2">
          <button
            onclick={addAccessKey}
            disabled={accessKeyLoading || !newAccessKeyName.trim() || !newAccessKeyValue.trim()}
            class="btn-primary btn-sm"
          >
            {accessKeyLoading ? 'Adding…' : 'Add Key'}
          </button>
          {#if accessKeys.length > 0}
            <button onclick={() => { showAddAccessKeyForm = false; newAccessKeyName = ''; newAccessKeyValue = ''; }} class="btn-outline btn-sm">Cancel</button>
          {/if}
        </div>
      </div>
    {:else}
      <button onclick={() => showAddAccessKeyForm = true} class="btn-outline btn-sm">
        Add another key
      </button>
    {/if}
  </div>
</div>

