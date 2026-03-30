<script lang="ts">
  import { notifications, removeNotification } from '$lib/stores/notifications';
</script>

<div class="fixed top-4 right-4 z-50 space-y-2">
  {#each $notifications as notif (notif.id)}
    <div
      class="px-4 py-3 rounded-lg shadow-lg text-white max-w-sm"
      class:bg-green-600={notif.type === 'success'}
      class:bg-red-600={notif.type === 'error'}
      class:bg-blue-600={notif.type === 'info'}
    >
      <div class="flex justify-between items-center">
        <span class="text-sm">{notif.message}</span>
        <button onclick={() => removeNotification(notif.id)} class="ml-2 text-white/80 hover:text-white">&times;</button>
      </div>
    </div>
  {/each}
</div>
