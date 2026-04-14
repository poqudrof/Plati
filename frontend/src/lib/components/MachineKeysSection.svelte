<script lang="ts">
  import type { UserSSHKey, GeneratedKeyResult } from '$lib/api/types';
  import { users } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';

  let { sshKeyMode = $bindable(), onSave, saving = false }: {
    sshKeyMode: string;
    onSave: () => void;
    saving?: boolean;
  } = $props();

  let machineKeys: UserSSHKey[] = $state([]);
  let newMachineKeyName = $state('');
  let machineKeyLoading = $state(false);
  let generatedKey: GeneratedKeyResult | null = $state(null);
  let showGenerateForm = $state(false);

  async function loadMachineKeys() {
    try { machineKeys = await users.listUserKeys(); }
    catch (e: any) { addNotification('error', e.message); }
  }

  async function generateMachineKey() {
    if (!newMachineKeyName.trim()) return;
    machineKeyLoading = true;
    try {
      generatedKey = await users.generateUserKey(newMachineKeyName.trim());
      newMachineKeyName = '';
      showGenerateForm = false;
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

  function copy(text: string, label: string) {
    navigator.clipboard.writeText(text);
    addNotification('success', `${label} copied`);
  }

  loadMachineKeys();
</script>

<div class="card-static p-6 space-y-5">

  <!-- Header -->
  <div class="flex items-start gap-3">
    <div class="bg-secondary-50 rounded-xl p-2.5 shrink-0">
      <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#D97706" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="7.5" cy="15.5" r="5.5"/><path d="M21 2l-9.6 9.6M15.5 7.5 19 4M13 10l-2 2"/>
      </svg>
    </div>
    <div>
      <h2 class="text-base font-bold text-gray-900">Machine Keys</h2>
      <p class="text-sm text-gray-500 leading-relaxed">Which private key gets injected into your instances for git access.</p>
    </div>
  </div>

  <!-- Mode selector -->
  <div class="space-y-3">
    <!-- Admin-managed -->
    <label class="flex items-start gap-3 p-4 rounded-xl border cursor-pointer transition-colors
      {sshKeyMode === 'plati'
        ? 'border-primary bg-primary-50'
        : 'border-gray-200 bg-white hover:border-gray-300'}">
      <input type="radio" bind:group={sshKeyMode} value="plati" class="mt-0.5 accent-primary" />
      <div class="min-w-0">
        <div class="flex items-center gap-2 mb-0.5">
          <span class="text-sm font-semibold text-gray-900">Admin-managed key</span>
          <span class="text-xs px-1.5 py-0.5 rounded bg-primary-50 text-primary font-medium border border-primary/20">Recommended</span>
        </div>
        <p class="text-xs text-gray-500 leading-relaxed">
          The key your admin assigned to you is injected into every instance.
          Gives automatic access to org repos — no setup needed.
        </p>
      </div>
    </label>

    <!-- Personal -->
    <label class="flex items-start gap-3 p-4 rounded-xl border cursor-pointer transition-colors
      {sshKeyMode === 'personal'
        ? 'border-primary bg-primary-50'
        : 'border-gray-200 bg-white hover:border-gray-300'}">
      <input type="radio" bind:group={sshKeyMode} value="personal" class="mt-0.5 accent-primary" />
      <div class="min-w-0">
        <span class="text-sm font-semibold text-gray-900">My machine key</span>
        <p class="text-xs text-gray-500 leading-relaxed mt-0.5">
          Your personal machine key is injected instead.
          Use this when you want instances to authenticate as your own GitHub account.
        </p>
      </div>
    </label>
  </div>

  <!-- Personal mode: key list + generate -->
  {#if sshKeyMode === 'personal'}
    <div class="border-t border-gray-100 pt-4 space-y-4">

      <!-- Diagram -->
      <div class="rounded-xl bg-gray-50 border border-gray-200 p-4 flex items-center justify-between gap-3 text-sm">
        <div class="flex flex-col items-center gap-1.5 flex-1">
          <div class="bg-white border border-gray-200 rounded-lg px-3 py-2.5 flex flex-col items-center gap-1 w-full">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#6B7280" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><path d="M6 6h.01M6 18h.01"/>
            </svg>
            <span class="font-mono text-xs text-gray-500">~/.ssh/id_rsa</span>
          </div>
          <span class="text-xs text-gray-400 font-medium">Your instance</span>
        </div>

        <div class="flex flex-col items-center gap-0.5 shrink-0">
          <div class="flex items-center gap-1 text-secondary font-semibold text-xs">
            <div class="w-6 h-px bg-secondary"></div>
            <span>git</span>
            <div class="w-6 h-px bg-secondary"></div>
            <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>
          </div>
          <span class="text-xs text-gray-400">SSH</span>
        </div>

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
      <p class="text-xs text-gray-400 -mt-2 text-center">The <strong class="text-gray-500">private key</strong> is auto-injected into your instance. Add the <strong class="text-gray-500">public key</strong> to your repo as a deploy key.</p>

      <!-- Key list -->
      {#if machineKeys.length > 0}
        <div class="space-y-2">
          {#each machineKeys as key}
            <div class="flex justify-between items-center px-3 py-2.5 bg-gray-50 rounded-lg border border-gray-200">
              <div class="min-w-0 flex-1">
                <span class="text-sm font-medium text-gray-800">{key.name}</span>
                <span class="text-xs text-gray-400 ml-2">{new Date(key.created_at).toLocaleDateString()}</span>
                <p class="font-mono text-xs text-gray-400 truncate mt-0.5">{key.public_key.substring(0, 52)}…</p>
              </div>
              <div class="flex items-center gap-2 ml-3 shrink-0">
                <button onclick={() => copy(key.public_key, 'Public key')} class="btn-outline btn-sm">Copy public key</button>
                <button onclick={() => deleteMachineKey(key.id)} class="btn-danger btn-sm">Remove</button>
              </div>
            </div>
          {/each}
        </div>
      {/if}

      <!-- Generate form / trigger -->
      {#if showGenerateForm}
        <div class="space-y-2">
          <p class="text-xs font-medium text-gray-500">Generate a new keypair</p>
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
          <button onclick={() => { showGenerateForm = false; newMachineKeyName = ''; }} class="btn-outline btn-sm">Cancel</button>
        </div>
      {:else}
        <button onclick={() => showGenerateForm = true} class="btn-outline btn-sm">
          Create a personal key to add to Github
        </button>
      {/if}
    </div>
  {/if}

  <!-- Save -->
  <div class="border-t border-gray-100 pt-4">
    <button onclick={onSave} disabled={saving} class="btn-primary">
      {saving ? 'Saving…' : 'Save'}
    </button>
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
