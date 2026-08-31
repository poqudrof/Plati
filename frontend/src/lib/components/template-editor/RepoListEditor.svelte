<script lang="ts">
  import type { GitRepo } from '$lib/api/types';
  import { admin } from '$lib/api';
  import { addNotification } from '$lib/stores/notifications';

  interface RepoRef {
    name: string;
    dest: string;
  }

  let {
    repos = $bindable([]),
    availableRepos = $bindable([])
  }: {
    repos: RepoRef[];
    availableRepos: GitRepo[];
  } = $props();

  let addingUrl = $state(false);
  let newUrl = $state('');
  let cloning = $state(false);

  function addRepoFromList(repo: GitRepo) {
    repos = [...repos, { name: repo.name, dest: '/workspace/' + repo.name }];
  }

  function removeRepo(index: number) {
    repos = repos.filter((_, i) => i !== index);
  }

  // Repos not already referenced in the template
  let unusedRepos = $derived(
    availableRepos.filter(r => !repos.some(ref => ref.name === r.name))
  );

  async function cloneAndAdd() {
    const url = newUrl.trim();
    if (!url) return;
    cloning = true;
    try {
      const repo = await admin.repos.add(url);
      availableRepos = [...availableRepos, repo];
      repos = [...repos, { name: repo.name, dest: '/workspace/' + repo.name }];
      addNotification('success', `Repo "${repo.name}" added and cloning started`);
      newUrl = '';
      addingUrl = false;
    } catch (e: any) {
      addNotification('error', 'Failed to add repo: ' + e.message);
    } finally {
      cloning = false;
    }
  }
</script>

<div class="space-y-2">
  {#if repos.length === 0}
    <p class="text-xs text-gray-400 italic">No repositories configured</p>
  {/if}
  {#each repos as repo, i}
    {@const matched = availableRepos.find(r => r.name === repo.name)}
    <div class="flex items-center gap-2 group">
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-1.5">
          <span class="px-2 py-1.5 text-xs font-mono bg-gray-50 border border-gray-200 rounded truncate">{repo.name}</span>
          {#if matched}
            <span class="shrink-0 w-1.5 h-1.5 rounded-full {matched.clone_status === 'ready' ? 'bg-green-400' : matched.clone_status === 'error' ? 'bg-red-400' : 'bg-amber-400'}" title={matched.clone_status}></span>
          {:else}
            <span class="shrink-0 text-[10px] text-amber-600 bg-amber-50 border border-amber-200 px-1 rounded">not found</span>
          {/if}
        </div>
      </div>
      <span class="text-xs text-gray-400 shrink-0">&#8594;</span>
      <input
        type="text"
        value={repo.dest}
        oninput={(e) => { repos[i].dest = (e.target as HTMLInputElement).value; repos = repos; }}
        placeholder="/workspace/repo"
        class="flex-1 px-2 py-1.5 border border-gray-200 rounded text-xs font-mono bg-gray-50 focus:bg-white focus:border-primary outline-none"
      />
      <button
        type="button"
        onclick={() => removeRepo(i)}
        class="p-1 text-red-400 hover:text-red-600 opacity-0 group-hover:opacity-100 transition-opacity"
        title="Remove"
      >
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
      </button>
    </div>
  {/each}

  <!-- Add from existing repos dropdown -->
  {#if unusedRepos.length > 0}
    <div class="flex items-center gap-2">
      <select
        onchange={(e) => {
          const sel = (e.target as HTMLSelectElement);
          const repo = availableRepos.find(r => r.name === sel.value);
          if (repo) addRepoFromList(repo);
          sel.value = '';
        }}
        class="flex-1 px-2 py-1.5 border border-dashed border-gray-300 rounded text-xs text-gray-500 bg-white focus:border-primary outline-none"
      >
        <option value="">+ Add from registered repos...</option>
        {#each unusedRepos as repo}
          <option value={repo.name}>
            {repo.name} ({repo.clone_status})
          </option>
        {/each}
      </select>
    </div>
  {/if}

  <!-- Add by URL -->
  {#if addingUrl}
    <div class="flex items-center gap-2">
      <input
        type="text"
        bind:value={newUrl}
        placeholder="git@github.com:org/repo.git"
        disabled={cloning}
        class="flex-1 px-2 py-1.5 border border-gray-200 rounded text-xs font-mono bg-gray-50 focus:bg-white focus:border-primary outline-none disabled:opacity-50"
        onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); cloneAndAdd(); } }}
      />
      <button
        type="button"
        onclick={cloneAndAdd}
        disabled={cloning || !newUrl.trim()}
        class="px-2 py-1 text-xs bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50"
      >
        {cloning ? 'Cloning...' : 'Clone & Add'}
      </button>
      <button
        type="button"
        onclick={() => { addingUrl = false; newUrl = ''; }}
        class="px-2 py-1 text-xs text-gray-500 hover:text-gray-700"
      >
        Cancel
      </button>
    </div>
  {:else}
    <button type="button" onclick={() => { addingUrl = true; }} class="text-xs text-primary hover:text-primary-dark">+ Add by git URL</button>
  {/if}
</div>
