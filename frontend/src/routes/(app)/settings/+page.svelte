<script lang="ts">
  import SSHKeyManager from '$lib/components/SSHKeyManager.svelte';
  import { browser } from '$app/environment';
  import { users } from '$lib/api';
  import type { Secret } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let secrets: Secret[] = $state([]);
  let newSecretName = $state('');
  let newSecretValue = $state('');

  async function loadSecrets() {
    secrets = await users.listSecrets();
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

  if (browser) { loadSecrets(); }
</script>

<div class="space-y-8">
  <h1 class="text-2xl font-bold">Settings</h1>

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
      <button onclick={addSecret} class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700">
        Add Secret
      </button>
    </div>
  </div>
</div>
