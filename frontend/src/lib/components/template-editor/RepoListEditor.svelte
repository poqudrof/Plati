<script lang="ts">
  interface RepoRef {
    name: string;
    dest: string;
  }

  let { repos = $bindable([]) }: { repos: RepoRef[] } = $props();

  function addRepo() {
    repos = [...repos, { name: '', dest: '/workspace/' }];
  }

  function removeRepo(index: number) {
    repos = repos.filter((_, i) => i !== index);
  }
</script>

<div class="space-y-2">
  {#if repos.length === 0}
    <p class="text-xs text-gray-400 italic">No repositories configured</p>
  {/if}
  {#each repos as repo, i}
    <div class="flex items-center gap-2 group">
      <input
        type="text"
        value={repo.name}
        oninput={(e) => { repos[i].name = (e.target as HTMLInputElement).value; repos = repos; }}
        placeholder="repo-name"
        class="flex-1 px-2 py-1.5 border border-gray-200 rounded text-xs font-mono bg-gray-50 focus:bg-white focus:border-primary outline-none"
      />
      <span class="text-xs text-gray-400">&#8594;</span>
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
  <button type="button" onclick={addRepo} class="text-xs text-primary hover:text-primary-dark">+ Add repository</button>
</div>
