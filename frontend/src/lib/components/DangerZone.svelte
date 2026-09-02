<script lang="ts">
  import { goto } from '$app/navigation';
  import { instances } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';

  interface Props {
    instanceId: number;
    /** Typed back by the operator to confirm — the same guard Settings uses. */
    instanceName: string;
    /** True when an admin is acting on someone else's workspace. */
    isForeign?: boolean;
    ownerEmail?: string;
    onDone?: () => void;
  }

  let { instanceId, instanceName, isForeign = false, ownerEmail = '', onDone }: Props = $props();

  // Collapsed by default. Rebuild and Delete deliberately do not sit next to Start/Stop:
  // both are destructive, and an instance is long-lived.
  let open = $state(false);
  let confirming = $state<'' | 'rebuild' | 'delete'>('');
  let typedName = $state('');
  let busy = $state(false);

  function ask(what: 'rebuild' | 'delete') {
    confirming = confirming === what ? '' : what;
    typedName = '';
  }

  async function rebuild() {
    busy = true;
    try {
      await instances.rebuild(instanceId);
      addNotification('success', 'Instance rebuilt');
      confirming = '';
      onDone?.();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      busy = false;
    }
  }

  async function remove() {
    busy = true;
    try {
      await instances.delete(instanceId);
      addNotification('success', 'Instance deleted');
      await goto('/dashboard');
    } catch (e: any) {
      addNotification('error', e.message);
      busy = false;
    }
  }
</script>

<div class="card-static p-6">
  <button
    class="flex items-center gap-2 text-sm font-semibold text-gray-900"
    onclick={() => (open = !open)}
    aria-expanded={open}
  >
    <span class="text-gray-400">{open ? '▾' : '▸'}</span>
    Danger zone
  </button>

  {#if open}
    <div class="mt-4 space-y-5">
      {#if isForeign}
        <p class="text-sm text-amber-900 bg-amber-50 border border-amber-200 rounded-md p-3">
          This is {ownerEmail || 'another user'}'s workspace. Both actions below affect their work.
        </p>
      {/if}

      <!-- ── Rebuild ── -->
      <div class="space-y-2">
        <div class="flex items-start justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-gray-900">Rebuild</p>
            <p class="text-sm text-gray-500 leading-relaxed">
              Destroys the container and recreates it from the template, then reattaches the
              persistent volumes. Anything outside those volumes — packages installed by hand,
              files under <code>/tmp</code> or <code>/opt</code> — is lost.
            </p>
          </div>
          <button class="btn-outline btn-sm shrink-0" onclick={() => ask('rebuild')} disabled={busy}>
            {confirming === 'rebuild' ? 'Cancel' : 'Rebuild'}
          </button>
        </div>
        {#if confirming === 'rebuild'}
          <div class="pt-2 border-t border-gray-200 flex gap-2">
            <input
              bind:value={typedName}
              placeholder={instanceName}
              class="input flex-1 font-mono text-sm"
              aria-label="Type the instance name to confirm the rebuild"
            />
            <button
              class="btn-primary btn-sm shrink-0 disabled:opacity-50"
              disabled={busy || typedName !== instanceName}
              onclick={rebuild}
            >
              {busy ? 'Rebuilding…' : 'Rebuild now'}
            </button>
          </div>
        {/if}
      </div>

      <!-- ── Delete ── -->
      <div class="space-y-2 border-t pt-5">
        <div class="flex items-start justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-gray-900">Delete</p>
            <p class="text-sm text-gray-500 leading-relaxed">
              Removes the instance and destroys every volume attached to it. There is no undo and
              no snapshot is taken first.
            </p>
          </div>
          <button class="btn-outline btn-sm shrink-0" onclick={() => ask('delete')} disabled={busy}>
            {confirming === 'delete' ? 'Cancel' : 'Delete'}
          </button>
        </div>
        {#if confirming === 'delete'}
          <div class="pt-2 border-t border-gray-200 space-y-2">
            <p class="text-xs text-red-600">
              Type <span class="font-mono font-semibold">{instanceName}</span> to confirm. All data
              on its volumes is destroyed.
            </p>
            <div class="flex gap-2">
              <input
                bind:value={typedName}
                placeholder={instanceName}
                class="input flex-1 font-mono text-sm"
                aria-label="Type the instance name to confirm the deletion"
              />
              <button
                class="btn-danger btn-sm shrink-0 disabled:opacity-50"
                disabled={busy || typedName !== instanceName}
                onclick={remove}
              >
                {busy ? 'Deleting…' : 'Delete permanently'}
              </button>
            </div>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>
