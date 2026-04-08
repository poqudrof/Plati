<script lang="ts">
  import { currentUser, isLoading } from '$lib/stores/auth';
  import { goto } from '$app/navigation';

  $effect(() => {
    if (!$isLoading) {
      goto($currentUser ? '/dashboard' : '/login');
    }
  });
</script>

<div class="flex justify-center items-center h-64">
  <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
</div>
