<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { currentUser } from '$lib/stores/auth';
  import { auth } from '$lib/api';

  onMount(async () => {
    try {
      const user = await auth.me();
      $currentUser = user;
      goto('/dashboard');
    } catch {
      goto('/login');
    }
  });
</script>

<div class="flex justify-center items-center h-64">
  <p class="text-gray-500">Completing sign-in...</p>
</div>
