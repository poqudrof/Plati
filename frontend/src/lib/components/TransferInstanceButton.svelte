<script lang="ts">
  import { admin } from '$lib/api';
  import type { User } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let { instanceId, currentUserId, users, disabled = false, onDone = () => {} }: {
    instanceId: number;
    /** The instance's current owner — offering it as a target would be a no-op. */
    currentUserId: number;
    /** Accounts the workspace can be handed to. Admin-only, so never empty here. */
    users: User[];
    disabled?: boolean;
    onDone?: () => void;
  } = $props();

  let picking = $state(false);
  let busy = $state(false);
  let targetUserId = $state(0);
  // Both default to the full hand-over, like the API: the container follows the new
  // owner and the previous one loses their SSH access.
  let renameContainer = $state(true);
  let revokePreviousKeys = $state(true);

  const candidates = $derived(users.filter(u => u.id !== currentUserId));

  async function run() {
    busy = true;
    try {
      const res = await admin.instances.transfer(instanceId, targetUserId, {
        keep_container_name: !renameContainer,
        keep_previous_keys: !revokePreviousKeys
      });
      addNotification('success', `"${res.name}" now belongs to ${res.new_owner_email}`);
      // The warnings are the part an admin has to act on — a rebuild for the new
      // owner's secrets above all — so each one gets its own line.
      for (const w of res.warnings ?? []) addNotification('info', w);
      if (res.container_rename_error) {
        addNotification('error', `container not renamed: ${res.container_rename_error}`);
      }
      picking = false;
      onDone();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      busy = false;
    }
  }
</script>

{#if !picking}
  <button class="btn-secondary btn-sm" onclick={() => (picking = true)} disabled={disabled || busy}>
    Transfer…
  </button>
{:else}
  <div class="flex flex-col items-end gap-2 text-left">
    <div class="flex flex-wrap items-center gap-2">
      <label class="text-xs text-gray-500" for="xfer-user-{instanceId}">Give to</label>
      <select id="xfer-user-{instanceId}" bind:value={targetUserId} class="input py-1 text-sm w-auto">
        <option value={0} disabled>Choose an account…</option>
        {#each candidates as u (u.id)}
          <option value={u.id}>{u.name || u.email}</option>
        {/each}
      </select>
    </div>
    <label class="flex items-center gap-2 text-xs text-gray-500">
      <input type="checkbox" bind:checked={renameContainer} />
      Rename the container to the new owner (restarts it)
    </label>
    <label class="flex items-center gap-2 text-xs text-gray-500">
      <input type="checkbox" bind:checked={revokePreviousKeys} />
      Remove the previous owner's SSH keys
    </label>
    <p class="text-xs text-gray-400 max-w-xs">
      The workspace itself moves — same container, same volumes, same data. Rebuild it
      afterwards to replace the previous owner's secrets and injected SSH key.
    </p>
    <div class="flex gap-2">
      <button class="btn-primary btn-sm" onclick={run} disabled={busy || targetUserId === 0}>
        {busy ? 'Transferring…' : 'Transfer'}
      </button>
      <button class="btn-secondary btn-sm" onclick={() => (picking = false)} disabled={busy}>Cancel</button>
    </div>
  </div>
{/if}
