<script lang="ts">
  import { page } from '$app/stores';
  import { browser } from '$app/environment';
  import { admin, templates as templatesApi } from '$lib/api';
  import type { Template, MixinInfo, GitRepo } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';
  import CollapsibleSection from '$lib/components/template-editor/CollapsibleSection.svelte';
  import FieldRow from '$lib/components/template-editor/FieldRow.svelte';
  import CommandListEditor from '$lib/components/template-editor/CommandListEditor.svelte';
  import MixinCard from '$lib/components/template-editor/MixinCard.svelte';
  import PersistenceDirEditor from '$lib/components/template-editor/PersistenceDirEditor.svelte';
  import RepoListEditor from '$lib/components/template-editor/RepoListEditor.svelte';
  import ExecutionPreview from '$lib/components/template-editor/ExecutionPreview.svelte';

  // --- State ---
  let template: Template | null = $state(null);
  let allMixins: MixinInfo[] = $state([]);
  let availableRepos: GitRepo[] = $state([]);
  let saving = $state(false);
  let yamlContent = $state('');
  let showYaml = $state(false);
  let dirty = $state(false);

  // Parsed editable fields (derived from template JSON strings)
  let profiles: string[] = $state([]);
  let resources: Record<string, any> = $state({});
  let persistenceDirs: { path: string; size: string; pool?: string }[] = $state([]);
  let includes: string[] = $state([]);
  let firstInitCommands: string[] = $state([]);
  let rebuildCommands: string[] = $state([]);
  let repos: { name: string; dest: string }[] = $state([]);
  let healthChecks: { port: number; path: string; expected_status: number; timeout: number; description: string }[] = $state([]);
  let tailscaleServe: { port: number; funnel?: boolean } | null = $state(null);

  // Profile tag input
  let newProfile = $state('');

  function safeParseJSON<T>(json: string, fallback: T): T {
    try { return JSON.parse(json || JSON.stringify(fallback)); } catch { return fallback; }
  }

  function loadFromTemplate(t: Template) {
    profiles = safeParseJSON(t.profiles, []);
    resources = safeParseJSON(t.resources, {});
    persistenceDirs = safeParseJSON(t.persistence_dirs, []);
    includes = safeParseJSON(t.includes, []);
    firstInitCommands = safeParseJSON(t.first_init_commands, []);
    rebuildCommands = safeParseJSON(t.rebuild_commands, []);
    repos = safeParseJSON(t.repos, []);
    healthChecks = safeParseJSON(t.health_checks || '[]', []);
    tailscaleServe = t.tailscale_serve ? safeParseJSON(t.tailscale_serve, null) : null;
    dirty = false;
  }

  function buildTemplatePayload(): Partial<Template> {
    if (!template) return {};
    return {
      name: template.name,
      slug: template.slug,
      description: template.description,
      image: template.image,
      profiles: JSON.stringify(profiles),
      resources: JSON.stringify(resources),
      terminal_user: template.terminal_user,
      persistence_mode: template.persistence_mode,
      persistence_dirs: JSON.stringify(persistenceDirs),
      includes: JSON.stringify(includes),
      first_init_commands: JSON.stringify(firstInitCommands),
      rebuild_commands: JSON.stringify(rebuildCommands),
      repos: JSON.stringify(repos),
      health_checks: JSON.stringify(healthChecks),
      tailscale_serve: tailscaleServe ? JSON.stringify(tailscaleServe) : '',
      is_active: template.is_active,
    };
  }

  async function load() {
    const id = Number($page.params.id);
    try {
      const [t, m, repos] = await Promise.all([
        templatesApi.get(id),
        admin.templates.listMixins(),
        admin.repos.list()
      ]);
      template = t;
      allMixins = m;
      availableRepos = repos ?? [];
      loadFromTemplate(t);
    } catch (e: any) {
      addNotification('error', 'Failed to load template: ' + e.message);
    }
  }

  async function saveToDB() {
    if (!template) return;
    saving = true;
    try {
      const payload = buildTemplatePayload();
      await admin.templates.update(template.id, payload);
      addNotification('success', 'Template saved to database');
      dirty = false;
    } catch (e: any) {
      addNotification('error', 'Save failed: ' + e.message);
    } finally {
      saving = false;
    }
  }

  async function reloadFromDisk() {
    if (!template) return;
    saving = true;
    try {
      const updated = await admin.templates.reloadFromDisk(template.id);
      template = updated;
      loadFromTemplate(updated);
      addNotification('success', 'Reloaded from disk');
    } catch (e: any) {
      addNotification('error', 'Reload from disk failed: ' + e.message);
    } finally {
      saving = false;
    }
  }

  async function saveToDisk() {
    if (!template) return;
    saving = true;
    try {
      // First save to DB, then write to disk
      const payload = buildTemplatePayload();
      await admin.templates.update(template.id, payload);
      await admin.templates.saveToDisk(template.id);
      addNotification('success', 'Template saved to disk');
      dirty = false;
    } catch (e: any) {
      addNotification('error', 'Save to disk failed: ' + e.message);
    } finally {
      saving = false;
    }
  }

  async function loadYaml() {
    if (!template) return;
    try {
      const res = await admin.templates.exportYAML(template.id);
      yamlContent = res.yaml;
      showYaml = true;
    } catch (e: any) {
      addNotification('error', 'Failed to export YAML: ' + e.message);
    }
  }

  async function applyYaml() {
    if (!template || !yamlContent.trim()) return;
    try {
      const updated = await admin.templates.updateFromYAML(template.id, yamlContent);
      template = updated;
      loadFromTemplate(updated);
      addNotification('success', 'YAML applied');
    } catch (e: any) {
      addNotification('error', 'Failed to apply YAML: ' + e.message);
    }
  }

  function markDirty() { dirty = true; }

  function toggleMixin(name: string, checked: boolean) {
    if (checked) {
      includes = [...includes, name];
    } else {
      includes = includes.filter(n => n !== name);
    }
    markDirty();
  }

  function addProfile() {
    const p = newProfile.trim();
    if (!p || profiles.includes(p)) return;
    profiles = [...profiles, p];
    newProfile = '';
    markDirty();
  }

  function removeProfile(index: number) {
    profiles = profiles.filter((_, i) => i !== index);
    markDirty();
  }

  function addHealthCheck() {
    healthChecks = [...healthChecks, { port: 8080, path: '/health', expected_status: 200, timeout: 30, description: '' }];
    markDirty();
  }

  function removeHealthCheck(index: number) {
    healthChecks = healthChecks.filter((_, i) => i !== index);
    markDirty();
  }

  $effect(() => {
    if (browser) load();
  });
</script>

{#if !template}
  <div class="flex items-center justify-center h-64">
    <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
  </div>
{:else}
  <!-- Header -->
  <div class="mb-6">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-3">
        <a href="/admin" class="text-gray-400 hover:text-gray-600">
          <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"/></svg>
        </a>
        <div>
          <h1 class="text-xl font-bold text-gray-900">{template.name}</h1>
          <div class="flex items-center gap-2 mt-0.5">
            <span class="text-xs font-mono text-gray-500 bg-gray-100 px-1.5 py-0.5 rounded">{template.slug}</span>
            <span class="text-xs {template.is_active ? 'text-green-600' : 'text-gray-400'}">{template.is_active ? 'Active' : 'Inactive'}</span>
            {#if dirty}
              <span class="text-xs text-amber-600 font-medium">Unsaved changes</span>
            {/if}
          </div>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <a href="/admin/templates/{template.id}/debug" class="px-3 py-1.5 text-sm text-purple-600 hover:text-purple-800 border border-purple-200 rounded hover:bg-purple-50">Debug</a>
        <button onclick={reloadFromDisk} disabled={saving} class="px-3 py-1.5 text-sm text-gray-600 border border-gray-300 rounded hover:bg-gray-50 disabled:opacity-50" title="Re-read this template's YAML file from disk and update the database">Reload from disk</button>
        <button onclick={saveToDB} disabled={saving} class="px-3 py-1.5 text-sm border border-gray-300 rounded hover:bg-gray-50 disabled:opacity-50">Save to DB</button>
        <button onclick={saveToDisk} disabled={saving} class="px-3 py-1.5 text-sm bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50">
          {saving ? 'Saving...' : 'Save to Disk'}
        </button>
      </div>
    </div>
  </div>

  <!-- Two-column layout -->
  <div class="flex gap-6 items-start">
    <!-- Left column: Editor -->
    <div class="flex-1 min-w-0 space-y-3">

      <!-- Identity -->
      <CollapsibleSection title="Identity" description="Template metadata visible to users">
        <div class="grid grid-cols-2 gap-4">
          <FieldRow label="Name" description="Display name shown when creating instances">
            <input type="text" bind:value={template.name} oninput={markDirty} class="w-full px-2 py-1.5 border border-gray-200 rounded text-sm focus:border-primary focus:ring-1 focus:ring-primary/20 outline-none" />
          </FieldRow>
          <FieldRow label="Slug" description="Unique ID, used as YAML filename">
            <input type="text" bind:value={template.slug} oninput={markDirty} class="w-full px-2 py-1.5 border border-gray-200 rounded text-sm font-mono focus:border-primary focus:ring-1 focus:ring-primary/20 outline-none" />
          </FieldRow>
          <div class="col-span-2">
            <FieldRow label="Description" description="What this environment provides">
              <input type="text" bind:value={template.description} oninput={markDirty} class="w-full px-2 py-1.5 border border-gray-200 rounded text-sm focus:border-primary focus:ring-1 focus:ring-primary/20 outline-none" />
            </FieldRow>
          </div>
          <FieldRow label="Image" description="Incus base image reference">
            <input type="text" bind:value={template.image} oninput={markDirty} class="w-full px-2 py-1.5 border border-gray-200 rounded text-sm font-mono focus:border-primary focus:ring-1 focus:ring-primary/20 outline-none" />
          </FieldRow>
          <FieldRow label="Active">
            <label class="flex items-center gap-2 mt-1">
              <input type="checkbox" bind:checked={template.is_active} onchange={markDirty} class="rounded border-gray-300 text-primary focus:ring-primary" />
              <span class="text-sm text-gray-600">Visible to users</span>
            </label>
          </FieldRow>
        </div>
      </CollapsibleSection>

      <!-- Infrastructure -->
      <CollapsibleSection title="Infrastructure" description="Resource limits, profiles, and system configuration">
        <div class="space-y-4">
          <FieldRow label="Profiles" description="Incus profiles applied to the instance (e.g. default, docker, nvidia)">
            <div class="flex flex-wrap gap-1.5 mb-1.5">
              {#each profiles as profile, i}
                <span class="inline-flex items-center gap-1 px-2 py-0.5 bg-gray-100 rounded text-xs font-mono">
                  {profile}
                  <button type="button" onclick={() => removeProfile(i)} class="text-gray-400 hover:text-red-500">&times;</button>
                </span>
              {/each}
            </div>
            <div class="flex gap-1.5">
              <input type="text" bind:value={newProfile} placeholder="Add profile..." class="flex-1 px-2 py-1 border border-dashed border-gray-300 rounded text-xs font-mono focus:border-primary outline-none" onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addProfile(); } }} />
              <button type="button" onclick={addProfile} class="px-2 py-1 text-xs bg-gray-100 hover:bg-gray-200 rounded">Add</button>
            </div>
          </FieldRow>

          <div class="grid grid-cols-3 gap-4">
            <FieldRow label="CPU" description="Core limit">
              <input type="number" value={resources.cpu || ''} oninput={(e) => { resources.cpu = Number((e.target as HTMLInputElement).value) || undefined; resources = resources; markDirty(); }} class="w-full px-2 py-1.5 border border-gray-200 rounded text-sm font-mono focus:border-primary outline-none" />
            </FieldRow>
            <FieldRow label="Memory" description="e.g. 4GB">
              <input type="text" value={resources.memory || ''} oninput={(e) => { resources.memory = (e.target as HTMLInputElement).value || undefined; resources = resources; markDirty(); }} class="w-full px-2 py-1.5 border border-gray-200 rounded text-sm font-mono focus:border-primary outline-none" />
            </FieldRow>
            <FieldRow label="Disk" description="Legacy, see persistence">
              <input type="text" value={resources.disk || ''} oninput={(e) => { resources.disk = (e.target as HTMLInputElement).value || undefined; resources = resources; markDirty(); }} class="w-full px-2 py-1.5 border border-gray-200 rounded text-sm font-mono focus:border-primary outline-none" />
            </FieldRow>
          </div>

          <FieldRow label="Terminal User" description="Non-root user for web terminal. Empty = root only.">
            <input type="text" bind:value={template.terminal_user} oninput={markDirty} placeholder="e.g. ubuntu" class="w-full px-2 py-1.5 border border-gray-200 rounded text-sm focus:border-primary outline-none" />
          </FieldRow>

        </div>
      </CollapsibleSection>

      <!-- Persistence -->
      <CollapsibleSection title="Persistence" description="How storage survives instance rebuild">
        <div class="space-y-4">
          <FieldRow label="Mode">
            <select
              value={template.persistence_mode === 'ephemeral' ? 'ephemeral' : 'normal'}
              onchange={(e) => { template!.persistence_mode = (e.target as HTMLSelectElement).value; markDirty(); }}
              class="w-full px-2 py-1.5 border border-gray-200 rounded text-sm bg-white focus:border-primary focus:ring-1 focus:ring-primary/20 outline-none"
            >
              <option value="normal">Normal — persistent volumes survive rebuilds</option>
              <option value="ephemeral">Ephemeral — all storage wiped on rebuild/delete</option>
            </select>
          </FieldRow>

          {#if template.persistence_mode === 'ephemeral'}
            <div class="rounded-md bg-amber-50 border border-amber-200 px-3 py-2.5 space-y-1">
              <p class="text-xs font-semibold text-amber-800">Ephemeral mode</p>
              <p class="text-xs text-amber-700">No Incus volumes are created. All data lives on the instance's root disk and is <strong>permanently deleted</strong> when the instance is rebuilt or deleted. <code class="font-mono bg-amber-100 px-1 rounded">first_init_commands</code> run on every creation (no sentinel file). Best for stateless workloads, CI runners, or quick throwaway environments.</p>
            </div>
          {:else}
            <div class="rounded-md bg-blue-50 border border-blue-200 px-3 py-2.5 space-y-1">
              <p class="text-xs font-semibold text-blue-800">Normal (persistent) mode</p>
              <p class="text-xs text-blue-700">Incus storage volumes are created for each directory below and re-attached on rebuild — workspace data is preserved. A sentinel file (<code class="font-mono bg-blue-100 px-1 rounded">.plati-initialized</code>) distinguishes first creation from rebuilds: <code class="font-mono bg-blue-100 px-1 rounded">first_init_commands</code> run once on creation, <code class="font-mono bg-blue-100 px-1 rounded">rebuild_commands</code> run on subsequent rebuilds.</p>
            </div>
            <FieldRow label="Persistent directories" description="Each entry creates a separate Incus storage volume mounted at that path">
              <div class="mt-1">
                <PersistenceDirEditor bind:dirs={persistenceDirs} />
              </div>
            </FieldRow>
          {/if}
        </div>
      </CollapsibleSection>

      <!-- Mixins -->
      <CollapsibleSection title="Mixins" description="Modular setup bundles — files and commands injected at creation">
        <div class="space-y-2">
          {#each allMixins as mixin}
            <MixinCard
              {mixin}
              included={includes.includes(mixin.name)}
              ontoggle={toggleMixin}
            />
          {/each}
          {#if allMixins.length === 0}
            <p class="text-xs text-gray-400 italic">No mixins loaded from disk</p>
          {/if}
        </div>
      </CollapsibleSection>

      <!-- Repositories -->
      <CollapsibleSection title="Repositories" description="Git repos copied from host cache into the instance on first init">
        <RepoListEditor bind:repos bind:availableRepos />
      </CollapsibleSection>

      <!-- Lifecycle Commands -->
      <CollapsibleSection title="Lifecycle Commands" description="Shell commands run via incus exec after setup">
        <div class="space-y-4">
          <FieldRow label="First Init Commands" description="Run once on first instance creation, after mixin commands">
            <div class="mt-1">
              <CommandListEditor bind:commands={firstInitCommands} label="first init commands" />
            </div>
          </FieldRow>
          <FieldRow label="Rebuild Commands" description="Run on rebuild when persistent volumes already exist">
            <div class="mt-1">
              <CommandListEditor bind:commands={rebuildCommands} label="rebuild commands" />
            </div>
          </FieldRow>
        </div>
      </CollapsibleSection>

      <!-- Networking -->
      <CollapsibleSection title="Networking" description="Health checks and Tailscale configuration" open={healthChecks.length > 0 || (tailscaleServe !== null && tailscaleServe.port > 0)}>
        <div class="space-y-4">
          <FieldRow label="Tailscale Serve" description="Auto-expose a port via HTTPS on the Tailscale network">
            <div class="flex items-center gap-3 mt-1">
              <input
                type="number"
                value={tailscaleServe?.port || ''}
                oninput={(e) => {
                  const port = Number((e.target as HTMLInputElement).value);
                  if (port > 0) {
                    tailscaleServe = { port, funnel: tailscaleServe?.funnel };
                  } else {
                    tailscaleServe = null;
                  }
                  markDirty();
                }}
                placeholder="Port"
                class="w-24 px-2 py-1.5 border border-gray-200 rounded text-sm font-mono focus:border-primary outline-none"
              />
              <label class="flex items-center gap-1.5">
                <input
                  type="checkbox"
                  checked={tailscaleServe?.funnel || false}
                  onchange={(e) => {
                    if (tailscaleServe) {
                      tailscaleServe = { ...tailscaleServe, funnel: (e.target as HTMLInputElement).checked };
                      markDirty();
                    }
                  }}
                  class="rounded border-gray-300 text-primary focus:ring-primary"
                  disabled={!tailscaleServe}
                />
                <span class="text-sm text-gray-600">Funnel (public)</span>
              </label>
            </div>
          </FieldRow>

          <FieldRow label="Health Checks" description="HTTP endpoints polled after setup to verify readiness">
            <div class="space-y-2 mt-1">
              {#each healthChecks as hc, i}
                <div class="flex items-center gap-2 group">
                  <input type="number" value={hc.port} oninput={(e) => { healthChecks[i].port = Number((e.target as HTMLInputElement).value); healthChecks = healthChecks; markDirty(); }} placeholder="Port" class="w-16 px-2 py-1 border border-gray-200 rounded text-xs font-mono focus:border-primary outline-none" />
                  <input type="text" value={hc.path} oninput={(e) => { healthChecks[i].path = (e.target as HTMLInputElement).value; healthChecks = healthChecks; markDirty(); }} placeholder="/health" class="w-24 px-2 py-1 border border-gray-200 rounded text-xs font-mono focus:border-primary outline-none" />
                  <input type="number" value={hc.expected_status} oninput={(e) => { healthChecks[i].expected_status = Number((e.target as HTMLInputElement).value); healthChecks = healthChecks; markDirty(); }} placeholder="200" class="w-14 px-2 py-1 border border-gray-200 rounded text-xs font-mono focus:border-primary outline-none" />
                  <input type="text" value={hc.description} oninput={(e) => { healthChecks[i].description = (e.target as HTMLInputElement).value; healthChecks = healthChecks; markDirty(); }} placeholder="Description" class="flex-1 px-2 py-1 border border-gray-200 rounded text-xs focus:border-primary outline-none" />
                  <button type="button" onclick={() => removeHealthCheck(i)} class="p-1 text-red-400 hover:text-red-600 opacity-0 group-hover:opacity-100" title="Remove">
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
                  </button>
                </div>
              {/each}
              <button type="button" onclick={addHealthCheck} class="text-xs text-primary hover:text-primary-dark">+ Add health check</button>
            </div>
          </FieldRow>
        </div>
      </CollapsibleSection>

      <!-- Raw YAML -->
      <CollapsibleSection title="Raw YAML" description="Export, edit, or import template as YAML" open={false}>
        <div class="space-y-2">
          <div class="flex gap-2">
            <button type="button" onclick={loadYaml} class="px-2 py-1 text-xs bg-gray-100 hover:bg-gray-200 rounded">
              {showYaml ? 'Refresh YAML' : 'Load YAML'}
            </button>
            {#if showYaml}
              <button type="button" onclick={applyYaml} class="px-2 py-1 text-xs bg-primary-50 hover:bg-primary-50 text-primary-dark rounded">Apply YAML</button>
            {/if}
          </div>
          {#if showYaml}
            <textarea bind:value={yamlContent} rows="20" class="w-full px-3 py-2 border border-gray-200 rounded text-xs font-mono bg-gray-50 focus:bg-white focus:border-primary outline-none resize-y"></textarea>
          {/if}
        </div>
      </CollapsibleSection>
    </div>

    <!-- Right column: Execution Preview -->
    <div class="w-80 xl:w-96 shrink-0 sticky top-4">
      <div class="border border-gray-200 rounded-lg bg-white p-4">
        <h2 class="text-sm font-semibold text-gray-900 mb-3">Execution Preview</h2>
        <ExecutionPreview
          image={template.image}
          {profiles}
          {resources}
          terminalUser={template.terminal_user}
          persistenceMode={template.persistence_mode}
          {persistenceDirs}
          {includes}
          mixins={allMixins}
          {firstInitCommands}
          {rebuildCommands}
          {repos}
          {tailscaleServe}
        />
      </div>
    </div>
  </div>
{/if}
