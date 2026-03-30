<script lang="ts">
  import { currentUser } from '$lib/stores/auth';
  import { auth } from '$lib/api';
  import { goto } from '$app/navigation';

  async function logout() {
    await auth.logout();
    $currentUser = null;
    goto('/login');
  }
</script>

<nav class="bg-white shadow-sm border-b">
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
    <div class="flex justify-between h-16">
      <div class="flex items-center space-x-8">
        <a href="/dashboard" class="text-xl font-bold text-indigo-600">Plati</a>
        {#if $currentUser}
          <a href="/dashboard" class="text-gray-600 hover:text-gray-900">Dashboard</a>
          <a href="/instances/new" class="text-gray-600 hover:text-gray-900">New Instance</a>
          <a href="/settings" class="text-gray-600 hover:text-gray-900">Settings</a>
          <a href="/docs" class="text-gray-600 hover:text-gray-900">Docs</a>
          {#if $currentUser.role === 'admin'}
            <a href="/admin" class="text-gray-600 hover:text-gray-900">Admin</a>
          {/if}
        {/if}
      </div>
      <div class="flex items-center">
        {#if $currentUser}
          <span class="text-sm text-gray-500 mr-4">{$currentUser.email}</span>
          <button onclick={logout} class="text-sm text-red-600 hover:text-red-800">Logout</button>
        {/if}
      </div>
    </div>
  </div>
</nav>
