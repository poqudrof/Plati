<script lang="ts">
  import { setup } from '$lib/api';
  import { goto } from '$app/navigation';
  import { addNotification } from '$lib/stores/notifications';
  import type { SetupRequest } from '$lib/api/types';

  // Steps: 1=admin, 2=entra, 3=server, 4=review, 5=done
  let step = $state(1);
  let submitting = $state(false);

  // Step 1: Admin
  let adminPassword = $state('');
  let adminConfirm = $state('');

  // Step 2: Entra (optional)
  let entraEnabled = $state(false);
  let entraClientId = $state('');
  let entraClientSecret = $state('');
  let entraTenantId = $state('');

  // Step 3: Server (optional) — pre-filled for local Incus
  let serverEnabled = $state(true);
  let serverName = $state('local');
  let serverEndpoint = $state('https://127.0.0.1:8443');
  let serverTlsCert = $state('');
  let serverTlsKey = $state('');
  let serverMaxInstances = $state(50);

  // Result
  let needsRestart = $state(false);

  // Check if already set up
  $effect(() => {
    setup.status().then(({ setup_completed }) => {
      if (setup_completed) goto('/login');
    });
  });

  // Validation
  let passwordError = $derived(
    adminPassword.length > 0 && adminPassword.length < 8
      ? 'Password must be at least 8 characters'
      : adminPassword.length > 0 && adminConfirm.length > 0 && adminPassword !== adminConfirm
        ? 'Passwords do not match'
        : ''
  );

  let step1Valid = $derived(
    adminPassword.length >= 8 && adminPassword === adminConfirm
  );

  let step2Valid = $derived(
    !entraEnabled || (entraClientId !== '' && entraClientSecret !== '' && entraTenantId !== '')
  );

  let step3Valid = $derived(
    !serverEnabled || (serverName !== '' && serverEndpoint !== '')
  );

  function nextStep() {
    if (step < 4) step++;
  }

  function prevStep() {
    if (step > 1) step--;
  }

  async function completeSetup() {
    submitting = true;
    try {
      const data: SetupRequest = {
        admin_password: adminPassword
      };
      if (entraEnabled) {
        data.entra = {
          client_id: entraClientId,
          client_secret: entraClientSecret,
          tenant_id: entraTenantId
        };
      }
      if (serverEnabled) {
        data.server = {
          name: serverName,
          endpoint: serverEndpoint,
          tls_client_cert: serverTlsCert,
          tls_client_key: serverTlsKey,
          max_instances: serverMaxInstances
        };
      }
      const result = await setup.complete(data);
      needsRestart = result.needs_restart;
      step = 5;
    } catch (e: any) {
      addNotification('error', e.message || 'Setup failed');
    } finally {
      submitting = false;
    }
  }

  const steps = ['Admin', 'Entra ID', 'Server', 'Review'];
</script>

<div class="min-h-[80vh] flex items-center justify-center">
  <div class="bg-white p-8 rounded-lg shadow-sm border max-w-xl w-full">

    {#if step <= 4}
      <!-- Step indicator -->
      <div class="flex items-center justify-center mb-8">
        {#each steps as label, i}
          <div class="flex items-center">
            <div class="flex flex-col items-center">
              <div
                class="w-8 h-8 rounded-full flex items-center justify-center text-sm font-medium
                  {i + 1 === step ? 'bg-primary text-white' :
                   i + 1 < step ? 'bg-primary-50 text-primary' :
                   'bg-gray-100 text-gray-400'}"
              >
                {#if i + 1 < step}
                  <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd"/></svg>
                {:else}
                  {i + 1}
                {/if}
              </div>
              <span class="text-xs mt-1 {i + 1 <= step ? 'text-primary' : 'text-gray-400'}">{label}</span>
            </div>
            {#if i < steps.length - 1}
              <div class="w-12 h-px mx-1 mb-4 {i + 1 < step ? 'bg-primary' : 'bg-gray-200'}"></div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}

    <!-- Step 1: Admin password -->
    {#if step === 1}
      <h2 class="text-xl font-bold mb-2">Welcome to Plati</h2>
      <p class="text-gray-500 text-sm mb-6">Set up your admin account to get started.</p>

      <div class="space-y-4">
        <div>
          <label for="password" class="block text-sm font-medium text-gray-700 mb-1">Admin Password</label>
          <input
            id="password"
            type="password"
            bind:value={adminPassword}
            placeholder="Minimum 8 characters"
            class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
          />
        </div>
        <div>
          <label for="confirm" class="block text-sm font-medium text-gray-700 mb-1">Confirm Password</label>
          <input
            id="confirm"
            type="password"
            bind:value={adminConfirm}
            placeholder="Re-enter your password"
            class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
          />
        </div>
        {#if passwordError}
          <p class="text-sm text-red-600">{passwordError}</p>
        {/if}
      </div>

      <div class="mt-6 flex justify-end">
        <button
          onclick={nextStep}
          disabled={!step1Valid}
          class="px-6 py-2 bg-primary text-white rounded-lg hover:bg-primary-dark disabled:opacity-50 disabled:cursor-not-allowed"
        >
          Next
        </button>
      </div>

    <!-- Step 2: Entra ID -->
    {:else if step === 2}
      <h2 class="text-xl font-bold mb-2">Microsoft Entra ID</h2>
      <p class="text-gray-500 text-sm mb-6">
        Configure Single Sign-On with Microsoft Entra ID (Azure AD). This is optional — you can always set it up later in the config file.
      </p>

      <label class="flex items-center gap-3 mb-6 cursor-pointer">
        <input type="checkbox" bind:checked={entraEnabled} class="w-4 h-4 rounded text-primary" />
        <span class="text-sm font-medium text-gray-700">Enable Entra ID authentication</span>
      </label>

      {#if entraEnabled}
        <div class="space-y-4">
          <div>
            <label for="entra-client" class="block text-sm font-medium text-gray-700 mb-1">Client ID</label>
            <input
              id="entra-client"
              type="text"
              bind:value={entraClientId}
              placeholder="Application (client) ID"
              class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
            />
          </div>
          <div>
            <label for="entra-secret" class="block text-sm font-medium text-gray-700 mb-1">Client Secret</label>
            <input
              id="entra-secret"
              type="password"
              bind:value={entraClientSecret}
              placeholder="Client secret value"
              class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
            />
          </div>
          <div>
            <label for="entra-tenant" class="block text-sm font-medium text-gray-700 mb-1">Tenant ID</label>
            <input
              id="entra-tenant"
              type="text"
              bind:value={entraTenantId}
              placeholder="Directory (tenant) ID"
              class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
            />
          </div>
        </div>
      {/if}

      <div class="mt-6 flex justify-between">
        <button onclick={prevStep} class="px-6 py-2 text-gray-600 hover:text-gray-900">
          Back
        </button>
        <button
          onclick={nextStep}
          disabled={!step2Valid}
          class="px-6 py-2 bg-primary text-white rounded-lg hover:bg-primary-dark disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {entraEnabled ? 'Next' : 'Skip'}
        </button>
      </div>

    <!-- Step 3: Incus Server -->
    {:else if step === 3}
      <h2 class="text-xl font-bold mb-2">Incus Server</h2>
      <p class="text-gray-500 text-sm mb-6">
        Connect your first Incus server for running development instances. You can add more servers later from the admin panel config.
      </p>

      <label class="flex items-center gap-3 mb-6 cursor-pointer">
        <input type="checkbox" bind:checked={serverEnabled} class="w-4 h-4 rounded text-primary" />
        <span class="text-sm font-medium text-gray-700">Add an Incus server now</span>
      </label>

      {#if serverEnabled}
        <div class="space-y-4">
          <div>
            <label for="srv-name" class="block text-sm font-medium text-gray-700 mb-1">Server Name</label>
            <input
              id="srv-name"
              type="text"
              bind:value={serverName}
              placeholder="local"
              class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
            />
          </div>
          <div>
            <label for="srv-endpoint" class="block text-sm font-medium text-gray-700 mb-1">Endpoint</label>
            <input
              id="srv-endpoint"
              type="text"
              bind:value={serverEndpoint}
              placeholder="https://127.0.0.1:8443"
              class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
            />
          </div>
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label for="srv-cert" class="block text-sm font-medium text-gray-700 mb-1">TLS Client Cert Path</label>
              <input
                id="srv-cert"
                type="text"
                bind:value={serverTlsCert}
                placeholder="~/.config/incus/client.crt"
                class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
              />
            </div>
            <div>
              <label for="srv-key" class="block text-sm font-medium text-gray-700 mb-1">TLS Client Key Path</label>
              <input
                id="srv-key"
                type="text"
                bind:value={serverTlsKey}
                placeholder="~/.config/incus/client.key"
                class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
              />
            </div>
          </div>
          <div>
            <label for="srv-max" class="block text-sm font-medium text-gray-700 mb-1">Max Instances</label>
            <input
              id="srv-max"
              type="number"
              bind:value={serverMaxInstances}
              min="1"
              class="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:border-primary"
            />
          </div>
        </div>
      {/if}

      <div class="mt-6 flex justify-between">
        <button onclick={prevStep} class="px-6 py-2 text-gray-600 hover:text-gray-900">
          Back
        </button>
        <button
          onclick={nextStep}
          disabled={!step3Valid}
          class="px-6 py-2 bg-primary text-white rounded-lg hover:bg-primary-dark disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {serverEnabled ? 'Next' : 'Skip'}
        </button>
      </div>

    <!-- Step 4: Review -->
    {:else if step === 4}
      <h2 class="text-xl font-bold mb-2">Review Configuration</h2>
      <p class="text-gray-500 text-sm mb-6">Review your settings before completing setup.</p>

      <div class="space-y-4">
        <div class="p-4 bg-gray-50 rounded-lg">
          <h3 class="text-sm font-semibold text-gray-700 mb-2">Admin Account</h3>
          <p class="text-sm text-gray-600">Password configured</p>
        </div>

        <div class="p-4 bg-gray-50 rounded-lg">
          <h3 class="text-sm font-semibold text-gray-700 mb-2">Microsoft Entra ID</h3>
          {#if entraEnabled}
            <p class="text-sm text-gray-600">Client ID: <code class="bg-gray-200 px-1 rounded">{entraClientId}</code></p>
            <p class="text-sm text-gray-600">Tenant ID: <code class="bg-gray-200 px-1 rounded">{entraTenantId}</code></p>
          {:else}
            <p class="text-sm text-gray-400 italic">Not configured</p>
          {/if}
        </div>

        <div class="p-4 bg-gray-50 rounded-lg">
          <h3 class="text-sm font-semibold text-gray-700 mb-2">Incus Server</h3>
          {#if serverEnabled}
            <p class="text-sm text-gray-600">Name: <code class="bg-gray-200 px-1 rounded">{serverName}</code></p>
            <p class="text-sm text-gray-600">Endpoint: <code class="bg-gray-200 px-1 rounded">{serverEndpoint}</code></p>
            <p class="text-sm text-gray-600">Max instances: {serverMaxInstances}</p>
          {:else}
            <p class="text-sm text-gray-400 italic">Not configured</p>
          {/if}
        </div>

        <div class="p-4 bg-blue-50 rounded-lg border border-blue-200">
          <p class="text-sm text-blue-700">
            A new JWT secret and encryption key will be generated automatically. The config will be saved to <code class="bg-blue-100 px-1 rounded">config/plati.yaml</code>.
          </p>
        </div>
      </div>

      <div class="mt-6 flex justify-between">
        <button onclick={prevStep} class="px-6 py-2 text-gray-600 hover:text-gray-900">
          Back
        </button>
        <button
          onclick={completeSetup}
          disabled={submitting}
          class="px-6 py-2 bg-primary text-white rounded-lg hover:bg-primary-dark disabled:opacity-50"
        >
          {submitting ? 'Setting up...' : 'Complete Setup'}
        </button>
      </div>

    <!-- Step 5: Done -->
    {:else if step === 5}
      <div class="text-center">
        <div class="w-16 h-16 bg-green-100 rounded-full flex items-center justify-center mx-auto mb-4">
          <svg class="w-8 h-8 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/>
          </svg>
        </div>
        <h2 class="text-xl font-bold mb-2">Setup Complete</h2>
        <p class="text-gray-500 text-sm mb-6">Your Plati instance is ready.</p>

        {#if needsRestart}
          <div class="p-4 bg-amber-50 rounded-lg border border-amber-200 mb-6 text-left">
            <p class="text-sm font-medium text-amber-800 mb-1">Restart required</p>
            <p class="text-sm text-amber-700">
              Restart the Plati server to activate
              {#if entraEnabled && serverEnabled}
                Entra ID sign-in and the Incus server connection.
              {:else if entraEnabled}
                Entra ID sign-in.
              {:else}
                the Incus server connection.
              {/if}
            </p>
          </div>
        {/if}

        <p class="text-sm text-gray-500 mb-4">You can sign in now with your admin password.</p>

        <button
          onclick={() => { window.location.href = '/login'; }}
          class="px-6 py-2 bg-primary text-white rounded-lg hover:bg-primary-dark"
        >
          Go to Login
        </button>
      </div>
    {/if}
  </div>
</div>
