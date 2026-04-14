<script lang="ts">
  import '../app.css';
  import Navbar from '$lib/components/Navbar.svelte';
  import Notifications from '$lib/components/Notifications.svelte';
  import { currentUser, isLoading } from '$lib/stores/auth';
  import { auth, setup } from '$lib/api';
  import type { Snippet } from 'svelte';
  import { browser } from '$app/environment';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';

  let { children }: { children: Snippet } = $props();
  let initialized = $state(false);
  let isSetupRoute = $derived($page.url.pathname === '/setup');
  let isLandingRoute = $derived($page.url.pathname === '/landing');

  $effect(() => {
    if (browser && !initialized) {
      initialized = true;
      setup.status().then(({ setup_completed }) => {
        if (!setup_completed) {
          if (window.location.pathname !== '/setup') {
            goto('/setup');
          } else {
            isLoading.set(false);
          }
          return;
        }
        // Setup done — normal auth flow
        auth.me().then(user => {
          currentUser.set(user);
        }).catch(() => {
          currentUser.set(null);
        }).finally(() => {
          isLoading.set(false);
        });
      }).catch(() => {
        // Status check failed — assume setup done, proceed normally
        auth.me().then(user => {
          currentUser.set(user);
        }).catch(() => {
          currentUser.set(null);
        }).finally(() => {
          isLoading.set(false);
        });
      });
    }
  });
</script>

<Notifications />
{#if !isSetupRoute && !isLandingRoute}
  <Navbar />
{/if}
<main class={isLandingRoute ? '' : 'max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8'}>
  {@render children()}
</main>
