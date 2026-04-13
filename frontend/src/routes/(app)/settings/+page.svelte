<script lang="ts">
  import SSHKeyManager from '$lib/components/SSHKeyManager.svelte';
  import { browser } from '$app/environment';
  import { users } from '$lib/api';
  import type { Secret, UserPreferences } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let secrets: Secret[] = $state([]);
  let newSecretName = $state('');
  let newSecretValue = $state('');
  let prefs: UserPreferences | null = $state(null);
  let savingPrefs = $state(false);

  async function loadSecrets() {
    secrets = await users.listSecrets();
  }

  async function loadPrefs() {
    prefs = await users.preferences();
  }

  async function savePrefs() {
    if (!prefs) return;
    savingPrefs = true;
    try {
      prefs = await users.updatePreferences({
        ssh_key_mode: prefs.ssh_key_mode,
        tailscale_mode: prefs.tailscale_mode
      });
      addNotification('success', 'Preferences saved');
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      savingPrefs = false;
    }
  }

  async function addSecret() {
    if (!newSecretName || !newSecretValue) return;
    try {
      await users.createSecret(newSecretName, newSecretValue);
      newSecretName = '';
      newSecretValue = '';
      await loadSecrets();
      addNotification('success', 'Secret added');
    } catch (e: any) {
      addNotification('error', e.message);
    }
  }

  async function removeSecret(id: number) {
    await users.deleteSecret(id);
    await loadSecrets();
    addNotification('success', 'Secret removed');
  }

  if (browser) {
    loadSecrets();
    loadPrefs();
  }
</script>

<div class="space-y-8">
  <h1 class="text-2xl font-extrabold tracking-tight text-gray-900">Settings</h1>

  <!-- ─── SSH Keys ──────────────────────────────────────────── -->
  <section class="space-y-4">
    <div>
      <h2 class="text-lg font-bold text-gray-900">SSH Keys</h2>
      <p class="text-sm text-gray-500 leading-relaxed mt-0.5">
        Two separate key flows — one for logging into your instances, one for your instances to access git repos.
      </p>
    </div>

    <SSHKeyManager />
  </section>

  <!-- ─── Machine Key Mode ──────────────────────────────────── -->
  {#if prefs}
  <section>
    <div class="card-static p-6 space-y-5">
      <div class="flex items-start gap-3">
        <div class="bg-primary-50 rounded-xl p-2.5 shrink-0">
          <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          </svg>
        </div>
        <div>
          <h2 class="text-base font-bold text-gray-900">Machine Key Mode</h2>
          <p class="text-sm text-gray-500 leading-relaxed">Which private key gets injected into your instances for git access.</p>
        </div>
      </div>

      <div class="space-y-3">
        <!-- Plati mode -->
        <label class="flex items-start gap-3 p-4 rounded-xl border cursor-pointer transition-colors
          {prefs.ssh_key_mode === 'plati'
            ? 'border-primary bg-primary-50'
            : 'border-gray-200 bg-white hover:border-gray-300'}">
          <input type="radio" bind:group={prefs.ssh_key_mode} value="plati" class="mt-0.5 accent-primary" />
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

        <!-- Personal mode -->
        <label class="flex items-start gap-3 p-4 rounded-xl border cursor-pointer transition-colors
          {prefs.ssh_key_mode === 'personal'
            ? 'border-primary bg-primary-50'
            : 'border-gray-200 bg-white hover:border-gray-300'}">
          <input type="radio" bind:group={prefs.ssh_key_mode} value="personal" class="mt-0.5 accent-primary" />
          <div class="min-w-0">
            <span class="text-sm font-semibold text-gray-900">My machine key</span>
            <p class="text-xs text-gray-500 leading-relaxed mt-0.5">
              Your personal machine key (generated above) is injected instead.
              Use this when you want instances to authenticate as your own GitHub account.
            </p>
          </div>
        </label>
      </div>

      <!-- Tailscale mode -->
      <div class="border-t border-gray-100 pt-5 space-y-3">
        <div>
          <p class="text-sm font-semibold text-gray-900 mb-0.5">Tailscale Auth Key</p>
          <p class="text-xs text-gray-500">Which Tailscale key is injected when your instances join the tailnet.</p>
        </div>

        <label class="flex items-start gap-3 p-4 rounded-xl border cursor-pointer transition-colors
          {prefs.tailscale_mode === 'plati'
            ? 'border-primary bg-primary-50'
            : 'border-gray-200 bg-white hover:border-gray-300'}">
          <input type="radio" bind:group={prefs.tailscale_mode} value="plati" class="mt-0.5 accent-primary" />
          <div>
            <span class="text-sm font-semibold text-gray-900">Platform key</span>
            <p class="text-xs text-gray-500 leading-relaxed mt-0.5">Use the platform-level Tailscale auth key configured by your admin.</p>
          </div>
        </label>

        <label class="flex items-start gap-3 p-4 rounded-xl border cursor-pointer transition-colors
          {prefs.tailscale_mode === 'personal'
            ? 'border-primary bg-primary-50'
            : 'border-gray-200 bg-white hover:border-gray-300'}">
          <input type="radio" bind:group={prefs.tailscale_mode} value="personal" class="mt-0.5 accent-primary" />
          <div>
            <span class="text-sm font-semibold text-gray-900">Personal key</span>
            <p class="text-xs text-gray-500 leading-relaxed mt-0.5">
              Use your own <code class="bg-gray-100 px-1 rounded font-mono">TAILSCALE_AUTH_KEY</code> secret from Global Secrets below.
            </p>
          </div>
        </label>
      </div>

      <div class="border-t border-gray-100 pt-4">
        <button
          onclick={savePrefs}
          disabled={savingPrefs}
          class="btn-primary"
        >
          {savingPrefs ? 'Saving…' : 'Save Preferences'}
        </button>
      </div>
    </div>
  </section>
  {/if}

  <!-- ─── Global Secrets ────────────────────────────────────── -->
  <section>
    <div class="card-static p-6">
      <div class="flex items-start gap-3 mb-5">
        <div class="bg-secondary-50 rounded-xl p-2.5 shrink-0">
          <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#D97706" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>
          </svg>
        </div>
        <div>
          <h2 class="text-base font-bold text-gray-900">Global Environment Secrets</h2>
          <p class="text-sm text-gray-500 leading-relaxed mt-0.5">
            Encrypted secrets injected as environment variables into <strong>all</strong> your instances.
            Per-instance secrets can be set on each instance's page.
          </p>
        </div>
      </div>

      <div class="space-y-2 mb-5">
        {#each secrets as secret}
          <div class="flex justify-between items-center px-3 py-2.5 bg-gray-50 rounded-lg border border-gray-200">
            <span class="text-sm font-medium font-mono text-gray-700">{secret.name}</span>
            <button onclick={() => removeSecret(secret.id)} class="btn-danger btn-sm">Remove</button>
          </div>
        {/each}
        {#if secrets.length === 0}
          <p class="text-sm text-gray-400 py-2">No secrets configured.</p>
        {/if}
      </div>

      <div class="border-t border-gray-100 pt-4 space-y-2">
        <p class="text-xs font-medium text-gray-500 mb-2">Add a secret</p>
        <input bind:value={newSecretName} placeholder="SECRET_NAME" class="input w-full font-mono text-sm" />
        <input bind:value={newSecretValue} type="password" placeholder="Secret value" class="input w-full text-sm" />
        <button onclick={addSecret} class="btn-primary btn-sm">Add Secret</button>
      </div>
    </div>
  </section>
</div>
