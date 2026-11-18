<script lang="ts">
  import { browser } from '$app/environment';
  import ApiKeysSection from '$lib/components/ApiKeysSection.svelte';
  import { users } from '$lib/api';
  import type { McpInfo } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  // The frontend proxies /mcp to the backend, so the endpoint is this very origin:
  // reached through the tailnet URL, the page shows the tailnet URL.
  let origin = $state('');
  let endpoint = $derived(origin ? `${origin}/mcp` : '');
  let localOnly = $derived(/^https?:\/\/(localhost|127\.|\[::1\])/.test(origin));

  let info: McpInfo | null = $state(null);
  // A key regenerated on this page is dropped into the snippets so they paste as-is.
  let key: string | null = $state(null);
  let keyText = $derived(key ?? '<your API key>');
  let client: 'claude' | 'json' | 'curl' = $state('claude');

  let claudeCommand = $derived(
    `claude mcp add --transport http plati ${endpoint} --header "Authorization: Bearer ${keyText}"`
  );
  let jsonConfig = $derived(JSON.stringify({
    mcpServers: { plati: { type: 'http', url: endpoint, headers: { Authorization: `Bearer ${keyText}` } } }
  }, null, 2));
  let curlCommand = $derived(
    `curl -s ${endpoint} \\\n  -H "Authorization: Bearer ${keyText}" \\\n  -H "Content-Type: application/json" \\\n` +
    `  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_instances","arguments":{}}}'`
  );
  let snippet = $derived(client === 'claude' ? claudeCommand : client === 'json' ? jsonConfig : curlCommand);

  let creationOff = $derived.by(() => {
    const q = (info as McpInfo | null)?.quota;
    return !!q && (q.max_instances <= 0 || q.max_creations_per_hour <= 0);
  });

  if (browser) {
    origin = window.location.origin;
    users.mcpInfo().then((i) => (info = i)).catch((e) => addNotification('error', e.message));
  }

  function copy(text: string, what: string) {
    navigator.clipboard.writeText(text);
    addNotification('success', `${what} copied`);
  }
</script>

<svelte:head><title>Agents (MCP) — Plati</title></svelte:head>

<div class="space-y-8 max-w-4xl">
  <div>
    <h1 class="text-2xl font-extrabold tracking-tight text-gray-900">Agents (MCP)</h1>
    <p class="text-sm text-gray-500 mt-1 leading-relaxed">
      Plati is an MCP server. An AI agent or your own code can create machines, read their status and links,
      start, stop or restart them and change their settings — as you, on your own machines only.
    </p>
  </div>

  <!-- ─── 1. Endpoint ───────────────────────────────────────────── -->
  <section class="card-static p-6 space-y-3">
    <h2 class="text-base font-bold text-gray-900"><span class="text-primary">1.</span> Endpoint</h2>
    <div class="flex items-center gap-2">
      <code class="text-sm bg-gray-100 px-3 py-2 rounded font-mono flex-1 truncate" data-testid="mcp-endpoint">{endpoint}</code>
      <button onclick={() => copy(endpoint, 'Endpoint')} class="btn-primary btn-sm shrink-0">Copy</button>
    </div>
    {#if localOnly}
      <p class="text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded p-2">
        You opened Plati through a local address: this URL only works on this machine.
        Open Plati through its tailnet URL to get the address other machines can reach.
      </p>
    {:else}
      <p class="text-xs text-gray-400">Streamable HTTP transport. Reachable from any machine on the tailnet.</p>
    {/if}
  </section>

  <!-- ─── 2. API key ────────────────────────────────────────────── -->
  <section class="space-y-3">
    <h2 class="text-base font-bold text-gray-900"><span class="text-primary">2.</span> API key</h2>
    <p class="text-sm text-gray-500">
      The MCP endpoint authenticates with your API key only — your browser session does not count.
      Regenerate it here and it is filled into the snippets below.
    </p>
    <ApiKeysSection onReveal={(k) => (key = k)} />
  </section>

  <!-- ─── 3. Client ─────────────────────────────────────────────── -->
  <section class="card-static p-6 space-y-3">
    <h2 class="text-base font-bold text-gray-900"><span class="text-primary">3.</span> Connect your client</h2>
    <div class="flex gap-4 border-b text-sm">
      <button onclick={() => (client = 'claude')} class="pb-2 px-1 {client === 'claude' ? 'border-b-2 border-primary text-primary' : 'text-gray-500'}">Claude Code</button>
      <button onclick={() => (client = 'json')} class="pb-2 px-1 {client === 'json' ? 'border-b-2 border-primary text-primary' : 'text-gray-500'}">JSON config</button>
      <button onclick={() => (client = 'curl')} class="pb-2 px-1 {client === 'curl' ? 'border-b-2 border-primary text-primary' : 'text-gray-500'}">curl (test)</button>
    </div>
    <div class="flex items-start gap-2">
      <pre class="text-xs bg-gray-900 text-gray-100 px-3 py-2.5 rounded font-mono flex-1 overflow-x-auto whitespace-pre-wrap break-all">{snippet}</pre>
      <button onclick={() => copy(snippet, 'Snippet')} class="btn-outline btn-sm shrink-0">Copy</button>
    </div>
    {#if key}
      <p class="text-xs text-amber-700">These snippets now contain your key: treat them like a password.</p>
    {:else if client === 'json'}
      <p class="text-xs text-gray-400">For MCP clients configured by file (Cursor, Claude Desktop through a proxy, your own code…).</p>
    {/if}
  </section>

  <!-- ─── Limits ────────────────────────────────────────────────── -->
  {#if info}
    <section class="card-static p-6 space-y-2">
      <h2 class="text-base font-bold text-gray-900">Limits</h2>
      {#if creationOff}
        <p class="text-sm text-gray-600">Creating machines through MCP is turned off by the administrator. Everything else works.</p>
      {:else}
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div class="bg-gray-50 border rounded-lg px-4 py-3">
            <p class="text-xs text-gray-500">Machines created through MCP</p>
            <p class="text-xl font-bold text-gray-900">{info.quota.instances} <span class="text-sm font-normal text-gray-400">/ {info.quota.max_instances} at a time</span></p>
          </div>
          <div class="bg-gray-50 border rounded-lg px-4 py-3">
            <p class="text-xs text-gray-500">Creations in the last hour</p>
            <p class="text-xl font-bold text-gray-900">{info.quota.creations_last_hour} <span class="text-sm font-normal text-gray-400">/ {info.quota.max_creations_per_hour}</span></p>
            {#if info.quota.next_creation_possible_at}
              <p class="text-xs text-amber-700 mt-1">Next creation possible at {new Date(info.quota.next_creation_possible_at).toLocaleTimeString()}</p>
            {/if}
          </div>
        </div>
        <p class="text-xs text-gray-400">
          Machines you create in this web UI do not count. Deleting one frees a slot, but not the hourly count.
        </p>
      {/if}
      <p class="text-xs text-gray-400">Deleting and rebuilding machines is not available through MCP — do it here, in the web UI.</p>
    </section>

    <!-- ─── Tools ───────────────────────────────────────────────── -->
    <section class="card-static p-6">
      <h2 class="text-base font-bold text-gray-900 mb-3">Tools ({info.tools.length})</h2>
      <ul class="divide-y">
        {#each info.tools as tool}
          <li class="py-2.5 flex items-start gap-3">
            <code class="text-xs font-mono text-primary-dark bg-primary-50 px-1.5 py-0.5 rounded shrink-0 mt-0.5">{tool.name}</code>
            <span class="text-sm text-gray-600 flex-1">{tool.description}</span>
            {#if tool.read_only}
              <span class="text-xs text-gray-400 shrink-0 mt-0.5">read-only</span>
            {/if}
          </li>
        {/each}
      </ul>
    </section>
  {/if}
</div>
