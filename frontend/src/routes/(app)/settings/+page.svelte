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
  <h1 class="text-2xl font-bold">Settings</h1>

  <!-- Instance Preferences -->
  {#if prefs}
  <div class="bg-white rounded-lg border p-6">
    <h3 class="text-lg font-semibold mb-1">Instance Preferences</h3>
    <p class="text-sm text-gray-500 mb-4">These defaults apply when creating new instances. You can override them per-instance in the Advanced section.</p>

    <div class="space-y-6">
      <!-- SSH Key Mode -->
      <div>
        <p class="text-sm font-medium text-gray-700 mb-2">SSH Key Mode</p>
        <div class="space-y-2">
          <label class="flex items-start gap-3 cursor-pointer">
            <input type="radio" bind:group={prefs.ssh_key_mode} value="plati" class="mt-1" />
            <div>
              <span class="font-medium">Plati (recommended)</span>
              <p class="text-xs text-gray-500">The admin-managed key assigned to you is injected into each instance, enabling git/SSH operations automatically (e.g. access to org repos).</p>
            </div>
          </label>
          <label class="flex items-start gap-3 cursor-pointer">
            <input type="radio" bind:group={prefs.ssh_key_mode} value="personal" class="mt-1" />
            <div>
              <span class="font-medium">Personal</span>
              <p class="text-xs text-gray-500">Your personal SSH key (generated below) is injected into each instance. Use this if you want to authenticate with your own GitHub account.</p>
            </div>
          </label>
        </div>
      </div>

      <!-- Tailscale Mode -->
      <div>
        <p class="text-sm font-medium text-gray-700 mb-2">Tailscale Auth Key</p>
        <div class="space-y-2">
          <label class="flex items-start gap-3 cursor-pointer">
            <input type="radio" bind:group={prefs.tailscale_mode} value="plati" class="mt-1" />
            <div>
              <span class="font-medium">Platform key</span>
              <p class="text-xs text-gray-500">Use the platform-level Tailscale auth key configured by your admin. Requires admin to configure the platform key.</p>
            </div>
          </label>
          <label class="flex items-start gap-3 cursor-pointer">
            <input type="radio" bind:group={prefs.tailscale_mode} value="personal" class="mt-1" />
            <div>
              <span class="font-medium">Personal key</span>
              <p class="text-xs text-gray-500">Use your own <code class="bg-gray-100 px-1 rounded">TAILSCALE_AUTH_KEY</code> secret. Add it in Global Secrets below.</p>
            </div>
          </label>
        </div>
      </div>

      <button
        onclick={savePrefs}
        disabled={savingPrefs}
        class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50"
      >
        {savingPrefs ? 'Saving...' : 'Save Preferences'}
      </button>
    </div>
  </div>
  {/if}

  <div class="bg-white rounded-lg border p-6">
    <SSHKeyManager />
  </div>

  <div class="bg-white rounded-lg border p-6">
    <h3 class="text-lg font-semibold mb-4">Global Environment Secrets</h3>
    <p class="text-sm text-gray-500 mb-4">These secrets are encrypted and automatically injected into <strong>all</strong> your instances as environment variables. Per-instance secrets can be configured on each instance's page.</p>

    <div class="space-y-3 mb-6">
      {#each secrets as secret}
        <div class="flex justify-between items-center p-3 bg-gray-50 rounded">
          <span class="font-medium">{secret.name}</span>
          <button onclick={() => removeSecret(secret.id)} class="text-red-600 hover:text-red-800 text-sm">Remove</button>
        </div>
      {/each}
      {#if secrets.length === 0}
        <p class="text-gray-500 text-sm">No secrets configured.</p>
      {/if}
    </div>

    <div class="border-t pt-4 space-y-3">
      <input bind:value={newSecretName} placeholder="SECRET_NAME" class="w-full px-3 py-2 border rounded" />
      <input bind:value={newSecretValue} type="password" placeholder="Secret value" class="w-full px-3 py-2 border rounded" />
      <button onclick={addSecret} class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark">
        Add Secret
      </button>
    </div>
  </div>
</div>
