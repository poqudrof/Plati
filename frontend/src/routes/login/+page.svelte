<script lang="ts">
  import { auth } from '$lib/api';
  import { currentUser } from '$lib/stores/auth';
  import { goto } from '$app/navigation';
  import { addNotification } from '$lib/stores/notifications';

  let password = $state('');
  let email = $state('');
  let loading = $state(false);

  async function loginAdmin() {
    loading = true;
    try {
      await auth.login(password, email || undefined);
      const user = await auth.me();
      $currentUser = user;
      goto('/dashboard');
    } catch (e: any) {
      addNotification('error', e.message || 'Login failed');
    } finally {
      loading = false;
    }
  }

  function loginEntra() {
    window.location.href = '/auth/entra';
  }
</script>

<div class="min-h-[60vh] flex items-center justify-center">
  <div class="bg-white p-8 rounded-lg shadow-sm border max-w-md w-full">
    <h1 class="text-2xl font-bold text-center mb-8">Plati</h1>

    <button
      onclick={loginEntra}
      class="w-full py-3 px-4 bg-blue-600 text-white rounded-lg hover:bg-blue-700 mb-6"
    >
      Sign in with Microsoft
    </button>

    <div class="relative mb-6">
      <div class="absolute inset-0 flex items-center">
        <div class="w-full border-t"></div>
      </div>
      <div class="relative flex justify-center text-sm">
        <span class="px-2 bg-white text-gray-500">or login with password</span>
      </div>
    </div>

    <form onsubmit={(e) => { e.preventDefault(); loginAdmin(); }} class="space-y-4">
      <input
        type="email"
        bind:value={email}
        placeholder="Email (leave blank for admin)"
        class="w-full px-4 py-2 border rounded-lg"
      />
      <input
        type="password"
        bind:value={password}
        placeholder="Password"
        class="w-full px-4 py-2 border rounded-lg"
        required
      />
      <button
        type="submit"
        disabled={loading}
        class="w-full py-2 bg-gray-800 text-white rounded-lg hover:bg-gray-900 disabled:opacity-50"
      >
        {loading ? 'Signing in...' : 'Sign in'}
      </button>
    </form>
  </div>
</div>
