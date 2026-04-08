<script lang="ts">
  import { browser } from '$app/environment';
  import { admin, templates as templatesApi } from '$lib/api';
  import type { GitRepo, Template } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let repos: GitRepo[] = $state([]);
  let allTemplates: Template[] = $state([]);
  let serverKey = $state('');
  let newSSHURL = $state('');
  let loading = $state(false);
  let syncResults: Record<number, { status: string; command: string; output: string } | null> = $state({});
  let syncingIds: Set<number> = $state(new Set());
  let renamingId: number | null = $state(null);
  let renameValue = $state('');

  async function loadRepos() {
    try { repos = await admin.repos.list(); } catch (e: any) { addNotification('error', 'Failed to load repos: ' + e.message); }
  }

  async function loadTemplates() {
    try { allTemplates = await templatesApi.list(); } catch { allTemplates = []; }
  }

  async function loadServerKey() {
    try {
      const result = await admin.repos.getServerKey();
      serverKey = result.public_key;
    } catch {
      serverKey = '';
    }
  }

  function templatesUsingRepo(repoName: string): { name: string; slug: string; id: number }[] {
    return allTemplates.filter(t => {
      try {
        const refs: { name: string }[] = JSON.parse(t.repos || '[]');
        return refs.some(r => r.name === repoName);
      } catch { return false; }
    }).map(t => ({ name: t.name, slug: t.slug, id: t.id }));
  }

  async function addRepo() {
    if (!newSSHURL.trim()) return;
    loading = true;
    try {
      await admin.repos.add(newSSHURL.trim());
      newSSHURL = '';
      await loadRepos();
      addNotification('success', 'Repository added and cloning started');
    } catch (e: any) {
      addNotification('error', 'Failed to add repo: ' + e.message);
    } finally {
      loading = false;
    }
  }

  async function deleteRepo(id: number, name: string) {
    if (!confirm(`Delete repo "${name}"? This will remove the local clone.`)) return;
    try {
      await admin.repos.delete(id);
      await loadRepos();
      addNotification('success', 'Repository deleted');
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function syncRepo(id: number) {
    syncingIds.add(id);
    syncingIds = new Set(syncingIds);
    syncResults[id] = null;
    try {
      const result = await admin.repos.sync(id);
      syncResults[id] = result;
      syncResults = { ...syncResults };
      await loadRepos();
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      syncingIds.delete(id);
      syncingIds = new Set(syncingIds);
    }
  }

  function startRename(repo: GitRepo) {
    renamingId = repo.id;
    renameValue = repo.name;
  }

  async function submitRename(id: number) {
    if (!renameValue.trim()) return;
    try {
      await admin.repos.rename(id, renameValue.trim());
      renamingId = null;
      await loadRepos();
    } catch (e: any) { addNotification('error', e.message); }
  }

  function cancelRename() {
    renamingId = null;
  }

  function copyToClipboard(text: string) {
    navigator.clipboard.writeText(text).then(
      () => addNotification('success', 'Copied to clipboard'),
      () => addNotification('error', 'Failed to copy')
    );
  }

  function statusColor(status: string) {
    switch (status) {
      case 'ready': return 'text-green-600 bg-green-50';
      case 'cloning': return 'text-blue-600 bg-blue-50';
      case 'error': return 'text-red-600 bg-red-50';
      default: return 'text-gray-600 bg-gray-50';
    }
  }

  if (browser) {
    loadRepos();
    loadTemplates();
    loadServerKey();
  }
</script>

<div class="max-w-4xl mx-auto py-8 px-4 space-y-8">
  <h1 class="text-2xl font-bold">Git Repositories</h1>

  <!-- Add repo -->
  <section class="border rounded-lg p-6 space-y-4">
    <h2 class="text-lg font-semibold">Add Repository</h2>
    <form onsubmit={(e) => { e.preventDefault(); addRepo(); }} class="flex gap-2">
      <input
        type="text"
        bind:value={newSSHURL}
        placeholder="git@github.com:org/repo.git"
        class="flex-1 border rounded px-3 py-2 text-sm font-mono"
        disabled={loading}
      />
      <button
        type="submit"
        disabled={loading || !newSSHURL.trim()}
        class="px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
      >Add</button>
    </form>
  </section>

  <!-- Repo list -->
  <section class="space-y-3">
    {#if repos.length === 0}
      <p class="text-gray-500 text-sm">No repositories registered.</p>
    {:else}
      {#each repos as repo (repo.id)}
        {@const usedBy = templatesUsingRepo(repo.name)}
        <div class="border rounded-lg p-4 space-y-2">
          <div class="flex items-center justify-between gap-4">
            <div class="min-w-0">
              {#if renamingId === repo.id}
                <form onsubmit={(e) => { e.preventDefault(); submitRename(repo.id); }} class="flex items-center gap-2">
                  <input
                    type="text"
                    bind:value={renameValue}
                    class="border rounded px-2 py-1 text-sm font-medium w-48"
                    onkeydown={(e) => { if (e.key === 'Escape') cancelRename(); }}
                  />
                  <button type="submit" class="text-xs text-green-600 hover:text-green-800">Save</button>
                  <button type="button" onclick={cancelRename} class="text-xs text-gray-500 hover:text-gray-700">Cancel</button>
                </form>
              {:else}
                <div class="flex items-center gap-2">
                  <div class="font-medium">{repo.name}</div>
                  <button
                    onclick={() => startRename(repo)}
                    class="text-xs text-gray-400 hover:text-gray-600"
                    title="Rename"
                  >Rename</button>
                </div>
              {/if}
              <div class="text-xs text-gray-500 font-mono truncate">{repo.ssh_url}</div>
            </div>
            <div class="flex items-center gap-2 shrink-0">
              <span class="px-2 py-0.5 rounded text-xs font-medium {statusColor(repo.clone_status)}">
                {repo.clone_status}
              </span>
              <button
                onclick={() => syncRepo(repo.id)}
                class="px-3 py-1 text-sm border rounded hover:bg-gray-50 disabled:opacity-50"
                title="Pull latest"
                disabled={syncingIds.has(repo.id)}
              >{syncingIds.has(repo.id) ? 'Syncing...' : 'Sync'}</button>
              <button
                onclick={() => deleteRepo(repo.id, repo.name)}
                class="px-3 py-1 text-sm text-red-600 border border-red-200 rounded hover:bg-red-50"
              >Delete</button>
            </div>
          </div>
          {#if repo.clone_status === 'error' && repo.error_message}
            <pre class="text-xs text-red-700 bg-red-50 p-2 rounded overflow-x-auto">{repo.error_message}</pre>
          {/if}
          {#if usedBy.length > 0}
            <div class="text-xs text-gray-500">
              Used by:
              {#each usedBy as tmpl, i}
                <a href="/admin/templates/{tmpl.id}/debug" class="text-primary hover:text-primary-dark font-mono">{tmpl.slug}</a>{#if i < usedBy.length - 1},&nbsp;{/if}
              {/each}
            </div>
          {/if}
          {#if repo.last_synced_at}
            <div class="text-xs text-gray-400">Last synced: {new Date(repo.last_synced_at).toLocaleString()}</div>
          {/if}
          {#if syncResults[repo.id] != null}
            {@const res = syncResults[repo.id]!}
            <div class="mt-2 border rounded p-3 text-xs space-y-1 {res.status === 'error' ? 'bg-red-50 border-red-200' : 'bg-gray-50 border-gray-200'}">
              <div class="font-medium {res.status === 'error' ? 'text-red-700' : 'text-gray-700'}">
                {res.status === 'error' ? 'Sync failed' : 'Sync succeeded'}
              </div>
              {#if res.command}
                <div class="text-gray-500">$ <span class="font-mono">{res.command}</span></div>
              {/if}
              {#if res.output}
                <pre class="font-mono whitespace-pre-wrap text-gray-600 bg-white rounded p-2 border overflow-x-auto">{res.output}</pre>
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    {/if}
  </section>

  <!-- Server SSH Key (compact) -->
  <section class="border-t pt-6">
    <div class="flex items-start justify-between gap-4">
      <div class="min-w-0">
        <p class="text-xs font-medium text-gray-500 uppercase tracking-wide mb-1">Server SSH Key</p>
        {#if serverKey}
          <p class="text-xs text-gray-400 mb-1">Add as a deploy key (read-only) on each GitHub repo you want to cache.</p>
          <div class="flex items-center gap-2">
            <code class="text-xs bg-gray-100 px-2 py-1 rounded font-mono truncate max-w-md">{serverKey}</code>
            <button onclick={() => copyToClipboard(serverKey)} class="text-xs text-primary hover:text-primary-dark border border-primary/20 rounded px-2 py-0.5 shrink-0">Copy</button>
          </div>
        {:else}
          <p class="text-xs text-gray-400">No server key configured. Set one in <a href="/admin" class="underline text-primary">Admin → SSH Keys</a>.</p>
        {/if}
      </div>
    </div>
  </section>
</div>
