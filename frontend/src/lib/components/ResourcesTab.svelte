<script lang="ts">
  import { browser } from '$app/environment';
  import { instances, admin } from '$lib/api';
  import type { InstanceResources } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  interface Props {
    instanceId: number;
    instanceStatus: string;
    /** Admins only. The write route is admin-gated regardless; this drives the UI. */
    canEdit: boolean;
    /** Lets the parent refresh the Incus Detail tab, which reads the same values. */
    onSaved?: () => void;
  }

  let { instanceId, instanceStatus, canEdit, onSaved }: Props = $props();

  let res = $state<InstanceResources | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let error = $state('');

  // Form state, kept separate from `res` so a failed save can fall back to the server's
  // view rather than leaving the inputs showing a value that was refused.
  let cpuField = $state('');
  let memoryField = $state('');

  $effect(() => {
    if (browser && instanceId) load();
  });

  async function load() {
    loading = true;
    error = '';
    try {
      res = await instances.resources(instanceId);
      cpuField = res.effective_cpu;
      memoryField = res.effective_memory;
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function save() {
    saving = true;
    try {
      // An empty field means "inherit the template", so a value equal to the template's
      // is sent as an empty override rather than pinning the same number twice.
      res = await admin.instances.updateResources(instanceId, {
        limits_cpu: cpuField === res?.template_cpu ? '' : cpuField.trim(),
        limits_memory: memoryField === res?.template_memory ? '' : memoryField.trim()
      });
      cpuField = res.effective_cpu;
      memoryField = res.effective_memory;
      if (res.warning) {
        addNotification('error', res.warning);
      } else {
        addNotification(
          'success',
          instanceStatus === 'running'
            ? 'Limits applied — no restart needed'
            : 'Limits saved — they take effect at the next start'
        );
      }
      onSaved?.();
    } catch (e: any) {
      addNotification('error', e.message);
      await load(); // the UI must not keep showing a value the server refused
    } finally {
      saving = false;
    }
  }

  async function clearOverrides() {
    cpuField = res?.template_cpu ?? '';
    memoryField = res?.template_memory ?? '';
    await save();
  }

  let dirty = $derived(
    res != null && (cpuField !== res.effective_cpu || memoryField !== res.effective_memory)
  );
  let hasOverride = $derived(res != null && (res.override_cpu !== '' || res.override_memory !== ''));

  /** The caption under a value: where it came from. */
  function origin(override: string, template: string): string {
    if (override === '') return template === '' ? 'not set by the template' : 'from the template';
    return template === ''
      ? 'set by an administrator'
      : `set by an administrator (template: ${template})`;
  }
</script>

<div class="p-6 space-y-6">
  <div>
    <h3 class="text-base font-semibold mb-1">Resources</h3>
    <p class="text-sm text-gray-500 leading-relaxed">
      The CPU and memory limits Incus enforces on this instance, and where each one comes from.
    </p>
  </div>

  {#if loading}
    <p class="text-sm text-gray-500">Loading resource limits…</p>
  {:else if error}
    <p class="text-sm text-red-600">{error}</p>
  {:else if res}
    <div class="grid gap-4 sm:grid-cols-3">
      <div class="card-static p-4">
        <p class="text-xs uppercase tracking-wide text-gray-400 mb-1">CPU</p>
        <p class="text-lg font-medium font-mono">{res.effective_cpu || 'unlimited'}</p>
        <p class="text-xs text-gray-400 mt-1">{origin(res.override_cpu, res.template_cpu)}</p>
      </div>
      <div class="card-static p-4">
        <p class="text-xs uppercase tracking-wide text-gray-400 mb-1">Memory</p>
        <p class="text-lg font-medium font-mono">{res.effective_memory || 'unlimited'}</p>
        <p class="text-xs text-gray-400 mt-1">{origin(res.override_memory, res.template_memory)}</p>
      </div>
      <div class="card-static p-4">
        <p class="text-xs uppercase tracking-wide text-gray-400 mb-1">Disk</p>
        <p class="text-lg font-medium font-mono">{res.template_disk || '—'}</p>
        <p class="text-xs text-gray-400 mt-1">persistent volume, read-only</p>
      </div>
    </div>

    {#if canEdit}
      <div class="card-static p-6 space-y-5">
        <div>
          <h4 class="text-sm font-semibold text-gray-900">Change the limits</h4>
          <p class="text-sm text-gray-500 leading-relaxed mt-1">
            {#if instanceStatus === 'running'}
              CPU and memory apply immediately — no restart needed.
            {:else}
              The instance is stopped, so new limits take effect the next time it starts.
            {/if}
            They are stored on the instance, so a rebuild keeps them.
          </p>
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label for="res-cpu" class="block text-sm font-medium text-gray-700 mb-1">CPU</label>
            <input
              id="res-cpu"
              class="input font-mono"
              bind:value={cpuField}
              placeholder={res.template_cpu || '2'}
              disabled={saving}
            />
            <p class="text-xs text-gray-400 mt-1">
              A core count (<code>2</code>), a pinned set (<code>0-3</code>) or a share
              (<code>50%</code>). Leave empty for no limit.
            </p>
          </div>
          <div>
            <label for="res-memory" class="block text-sm font-medium text-gray-700 mb-1">Memory</label>
            <input
              id="res-memory"
              class="input font-mono"
              bind:value={memoryField}
              placeholder={res.template_memory || '4GB'}
              disabled={saving}
            />
            <p class="text-xs text-gray-400 mt-1">
              A size with its unit (<code>4GB</code>, <code>512MiB</code>) or a share of the host
              (<code>25%</code>).
            </p>
          </div>
        </div>

        <div class="flex items-center gap-3 border-t pt-4">
          <button class="btn-primary btn-sm" onclick={save} disabled={saving || !dirty}>
            {saving ? 'Applying…' : 'Apply'}
          </button>
          {#if hasOverride}
            <button class="btn-outline btn-sm" onclick={clearOverrides} disabled={saving}>
              Revert to the template
            </button>
          {/if}
        </div>
      </div>
    {:else}
      <div class="card-static p-6">
        <p class="text-sm text-gray-500">
          Contact an administrator to change these limits.
        </p>
      </div>
    {/if}

    <!-- Being straight about what "disk" does and does not cover here: two of the three
         things that word can mean are not managed by Plati at all. -->
    <div class="p-4 bg-secondary-50 rounded-md">
      <p class="text-sm font-medium text-gray-900 mb-1">About disk size</p>
      <p class="text-sm text-gray-600 leading-relaxed">
        The persistent volume is not resizable from Plati yet — it is set when the instance is
        created, from the template. The container's root filesystem is not size-limited at all,
        so a runaway process there fills the host's storage pool rather than hitting a quota.
      </p>
    </div>
  {/if}
</div>
