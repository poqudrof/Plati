<script lang="ts">
  import { instances, admin } from '$lib/api';
  import type { User } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let { instanceId, users = [], defaultUserId = 0, disabled = false, onDone = () => {} }: {
    instanceId: number;
    /** Accounts the copy can be assigned to. Empty (non-admin) means "copy to myself". */
    users?: User[];
    /** Account pre-selected in the picker — usually the source instance's owner. */
    defaultUserId?: number;
    disabled?: boolean;
    onDone?: () => void;
  } = $props();

  let picking = $state(false);
  let busy = $state(false);
  let targetUserId = $state(defaultUserId);

  async function run() {
    busy = true;
    try {
      // The admin route copies any user's instance and assigns the copy;
      // the plain route only copies one of your own, to yourself.
      const copy = users.length > 0
        ? await admin.instances.duplicate(instanceId, targetUserId)
        : await instances.duplicate(instanceId);
      addNotification('success', `Duplicating as "${copy.name}" — this may take a minute`);
      picking = false;
      onDone();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      busy = false;
    }
  }
</script>

{#if users.length === 0}
  <button class="btn-secondary btn-sm" onclick={run} disabled={disabled || busy}>
    {busy ? 'Duplicating…' : 'Duplicate'}
  </button>
{:else if !picking}
  <button class="btn-secondary btn-sm" onclick={() => (picking = true)} disabled={disabled || busy}>
    Duplicate…
  </button>
{:else}
  <div class="flex flex-wrap items-center gap-2">
    <label class="text-xs text-gray-500" for="dup-user-{instanceId}">Assign to</label>
    <select id="dup-user-{instanceId}" bind:value={targetUserId} class="input py-1 text-sm w-auto">
      {#each users as u (u.id)}
        <option value={u.id}>{u.name || u.email}</option>
      {/each}
    </select>
    <button class="btn-primary btn-sm" onclick={run} disabled={busy}>
      {busy ? 'Copying…' : 'Copy'}
    </button>
    <button class="btn-secondary btn-sm" onclick={() => (picking = false)} disabled={busy}>Cancel</button>
  </div>
{/if}
