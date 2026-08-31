<script lang="ts">
  import { browser } from '$app/environment';
  import { page } from '$app/stores';
  import { admin, templates as templatesApi, instances as instancesApi } from '$lib/api';
  import Terminal from '$lib/components/Terminal.svelte';
  import type { Template, Instance, MixinInfo, TailscaleServeResult, ProfileCheck, GitRepo } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  // ── State ──────────────────────────────────────────────────────────────────

  let templateId: number = $derived(parseInt($page.params.id ?? '0'));

  let template = $state<Template | null>(null);
  let editingTemplate = $state<Template | null>(null);
  let debugInstance: Instance | null = $state(null);
  let availableMixins: MixinInfo[] = $state([]);
  let loading = $state(true);
  let profileChecks = $state<ProfileCheck[]>([]);
  let availableRepos = $state<GitRepo[]>([]);

  let activeSection: 'instance' | 'runner' | 'editor' = $state('instance');

  // Creation log viewer
  type LogStep = { kind: 'step'; n: number; total: number; label: string; outputs: string[]; warning: string; expanded: boolean };
  type LogLine = { kind: 'log'; text: string };
  let logItems: (LogStep | LogLine)[] = $state([]);
  let creationLogEl: HTMLElement | undefined = $state();

  // Exec / step runner
  let execOutput = $state('');
  let execRunning = $state(false);

  // Template editor
  let yamlContent = $state('');
  let yamlLoading = $state(false);
  let duplicateName = $state('');
  let duplicateSlug = $state('');

  let tsServeStatus = $state<TailscaleServeResult | null>(null);
  let tsServeLoading = $state(false);

  // Persistence dirs editing
  type PersistenceDir = { path: string; size: string; pool: string };
  let persistenceDirs: PersistenceDir[] = $state([]);

  // Derived
  let parsedFirstInitCmds: string[] = $derived(
    (() => { try { return JSON.parse(editingTemplate?.first_init_commands ?? '[]'); } catch { return []; } })()
  );
  let parsedIncludes: string[] = $derived(
    (() => { try { return JSON.parse(editingTemplate?.includes ?? '[]'); } catch { return []; } })()
  );
  let hasVSCode = $derived(parsedIncludes.includes('openvscode-server'));
  let parsedRepos: { name: string; dest: string }[] = $derived(
    (() => { try { return JSON.parse(editingTemplate?.repos ?? '[]'); } catch { return []; } })()
  );
  let repoNames = $derived(new Set(availableRepos.map(r => r.name)));
  let missingRepos = $derived(parsedRepos.filter(r => !repoNames.has(r.name)));

  // ── Load ───────────────────────────────────────────────────────────────────

  async function load() {
    loading = true;
    try {
      const [tmpl, mixins, checks, repos] = await Promise.all([
        templatesApi.get(templateId),
        admin.templates.listMixins(),
        admin.templates.checkProfiles(templateId),
        admin.repos.list()
      ]);
      template = tmpl;
      editingTemplate = { ...tmpl };
      availableMixins = mixins;
      profileChecks = checks ?? [];
      availableRepos = repos ?? [];
      try { persistenceDirs = JSON.parse(tmpl.persistence_dirs || '[]'); } catch { persistenceDirs = []; }
      try {
        debugInstance = await admin.templates.getDebugInstance(templateId);
      } catch {
        debugInstance = null;
      }
    } catch (e: any) {
      addNotification('error', 'Failed to load: ' + e.message);
    } finally {
      loading = false;
    }
  }

  // ── Log viewer ─────────────────────────────────────────────────────────────

  function parseLogLine(raw: string): void {
    const line = raw.replace(/\r?\n$/, '').replace(/\x1b\[[0-9;]*m/g, '').replace(/\r/g, '');
    if (!line) return;
    if (line.startsWith('STEP:')) {
      const rest = line.slice(5);
      const slashIdx = rest.indexOf('/');
      const colonIdx = rest.indexOf(':', slashIdx + 1);
      const n = parseInt(rest.slice(0, slashIdx));
      const total = parseInt(rest.slice(slashIdx + 1, colonIdx));
      const label = rest.slice(colonIdx + 1);
      logItems.push({ kind: 'step', n, total, label, outputs: [], warning: '', expanded: false });
    } else if (line.startsWith('OUT:')) {
      const text = line.slice(4);
      const last = logItems[logItems.length - 1];
      if (last?.kind === 'step') last.outputs.push(text);
    } else if (line.startsWith('WARN:')) {
      const text = line.slice(5);
      const last = logItems[logItems.length - 1];
      if (last?.kind === 'step') {
        last.warning = text;
      } else {
        logItems.push({ kind: 'log', text: '⚠ ' + text });
      }
    } else {
      logItems.push({ kind: 'log', text: line });
    }
    setTimeout(() => { if (creationLogEl) creationLogEl.scrollTop = creationLogEl.scrollHeight; }, 0);
  }

  // Stream creation log while instance is being created.
  $effect(() => {
    if (!browser || debugInstance?.status !== 'creating') return;
    logItems = [];
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}/api/v1/instances/${debugInstance.id}/creation-stream`);
    ws.onmessage = (e) => parseLogLine(e.data);

    const timer = setInterval(async () => {
      try {
        const updated = await instancesApi.get(debugInstance!.id);
        if (updated.status !== 'creating') {
          clearInterval(timer);
          debugInstance = updated;
        }
      } catch { /* ignore */ }
    }, 3000);

    return () => { ws.close(); clearInterval(timer); };
  });

  $effect(() => {
    if (!browser || debugInstance?.status !== 'running') return;
    loadTsServeStatus();
  });

  // ── Instance tab actions ───────────────────────────────────────────────────

  async function createDebugInstance() {
    try {
      debugInstance = await admin.templates.debugCreate(templateId);
      logItems = [];
      addNotification('success', 'Debug instance creation started');
    } catch (e: any) {
      addNotification('error', e.message);
    }
  }

  async function startInstance() {
    if (!debugInstance) return;
    try {
      await instancesApi.start(debugInstance.id);
      debugInstance = await instancesApi.get(debugInstance.id);
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function stopInstance() {
    if (!debugInstance) return;
    try {
      await instancesApi.stop(debugInstance.id);
      debugInstance = await instancesApi.get(debugInstance.id);
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function deleteInstance() {
    if (!debugInstance || !confirm('Delete this debug instance?')) return;
    try {
      await instancesApi.delete(debugInstance.id);
      debugInstance = null;
      logItems = [];
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function loadTsServeStatus() {
    if (!browser || !debugInstance || debugInstance.status !== 'running') return;
    try {
      tsServeStatus = await instancesApi.tailscaleServeStatus(debugInstance.id);
    } catch { tsServeStatus = null; }
  }

  async function startVsCodeServe() {
    if (!debugInstance) return;
    tsServeLoading = true;
    try {
      tsServeStatus = await instancesApi.tailscaleServe(debugInstance.id, 3463);
      addNotification('success', 'Tailscale Serve started on port 3463');
    } catch (e: any) { addNotification('error', e.message); }
    finally { tsServeLoading = false; }
  }

  async function stopTsServe() {
    if (!debugInstance) return;
    tsServeLoading = true;
    try {
      await instancesApi.tailscaleServeOff(debugInstance.id);
      tsServeStatus = { status: 'off', url: '', port: 0 };
      addNotification('success', 'Tailscale Serve stopped');
    } catch (e: any) { addNotification('error', e.message); }
    finally { tsServeLoading = false; }
  }

  // ── Step runner ────────────────────────────────────────────────────────────

  function appendExecOutput(text: string) {
    execOutput += (execOutput ? '\n' : '') + text;
  }

  async function runReapplySetup() {
    if (!debugInstance) return;
    execRunning = true;
    try {
      const result = await admin.instances.reapplySetup(debugInstance.id);
      appendExecOutput('$ [reapply SSH keys & secrets]\n' + (result.output || '(no output)') + (result.error ? '\nError: ' + result.error : ''));
    } catch (e: any) {
      appendExecOutput('Error: ' + e.message);
    } finally { execRunning = false; }
  }

  async function runExec(command: string) {
    if (!debugInstance) return;
    execRunning = true;
    try {
      const result = await admin.instances.exec(debugInstance.id, command);
      const header = `$ ${command.length > 60 ? command.slice(0, 57) + '...' : command}`;
      appendExecOutput(header + '\n' + (result.output || '(no output)') + (result.error ? '\nError: ' + result.error : ''));
    } catch (e: any) {
      appendExecOutput('Error: ' + e.message);
    } finally { execRunning = false; }
  }

  async function runMixinAll(mixin: MixinInfo) {
    for (const cmd of mixin.commands) {
      await runExec(cmd);
    }
  }

  // ── Template editor ────────────────────────────────────────────────────────

  async function saveTemplate() {
    if (!editingTemplate) return;
    editingTemplate.persistence_dirs = JSON.stringify(persistenceDirs);
    try {
      await admin.templates.update(templateId, editingTemplate);
      template = { ...editingTemplate };
      addNotification('success', 'Template saved');
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function exportYAML() {
    yamlLoading = true;
    try {
      const res = await admin.templates.exportYAML(templateId);
      yamlContent = res.yaml;
    } catch (e: any) { addNotification('error', e.message); } finally { yamlLoading = false; }
  }

  async function updateFromYAML() {
    if (!yamlContent.trim()) return;
    yamlLoading = true;
    try {
      const updated = await admin.templates.updateFromYAML(templateId, yamlContent);
      template = updated;
      editingTemplate = { ...updated };
      try { persistenceDirs = JSON.parse(updated.persistence_dirs || '[]'); } catch { persistenceDirs = []; }
      addNotification('success', 'Template updated from YAML');
    } catch (e: any) { addNotification('error', e.message); } finally { yamlLoading = false; }
  }

  async function duplicateTemplate() {
    if (!duplicateName.trim() || !duplicateSlug.trim()) {
      addNotification('error', 'Name and slug are required');
      return;
    }
    try {
      await admin.templates.duplicate(templateId, duplicateName, duplicateSlug);
      duplicateName = '';
      duplicateSlug = '';
      addNotification('success', 'Template duplicated');
    } catch (e: any) { addNotification('error', e.message); }
  }

  function toggleMixin(name: string) {
    if (!editingTemplate) return;
    const current: string[] = (() => { try { return JSON.parse(editingTemplate.includes || '[]'); } catch { return []; } })();
    const idx = current.indexOf(name);
    const next = idx === -1 ? [...current, name] : current.filter(n => n !== name);
    editingTemplate = { ...editingTemplate, includes: JSON.stringify(next) };
  }

  function updateFirstInitCmds(cmds: string[]) {
    if (!editingTemplate) return;
    editingTemplate = { ...editingTemplate, first_init_commands: JSON.stringify(cmds) };
  }

  function addDirRow() {
    persistenceDirs = [...persistenceDirs, { path: '', size: '20GB', pool: '' }];
  }

  function removeDirRow(i: number) {
    persistenceDirs = persistenceDirs.filter((_, idx) => idx !== i);
  }

  if (browser) { load(); }
</script>

<div class="max-w-5xl mx-auto">
  <div class="flex items-center gap-4 mb-6">
    <a href="/admin" class="text-gray-500 hover:text-gray-700 text-sm">← Admin</a>
    <h1 class="text-2xl font-bold">
      Debug: {template?.name ?? '…'}
    </h1>
    {#if template}
      <span class="text-sm text-gray-500 font-mono">{template.slug}</span>
    {/if}
  </div>

  {#if loading}
    <div class="flex justify-center py-12">
      <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-purple-600"></div>
    </div>
  {:else}
    <!-- Profile warnings -->
    {#if profileChecks.some(c => !c.exists)}
      <div class="mb-4 rounded-lg border border-amber-300 bg-amber-50 p-4">
        <p class="font-semibold text-amber-800 mb-2">Missing Incus profiles</p>
        <ul class="space-y-1 text-sm text-amber-700">
          {#each profileChecks.filter(c => !c.exists) as check}
            <li>
              Profile <code class="font-mono bg-amber-100 px-1 rounded">{check.profile}</code>
              does not exist on server <strong>{check.server}</strong>.
            </li>
          {/each}
        </ul>
        <p class="mt-3 text-xs text-amber-600">
          Run <code class="font-mono bg-amber-100 px-1 rounded">bash scripts/setup-docker-profile.sh</code> on the server to create the <code class="font-mono bg-amber-100 px-1 rounded">docker</code> profile,
          or remove it from the template's <em>Profiles</em> list if Docker-in-Docker is not needed.
        </p>
      </div>
    {/if}

    <!-- Repo warnings -->
    {#if missingRepos.length > 0}
      <div class="mb-4 rounded-lg border border-amber-300 bg-amber-50 p-4">
        <p class="font-semibold text-amber-800 mb-2">Missing repos</p>
        <ul class="space-y-1 text-sm text-amber-700">
          {#each missingRepos as ref}
            <li>
              Repo <code class="font-mono bg-amber-100 px-1 rounded">{ref.name}</code>
              is referenced in the template but was not found in <a href="/admin/repos" class="underline font-medium">Admin &rsaquo; Repos</a>.
            </li>
          {/each}
        </ul>
        <p class="mt-3 text-xs text-amber-600">
          Add the repo in <a href="/admin/repos" class="underline">Admin &rsaquo; Repos</a> and make sure it has finished cloning, or remove it from this template.
        </p>
      </div>
    {/if}

    <!-- Tab bar -->
    <div class="flex space-x-4 mb-6 border-b">
      <button
        onclick={() => activeSection = 'instance'}
        class="pb-2 px-1 {activeSection === 'instance' ? 'border-b-2 border-purple-600 text-purple-600' : 'text-gray-500'}"
      >Instance</button>
      <button
        onclick={() => activeSection = 'runner'}
        class="pb-2 px-1 {activeSection === 'runner' ? 'border-b-2 border-purple-600 text-purple-600' : 'text-gray-500'} {debugInstance?.status !== 'running' ? 'opacity-50' : ''}"
      >Step Runner</button>
      <button
        onclick={() => activeSection = 'editor'}
        class="pb-2 px-1 {activeSection === 'editor' ? 'border-b-2 border-purple-600 text-purple-600' : 'text-gray-500'}"
      >Template Editor</button>
    </div>

    <!-- ── Instance Tab ───────────────────────────────────────────────────── -->
    {#if activeSection === 'instance'}
      {#if !debugInstance}
        <div class="bg-white border rounded-lg p-8 text-center">
          <p class="text-gray-500 mb-4">No debug instance exists for this template.</p>
          <button
            onclick={createDebugInstance}
            class="px-4 py-2 bg-purple-600 text-white rounded hover:bg-purple-700"
          >Create Debug Instance</button>
        </div>
      {:else}
        <!-- Instance header -->
        <div class="bg-white border rounded-lg p-4 mb-4">
          <div class="flex items-center justify-between">
            <div>
              <p class="font-medium font-mono text-sm">{debugInstance.name}</p>
              <p class="text-xs text-gray-500 mt-0.5">{debugInstance.incus_name}</p>
            </div>
            <div class="flex items-center gap-3">
              <span class="px-3 py-1 rounded-full text-sm font-medium
                {debugInstance.status === 'running' ? 'bg-green-100 text-green-800' : ''}
                {debugInstance.status === 'stopped' ? 'bg-gray-100 text-gray-800' : ''}
                {debugInstance.status === 'creating' ? 'bg-yellow-100 text-yellow-800 animate-pulse' : ''}
                {debugInstance.status === 'error' ? 'bg-red-100 text-red-800' : ''}
              ">{debugInstance.status}</span>
              {#if debugInstance.status !== 'running' && debugInstance.status !== 'creating'}
                <button onclick={startInstance} class="px-3 py-1 bg-green-600 text-white text-sm rounded hover:bg-green-700">Start</button>
              {/if}
              {#if debugInstance.status === 'running'}
                <button onclick={stopInstance} class="px-3 py-1 bg-yellow-600 text-white text-sm rounded hover:bg-yellow-700">Stop</button>
              {/if}
              <button onclick={deleteInstance} class="px-3 py-1 bg-red-600 text-white text-sm rounded hover:bg-red-700">Delete</button>
              <button onclick={createDebugInstance} class="px-3 py-1 bg-purple-600 text-white text-sm rounded hover:bg-purple-700">Create New</button>
            </div>
          </div>
        </div>

        <!-- Creation log stream -->
        {#if debugInstance.status === 'creating'}
          <div class="bg-gray-950 rounded-lg border border-gray-800 overflow-hidden mb-4">
            <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-800">
              <div class="w-2 h-2 rounded-full bg-yellow-400 animate-pulse"></div>
              <span class="text-sm font-medium text-yellow-300">Creating instance…</span>
            </div>
            <div
              bind:this={creationLogEl}
              class="p-4 font-mono text-xs leading-relaxed overflow-y-auto"
              style="min-height: 200px; max-height: 400px;"
            >
              {#if logItems.length === 0}
                <span class="text-gray-500">Waiting for creation log…</span>
              {:else}
                {#each logItems as item}
                  {#if item.kind === 'log'}
                    <div class="text-green-400 whitespace-pre-wrap py-px">{item.text}</div>
                  {:else}
                    <div class="my-0.5">
                      <button
                        class="w-full text-left flex items-center gap-2 px-1 py-0.5 rounded hover:bg-gray-900"
                        onclick={() => { if (item.outputs.length > 0) item.expanded = !item.expanded; }}
                      >
                        <span class="text-gray-600 shrink-0">[{item.n}/{item.total}]</span>
                        <span class="text-cyan-300 flex-1 truncate">{item.label}</span>
                        {#if item.warning}<span class="text-yellow-400 shrink-0" title={item.warning}>⚠</span>{/if}
                        {#if item.outputs.length > 0}
                          <span class="text-gray-600 shrink-0 text-xs">{item.outputs.length} line{item.outputs.length !== 1 ? 's' : ''}</span>
                          <span class="text-gray-600 shrink-0">{item.expanded ? '▾' : '▸'}</span>
                        {/if}
                      </button>
                      {#if item.expanded && item.outputs.length > 0}
                        <div class="ml-8 mt-1 mb-1 pl-2 border-l border-gray-800">
                          {#each item.outputs as out}
                            <div class="text-gray-400 whitespace-pre-wrap">{out || '\u00a0'}</div>
                          {/each}
                        </div>
                      {/if}
                      {#if item.warning}
                        <div class="ml-8 text-yellow-500 py-px">{item.warning}</div>
                      {/if}
                    </div>
                  {/if}
                {/each}
              {/if}
            </div>
          </div>
        {/if}

        <!-- Terminals when running -->
        {#if debugInstance.status === 'running'}
          <div class="space-y-4">
            {#if hasVSCode && debugInstance.ip_address}
              <div class="bg-white border rounded-lg p-4">
                <p class="text-sm font-medium text-gray-700 mb-2">OpenVSCode Server</p>
                {#if tsServeStatus?.status === 'active' && tsServeStatus.port === 3463 && tsServeStatus.url}
                  <div class="flex items-center gap-2">
                    <a href={tsServeStatus.url} target="_blank" rel="noopener noreferrer"
                      class="inline-flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm"
                    >Open in VSCode ↗</a>
                    <span class="text-xs text-gray-500 font-mono truncate max-w-xs">{tsServeStatus.url}</span>
                    <button onclick={stopTsServe} disabled={tsServeLoading}
                      class="px-3 py-2 bg-red-600 text-white rounded hover:bg-red-700 disabled:opacity-50 text-sm"
                    >{tsServeLoading ? 'Stopping…' : 'Stop'}</button>
                  </div>
                {:else}
                  <div class="flex items-center gap-2">
                    <a href={`http://${debugInstance.ip_address}:3463`} target="_blank" rel="noopener noreferrer"
                      class="inline-flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm"
                    >Open in VSCode ↗</a>
                    <button onclick={startVsCodeServe} disabled={tsServeLoading}
                      class="px-3 py-2 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm"
                    >{tsServeLoading ? 'Starting…' : 'Serve via Tailscale'}</button>
                  </div>
                {/if}
              </div>
            {/if}
            <div class="bg-white border rounded-lg overflow-hidden">
              <div class="px-4 py-2 border-b bg-gray-50">
                <span class="text-sm font-medium">Terminal — root</span>
              </div>
              <Terminal instanceId={debugInstance.id} user="root" />
            </div>
            {#if template?.terminal_user}
              <div class="bg-white border rounded-lg overflow-hidden">
                <div class="px-4 py-2 border-b bg-gray-50">
                  <span class="text-sm font-medium">Terminal — {template.terminal_user}</span>
                </div>
                <Terminal instanceId={debugInstance.id} user={template.terminal_user} />
              </div>
            {/if}
          </div>
        {/if}
      {/if}
    {/if}

    <!-- ── Step Runner Tab ────────────────────────────────────────────────── -->
    {#if activeSection === 'runner'}
      {#if debugInstance?.status !== 'running'}
        <div class="bg-yellow-50 border border-yellow-200 rounded-lg p-4 text-yellow-800 text-sm">
          Instance must be running to use the Step Runner. Start or create a debug instance first.
        </div>
      {:else}
        <div class="space-y-4">
          <!-- Output panel -->
          <div class="bg-white border rounded-lg overflow-hidden">
            <div class="flex items-center justify-between px-4 py-2 border-b bg-gray-50">
              <span class="text-sm font-medium">Output</span>
              <button onclick={() => execOutput = ''} class="text-xs text-gray-500 hover:text-gray-700">Clear</button>
            </div>
            <pre class="p-4 text-xs font-mono whitespace-pre-wrap overflow-y-auto bg-gray-950 text-green-400" style="min-height: 120px; max-height: 300px;">{execOutput || '(no output yet)'}</pre>
          </div>

          <!-- SSH Keys & Secrets -->
          <div class="bg-white border rounded-lg p-4">
            <h3 class="font-medium mb-3">Setup Steps</h3>
            <div class="flex gap-3">
              <div class="flex-1 border rounded p-3">
                <p class="text-sm font-medium mb-1">SSH Keys & Secrets</p>
                <p class="text-xs text-gray-500 mb-2">Re-applies authorized_keys and env vars</p>
                <button
                  onclick={runReapplySetup}
                  disabled={execRunning}
                  class="px-3 py-1 bg-primary text-white text-sm rounded hover:bg-primary-dark disabled:opacity-50"
                >RUN</button>
              </div>
              <div class="flex-1 border rounded p-3">
                <p class="text-sm font-medium mb-1">Env Vars File</p>
                <p class="text-xs text-gray-500 mb-2">Show /etc/profile.d/plati-env.sh</p>
                <button
                  onclick={() => runExec('cat /etc/profile.d/plati-env.sh 2>/dev/null || echo "(not found)"')}
                  disabled={execRunning}
                  class="px-3 py-1 bg-primary text-white text-sm rounded hover:bg-primary-dark disabled:opacity-50"
                >RUN</button>
              </div>
            </div>
          </div>

          <!-- Repos -->
          <div class="bg-white border rounded-lg p-4">
            <h3 class="font-medium mb-3">Repos</h3>
            {#if parsedRepos.length > 0}
              <div class="space-y-2">
                {#each parsedRepos as ref}
                  <div class="flex items-center gap-3 text-sm {repoNames.has(ref.name) ? 'bg-gray-50 border' : 'bg-amber-50 border border-amber-300'} rounded px-3 py-2">
                    <a href="/admin/repos" class="font-mono {repoNames.has(ref.name) ? 'text-primary hover:text-primary-dark' : 'text-amber-700'} hover:underline shrink-0">{ref.name}</a>
                    {#if !repoNames.has(ref.name)}
                      <span class="text-amber-600 text-xs font-medium">(not found)</span>
                    {/if}
                    <span class="text-gray-400 shrink-0">&rarr;</span>
                    <span class="font-mono text-gray-600 flex-1 truncate">{ref.dest}</span>
                    <button
                      onclick={() => runExec(`[ -d "${ref.dest}" ] && [ "$(ls -A ${ref.dest} 2>/dev/null)" ] && echo "SKIP: ${ref.dest} already exists and is not empty" || cp -rp /plati-repos/${ref.name} ${ref.dest} && echo "Copied ${ref.name} to ${ref.dest}"`)}
                      disabled={execRunning || !repoNames.has(ref.name)}
                      class="shrink-0 px-2 py-0.5 bg-teal-600 text-white rounded text-xs hover:bg-teal-700 disabled:opacity-50"
                    >Copy Repo</button>
                  </div>
                {/each}
              </div>
            {:else}
              <p class="text-sm text-gray-500">No repos configured for this template.</p>
            {/if}
          </div>

          <!-- first_init_commands -->
          {#if parsedFirstInitCmds.length > 0}
            <div class="bg-white border rounded-lg p-4">
              <h3 class="font-medium mb-3">first_init_commands</h3>
              <div class="space-y-2">
                {#each parsedFirstInitCmds as cmd, i}
                  <div class="flex items-center gap-3 font-mono text-xs bg-gray-50 border rounded px-3 py-2">
                    <span class="text-gray-400 shrink-0">{i + 1}.</span>
                    <span class="flex-1 truncate text-gray-800">{cmd}</span>
                    <button
                      onclick={() => runExec(cmd)}
                      disabled={execRunning}
                      class="shrink-0 px-2 py-0.5 bg-purple-600 text-white rounded text-xs hover:bg-purple-700 disabled:opacity-50"
                    >RUN</button>
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          <!-- Mixin includes -->
          {#if parsedIncludes.length > 0}
            <div class="bg-white border rounded-lg p-4">
              <h3 class="font-medium mb-3">Mixin Includes</h3>
              <div class="space-y-4">
                {#each parsedIncludes as mixinName}
                  {@const mixin = availableMixins.find(m => m.name === mixinName)}
                  <div class="border rounded p-3">
                    <div class="flex items-center justify-between mb-2">
                      <span class="font-mono text-sm font-medium">{mixinName}</span>
                      {#if !mixin}
                        <span class="text-xs text-yellow-600 bg-yellow-50 border border-yellow-200 rounded px-2 py-0.5">⚠ not found on disk</span>
                      {:else}
                        <button
                          onclick={() => runMixinAll(mixin)}
                          disabled={execRunning}
                          class="px-3 py-1 bg-purple-600 text-white text-xs rounded hover:bg-purple-700 disabled:opacity-50"
                        >RUN ALL</button>
                      {/if}
                    </div>
                    {#if mixin}
                      <div class="space-y-1">
                        {#each mixin.commands as cmd, i}
                          <div class="flex items-center gap-3 font-mono text-xs bg-gray-50 border rounded px-3 py-1.5">
                            <span class="text-gray-400 shrink-0">{i + 1}.</span>
                            <span class="flex-1 truncate text-gray-700">{cmd}</span>
                            <button
                              onclick={() => runExec(cmd)}
                              disabled={execRunning}
                              class="shrink-0 px-2 py-0.5 bg-primary text-white rounded text-xs hover:bg-primary-dark disabled:opacity-50"
                            >RUN</button>
                          </div>
                        {/each}
                      </div>
                    {/if}
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          <!-- Custom exec -->
          <div class="bg-white border rounded-lg p-4">
            <h3 class="font-medium mb-3">Custom Command</h3>
            <div class="flex gap-2">
              <input
                id="custom-cmd"
                type="text"
                placeholder="Enter shell command..."
                class="flex-1 border rounded px-3 py-1.5 text-sm font-mono"
                onkeydown={(e) => { if (e.key === 'Enter') { const el = e.currentTarget as HTMLInputElement; runExec(el.value); el.value = ''; } }}
              />
              <button
                onclick={() => { const el = document.getElementById('custom-cmd') as HTMLInputElement; if (el?.value) { runExec(el.value); el.value = ''; } }}
                disabled={execRunning}
                class="px-4 py-1.5 bg-gray-800 text-white text-sm rounded hover:bg-gray-900 disabled:opacity-50"
              >RUN</button>
            </div>
          </div>
        </div>
      {/if}
    {/if}

    <!-- ── Template Editor Tab ────────────────────────────────────────────── -->
    {#if activeSection === 'editor' && editingTemplate}
      <div class="space-y-4">
        <!-- Basic fields -->
        <div class="bg-white border rounded-lg p-4">
          <h3 class="font-medium mb-3">Basic</h3>
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label class="block text-sm text-gray-600 mb-1">Name</label>
              <input
                type="text"
                bind:value={editingTemplate.name}
                class="w-full border rounded px-3 py-1.5 text-sm"
              />
            </div>
            <div>
              <label class="block text-sm text-gray-600 mb-1">Terminal User</label>
              <input
                type="text"
                bind:value={editingTemplate.terminal_user}
                placeholder="(root only)"
                class="w-full border rounded px-3 py-1.5 text-sm"
              />
            </div>
          </div>
        </div>

        <!-- Persistence dirs -->
        <div class="bg-white border rounded-lg p-4">
          <div class="flex items-center justify-between mb-3">
            <h3 class="font-medium">Workspace Directories</h3>
            <button onclick={addDirRow} class="text-sm text-primary hover:text-primary-dark">+ Add</button>
          </div>
          {#if persistenceDirs.length === 0}
            <p class="text-sm text-gray-500">(no directories)</p>
          {:else}
            <div class="space-y-2">
              {#each persistenceDirs as dir, i}
                <div class="flex gap-2 items-center">
                  <input type="text" bind:value={dir.path} placeholder="/workspace" class="flex-1 border rounded px-2 py-1 text-sm font-mono" />
                  <input type="text" bind:value={dir.size} placeholder="20GB" class="w-24 border rounded px-2 py-1 text-sm" />
                  <input type="text" bind:value={dir.pool} placeholder="pool" class="w-24 border rounded px-2 py-1 text-sm" />
                  <button onclick={() => removeDirRow(i)} class="text-red-500 hover:text-red-700 text-sm px-1">×</button>
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Mixin includes -->
        {#if availableMixins.length > 0}
          <div class="bg-white border rounded-lg p-4">
            <h3 class="font-medium mb-3">Mixin Includes</h3>
            <div class="grid grid-cols-2 gap-2">
              {#each availableMixins as mixin}
                <label class="flex items-center gap-2 text-sm cursor-pointer">
                  <input
                    type="checkbox"
                    checked={parsedIncludes.includes(mixin.name)}
                    onchange={() => toggleMixin(mixin.name)}
                    class="rounded"
                  />
                  <span class="font-mono">{mixin.name}</span>
                  <span class="text-gray-400 text-xs">({mixin.commands.length} cmd{mixin.commands.length !== 1 ? 's' : ''})</span>
                </label>
              {/each}
            </div>
          </div>
        {/if}

        <!-- first_init_commands editor -->
        <div class="bg-white border rounded-lg p-4">
          <div class="flex items-center justify-between mb-3">
            <h3 class="font-medium">first_init_commands</h3>
            <button
              onclick={() => updateFirstInitCmds([...parsedFirstInitCmds, ''])}
              class="text-sm text-primary hover:text-primary-dark"
            >+ Add</button>
          </div>
          {#if parsedFirstInitCmds.length === 0}
            <p class="text-sm text-gray-500">(no commands)</p>
          {:else}
            <div class="space-y-2">
              {#each parsedFirstInitCmds as cmd, i}
                <div class="flex gap-2 items-center">
                  <span class="text-gray-400 text-xs w-5 text-right shrink-0">{i + 1}.</span>
                  <input
                    type="text"
                    value={cmd}
                    oninput={(e) => {
                      const cmds = [...parsedFirstInitCmds];
                      cmds[i] = (e.currentTarget as HTMLInputElement).value;
                      updateFirstInitCmds(cmds);
                    }}
                    class="flex-1 border rounded px-2 py-1 text-sm font-mono"
                  />
                  <button
                    onclick={() => updateFirstInitCmds(parsedFirstInitCmds.filter((_, idx) => idx !== i))}
                    class="text-red-500 hover:text-red-700 text-sm px-1 shrink-0"
                  >×</button>
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Save button -->
        <div class="flex gap-3">
          <button
            onclick={saveTemplate}
            class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark"
          >Save Template</button>
          <button
            onclick={exportYAML}
            disabled={yamlLoading}
            class="px-4 py-2 bg-gray-700 text-white rounded hover:bg-gray-800 disabled:opacity-50"
          >Export YAML</button>
        </div>

        <!-- YAML editor -->
        <div class="bg-white border rounded-lg p-4">
          <h3 class="font-medium mb-3">YAML Editor</h3>
          <textarea
            bind:value={yamlContent}
            class="w-full border rounded px-3 py-2 text-xs font-mono"
            rows="20"
            placeholder="YAML will appear here after Export, or paste to update..."
          ></textarea>
          <div class="mt-2 flex gap-2">
            <button
              onclick={updateFromYAML}
              disabled={yamlLoading || !yamlContent.trim()}
              class="px-4 py-2 bg-purple-600 text-white rounded hover:bg-purple-700 disabled:opacity-50"
            >Update from YAML</button>
          </div>
        </div>

        <!-- Duplicate section -->
        <div class="bg-white border rounded-lg p-4">
          <h3 class="font-medium mb-3">Duplicate Template</h3>
          <div class="flex gap-3 items-end">
            <div class="flex-1">
              <label class="block text-sm text-gray-600 mb-1">New Name</label>
              <input type="text" bind:value={duplicateName} class="w-full border rounded px-3 py-1.5 text-sm" />
            </div>
            <div class="flex-1">
              <label class="block text-sm text-gray-600 mb-1">New Slug</label>
              <input type="text" bind:value={duplicateSlug} class="w-full border rounded px-3 py-1.5 text-sm font-mono" />
            </div>
            <button
              onclick={duplicateTemplate}
              class="px-4 py-1.5 bg-gray-700 text-white rounded hover:bg-gray-800 text-sm"
            >Duplicate</button>
          </div>
        </div>
      </div>
    {/if}
  {/if}
</div>
