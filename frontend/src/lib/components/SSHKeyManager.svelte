<script lang="ts">
  import type { UserSSHKey, GeneratedKeyResult, SSHKey } from '$lib/api/types';
  import { users } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';

  // Machine keys (Plati-generated keypairs — injected into instances for git access)
  let machineKeys: UserSSHKey[] = $state([]);
  let newMachineKeyName = $state('');
  let machineKeyLoading = $state(false);
  let generatedKey: GeneratedKeyResult | null = $state(null);

  // Access keys (user-provided public keys — for SSH login from laptop)
  let accessKeys: SSHKey[] = $state([]);
  let newAccessKeyName = $state('');
  let newAccessKeyValue = $state('');
  let accessKeyLoading = $state(false);

  async function loadMachineKeys() {
    try { machineKeys = await users.listUserKeys(); }
    catch (e: any) { addNotification('error', e.message); }
  }

  async function loadAccessKeys() {
    try { accessKeys = await users.listSSHKeys(); }
    catch (e: any) { addNotification('error', e.message); }
  }

  async function generateMachineKey() {
    if (!newMachineKeyName.trim()) return;
    machineKeyLoading = true;
    try {
      generatedKey = await users.generateUserKey(newMachineKeyName.trim());
      newMachineKeyName = '';
      await loadMachineKeys();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      machineKeyLoading = false;
    }
  }

  async function deleteMachineKey(id: number) {
    try {
      await users.deleteUserKey(id);
      await loadMachineKeys();
      addNotification('success', 'Key removed');
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function addAccessKey() {
    if (!newAccessKeyName.trim() || !newAccessKeyValue.trim()) return;
    accessKeyLoading = true;
    try {
      await users.createSSHKey(newAccessKeyName.trim(), newAccessKeyValue.trim());
      newAccessKeyName = '';
      newAccessKeyValue = '';
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

  function copy(text: string, label: string) {
    navigator.clipboard.writeText(text);
    addNotification('success', `${label} copied`);
  }

  loadMachineKeys();
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
  <div class="border-t border-gray-100 pt-4 space-y-2">
    <p class="text-xs font-medium text-gray-500 mb-2">Add a public key</p>
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
    <button
      onclick={addAccessKey}
      disabled={accessKeyLoading || !newAccessKeyName.trim() || !newAccessKeyValue.trim()}
      class="btn-primary btn-sm"
    >
      {accessKeyLoading ? 'Adding…' : 'Add Key'}
    </button>
  </div>
</div>

<!-- ─── Machine Keys ─────────────────────────────────────────── -->
<div class="card-static p-6">

  <div class="flex items-start gap-3 mb-5">
    <div class="bg-secondary-50 rounded-xl p-2.5 shrink-0">
      <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#D97706" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="7.5" cy="15.5" r="5.5"/><path d="M21 2l-9.6 9.6M15.5 7.5 19 4M13 10l-2 2"/>
      </svg>
    </div>
    <div>
      <h3 class="text-base font-bold text-gray-900">Machine Keys</h3>
      <p class="text-sm text-gray-500 leading-relaxed">A keypair injected into your instances so they can clone and push to git repositories.</p>
    </div>
  </div>

  <!-- Diagram -->
  <div class="rounded-xl bg-gray-50 border border-gray-200 p-4 mb-6 flex items-center justify-between gap-3 text-sm">
    <!-- Instance -->
    <div class="flex flex-col items-center gap-1.5 flex-1">
      <div class="bg-white border border-gray-200 rounded-lg px-3 py-2.5 flex flex-col items-center gap-1 w-full">
        <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#6B7280" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><path d="M6 6h.01M6 18h.01"/>
        </svg>
        <span class="font-mono text-xs text-gray-500">~/.ssh/id_rsa</span>
      </div>
      <span class="text-xs text-gray-400 font-medium">Your instance</span>
    </div>

    <!-- Arrow -->
    <div class="flex flex-col items-center gap-0.5 shrink-0">
      <div class="flex items-center gap-1 text-secondary font-semibold text-xs">
        <div class="w-6 h-px bg-secondary"></div>
        <span>git</span>
        <div class="w-6 h-px bg-secondary"></div>
        <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>
      </div>
      <span class="text-xs text-gray-400">SSH</span>
    </div>

    <!-- GitHub -->
    <div class="flex flex-col items-center gap-1.5 flex-1">
      <div class="bg-white border border-gray-200 rounded-lg px-3 py-2.5 flex flex-col items-center gap-1 w-full">
        <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#6B7280" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="10"/><path d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.87a3.37 3.37 0 0 0-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0 0 20 4.77 5.07 5.07 0 0 0 19.91 1S18.73.65 16 2.48a13.38 13.38 0 0 0-7 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 0 0 5 4.77a5.44 5.44 0 0 0-1.5 3.78c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 0 0 9 18.13V22"/>
        </svg>
        <span class="font-mono text-xs text-gray-500">Deploy keys</span>
      </div>
      <span class="text-xs text-gray-400 font-medium">GitHub / repo</span>
    </div>
  </div>
  <p class="text-xs text-gray-400 -mt-4 mb-5 text-center">The <strong class="text-gray-500">private key</strong> is auto-injected into your instance. Add the <strong class="text-gray-500">public key</strong> to your repo as a deploy key.</p>

  <!-- Key list -->
  <div class="space-y-2 mb-5">
    {#each machineKeys as key}
      <div class="flex justify-between items-center px-3 py-2.5 bg-gray-50 rounded-lg border border-gray-200">
        <div class="min-w-0 flex-1">
          <span class="text-sm font-medium text-gray-800">{key.name}</span>
          <span class="text-xs text-gray-400 ml-2">{new Date(key.created_at).toLocaleDateString()}</span>
          <p class="font-mono text-xs text-gray-400 truncate mt-0.5">{key.public_key.substring(0, 52)}…</p>
        </div>
        <div class="flex items-center gap-2 ml-3 shrink-0">
          <button
            onclick={() => copy(key.public_key, 'Public key')}
            class="btn-outline btn-sm"
          >Copy public key</button>
          <button onclick={() => deleteMachineKey(key.id)} class="btn-danger btn-sm">Remove</button>
        </div>
      </div>
    {/each}
    {#if machineKeys.length === 0}
      <p class="text-sm text-gray-400 py-2">No machine keys generated yet.</p>
    {/if}
  </div>

  <!-- Generate form -->
  <div class="border-t border-gray-100 pt-4">
    <p class="text-xs font-medium text-gray-500 mb-2">Generate a new keypair</p>
    <div class="flex gap-2">
      <input
        bind:value={newMachineKeyName}
        placeholder="Name (e.g. work)"
        class="input flex-1 text-sm"
        onkeydown={(e) => e.key === 'Enter' && generateMachineKey()}
      />
      <button
        onclick={generateMachineKey}
        disabled={machineKeyLoading || !newMachineKeyName.trim()}
        class="btn-primary btn-sm whitespace-nowrap"
      >
        {machineKeyLoading ? 'Generating…' : 'Generate Key'}
      </button>
    </div>
  </div>
</div>

<!-- One-time key reveal modal -->
{#if generatedKey}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
    <div class="bg-white rounded-xl shadow-xl w-full max-w-lg mx-4">
      <div class="p-6">
        <div class="flex items-start gap-3 mb-4">
          <div class="bg-secondary-50 rounded-xl p-2.5 shrink-0">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#D97706" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="7.5" cy="15.5" r="5.5"/><path d="M21 2l-9.6 9.6M15.5 7.5 19 4M13 10l-2 2"/>
            </svg>
          </div>
          <div>
            <h2 class="text-base font-bold text-gray-900">Key generated — {generatedKey.name}</h2>
            <p class="text-sm text-gray-500">Two halves, two destinations.</p>
          </div>
        </div>

        <div class="rounded-xl bg-amber-50 border border-amber-200 p-3 mb-5 text-xs text-amber-700 leading-relaxed">
          Save the private key now if you need a local backup — it won't be shown again.
          In normal use you don't need it: Plati injects it into your instances automatically.
        </div>

        <div class="space-y-4">
          <!-- Public key -->
          <div class="rounded-xl border border-gray-200 overflow-hidden">
            <div class="flex items-center justify-between px-4 py-2.5 bg-primary-50 border-b border-gray-200">
              <div class="flex items-center gap-2">
                <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <circle cx="12" cy="12" r="10"/><path d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.87a3.37 3.37 0 0 0-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0 0 20 4.77 5.07 5.07 0 0 0 19.91 1S18.73.65 16 2.48a13.38 13.38 0 0 0-7 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 0 0 5 4.77a5.44 5.44 0 0 0-1.5 3.78c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 0 0 9 18.13V22"/>
                </svg>
                <span class="text-xs font-semibold text-primary">Public key → GitHub / repo deploy keys</span>
              </div>
              <button onclick={() => copy(generatedKey!.public_key, 'Public key')} class="text-xs text-primary hover:text-primary-dark font-medium">Copy</button>
            </div>
            <textarea readonly rows="2" class="w-full px-4 py-3 font-mono text-xs bg-white text-gray-600 resize-none outline-none">{generatedKey.public_key}</textarea>
          </div>

          <!-- Private key -->
          <div class="rounded-xl border border-gray-200 overflow-hidden">
            <div class="flex items-center justify-between px-4 py-2.5 bg-gray-50 border-b border-gray-200">
              <div class="flex items-center gap-2">
                <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#6B7280" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><path d="M6 6h.01M6 18h.01"/>
                </svg>
                <span class="text-xs font-semibold text-gray-600">Private key → auto-injected into your instances by Plati</span>
              </div>
              <button onclick={() => copy(generatedKey!.private_key, 'Private key')} class="text-xs text-gray-500 hover:text-gray-700 font-medium">Copy</button>
            </div>
            <textarea readonly rows="5" class="w-full px-4 py-3 font-mono text-xs bg-white text-gray-600 resize-none outline-none">{generatedKey.private_key}</textarea>
          </div>
        </div>

        <div class="flex justify-end mt-5 pt-4 border-t border-gray-100">
          <button onclick={() => generatedKey = null} class="btn-primary">Done</button>
        </div>
      </div>
    </div>
  </div>
{/if}
