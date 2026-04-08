<script lang="ts">
  import { currentUser } from '$lib/stores/auth';
  import { auth } from '$lib/api';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';

  let menuOpen = $state(false);

  async function logout() {
    await auth.logout();
    $currentUser = null;
    goto('/login');
  }

  function closeMenu() {
    menuOpen = false;
  }

  function isActive(path: string): boolean {
    return $page.url.pathname.startsWith(path);
  }
</script>

<!-- Desktop navbar -->
<nav class="bg-white shadow-sm border-b">
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
    <div class="flex justify-between h-16">
      <div class="flex items-center space-x-8">
        <a href="/dashboard" class="text-xl font-bold text-primary">Plati</a>
        {#if $currentUser}
          <div class="hidden md:flex items-center space-x-8">
            <a href="/dashboard" class="text-gray-600 hover:text-gray-900">Dashboard</a>
            <a href="/instances/new" class="text-gray-600 hover:text-gray-900">New Instance</a>
            <a href="/disks" class="text-gray-600 hover:text-gray-900">Disks</a>
            <a href="/settings" class="text-gray-600 hover:text-gray-900">Settings</a>
            <a href="/docs" class="text-gray-600 hover:text-gray-900">Docs</a>
            {#if $currentUser.role === 'admin'}
              <a href="/admin" class="text-gray-600 hover:text-gray-900">Admin</a>
            {/if}
          </div>
        {/if}
      </div>
      <div class="flex items-center">
        {#if $currentUser}
          <span class="hidden md:inline text-sm text-gray-500 mr-4">{$currentUser.email}</span>
          <button onclick={logout} class="hidden md:inline text-sm text-red-600 hover:text-red-800">Logout</button>
          <!-- Hamburger button -->
          <button
            onclick={() => menuOpen = !menuOpen}
            class="md:hidden p-2 rounded-md text-gray-600 hover:text-gray-900 hover:bg-gray-100"
            aria-label="Toggle menu"
          >
            <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              {#if menuOpen}
                <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
              {:else}
                <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16M4 18h16" />
              {/if}
            </svg>
          </button>
        {/if}
      </div>
    </div>
  </div>
</nav>

<!-- Mobile side menu overlay -->
{#if menuOpen}
  <!-- Backdrop -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 bg-black/30 z-40 md:hidden"
    onclick={closeMenu}
    onkeydown={(e) => e.key === 'Escape' && closeMenu()}
  ></div>

  <!-- Side panel -->
  <div class="fixed top-0 right-0 h-full w-64 bg-white shadow-lg z-50 md:hidden flex flex-col">
    <!-- Header -->
    <div class="flex items-center justify-between h-16 px-4 border-b">
      <span class="text-xl font-bold text-primary">Plati</span>
      <button
        onclick={closeMenu}
        class="p-2 rounded-md text-gray-600 hover:text-gray-900 hover:bg-gray-100"
        aria-label="Close menu"
      >
        <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
    </div>

    {#if $currentUser}
      <!-- Nav links -->
      <div class="flex-1 py-4 overflow-y-auto">
        <a href="/dashboard" onclick={closeMenu} class="block px-4 py-3 text-gray-700 hover:bg-gray-100 {isActive('/dashboard') ? 'bg-gray-50 text-primary font-medium' : ''}">
          Dashboard
        </a>
        <a href="/instances/new" onclick={closeMenu} class="block px-4 py-3 text-gray-700 hover:bg-gray-100 {isActive('/instances/new') ? 'bg-gray-50 text-primary font-medium' : ''}">
          New Instance
        </a>
        <a href="/disks" onclick={closeMenu} class="block px-4 py-3 text-gray-700 hover:bg-gray-100 {isActive('/disks') ? 'bg-gray-50 text-primary font-medium' : ''}">
          Disks
        </a>
        <a href="/settings" onclick={closeMenu} class="block px-4 py-3 text-gray-700 hover:bg-gray-100 {isActive('/settings') ? 'bg-gray-50 text-primary font-medium' : ''}">
          Settings
        </a>
        <a href="/docs" onclick={closeMenu} class="block px-4 py-3 text-gray-700 hover:bg-gray-100 {isActive('/docs') ? 'bg-gray-50 text-primary font-medium' : ''}">
          Docs
        </a>
        {#if $currentUser.role === 'admin'}
          <a href="/admin" onclick={closeMenu} class="block px-4 py-3 text-gray-700 hover:bg-gray-100 {isActive('/admin') ? 'bg-gray-50 text-primary font-medium' : ''}">
            Admin
          </a>
        {/if}
      </div>

      <!-- User info + logout -->
      <div class="border-t px-4 py-4">
        <p class="text-sm text-gray-500 truncate mb-3">{$currentUser.email}</p>
        <button onclick={() => { closeMenu(); logout(); }} class="text-sm text-red-600 hover:text-red-800">
          Logout
        </button>
      </div>
    {/if}
  </div>
{/if}
