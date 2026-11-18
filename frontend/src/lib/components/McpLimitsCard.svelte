<script lang="ts">
  import { browser } from '$app/environment';
  import { admin } from '$lib/api';
  import type { McpLimits } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let limits: McpLimits | null = $state(null);
  let saving = $state(false);

  const inRange = (n: number, max: number) => Number.isInteger(n) && n >= 0 && n <= max;
  let valid = $derived.by(() => {
    const l = limits as McpLimits | null;
    return !!l && inRange(l.max_instances, 50) && inRange(l.max_creations_per_hour, 100);
  });

  if (browser) {
    admin.settings.getMcpLimits().then((l) => (limits = l)).catch((e) => addNotification('error', e.message));
  }

  async function save() {
    if (!limits || !valid) return;
    saving = true;
    try {
      limits = await admin.settings.setMcpLimits(limits);
      addNotification('success', 'MCP limits saved');
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      saving = false;
    }
  }
</script>

<div class="bg-white border rounded-lg p-4">
  <h3 class="font-medium mb-1">MCP creation limits</h3>
  <p class="text-sm text-gray-500 mb-4">
    Caps what each user — admins included — can create through the MCP endpoint (<code class="bg-gray-100 px-1 rounded">/mcp</code>).
    Machines created in the web UI do not count. The hourly limit includes machines deleted since, so deleting and
    recreating does not get around it. Set either to 0 to turn creation through MCP off.
  </p>
  {#if limits}
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
      <div>
        <label for="mcp-max-instances" class="block text-sm font-medium text-gray-700 mb-1">Machines at a time, per user</label>
        <input id="mcp-max-instances" type="number" min="0" max="50" step="1" bind:value={limits.max_instances} class="w-full px-3 py-2 border rounded text-sm" />
      </div>
      <div>
        <label for="mcp-per-hour" class="block text-sm font-medium text-gray-700 mb-1">Creations per hour, per user</label>
        <input id="mcp-per-hour" type="number" min="0" max="100" step="1" bind:value={limits.max_creations_per_hour} class="w-full px-3 py-2 border rounded text-sm" />
      </div>
    </div>
    <div class="flex justify-end">
      <button onclick={save} disabled={saving || !valid} class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm">
        {saving ? 'Saving…' : 'Save limits'}
      </button>
    </div>
  {/if}
</div>
