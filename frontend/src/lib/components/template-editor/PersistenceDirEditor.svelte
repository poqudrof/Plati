<script lang="ts">
  interface PersistenceDir {
    path: string;
    size: string;
    pool?: string;
  }

  let { dirs = $bindable([]) }: { dirs: PersistenceDir[] } = $props();

  function addDir() {
    dirs = [...dirs, { path: '/home/ubuntu', size: '20GB', pool: '' }];
  }

  function removeDir(index: number) {
    dirs = dirs.filter((_, i) => i !== index);
  }
</script>

<div class="space-y-2">
  {#if dirs.length === 0}
    <p class="text-xs text-gray-400 italic">No persistent directories</p>
  {/if}
  {#each dirs as dir, i}
    <div class="flex items-center gap-2 group">
      <input
        type="text"
        value={dir.path}
        oninput={(e) => { dirs[i].path = (e.target as HTMLInputElement).value; dirs = dirs; }}
        placeholder="/home/ubuntu"
        class="flex-1 px-2 py-1.5 border border-gray-200 rounded text-xs font-mono bg-gray-50 focus:bg-white focus:border-primary outline-none"
      />
      <input
        type="text"
        value={dir.size}
        oninput={(e) => { dirs[i].size = (e.target as HTMLInputElement).value; dirs = dirs; }}
        placeholder="20GB"
        class="w-20 px-2 py-1.5 border border-gray-200 rounded text-xs font-mono bg-gray-50 focus:bg-white focus:border-primary outline-none"
      />
      <input
        type="text"
        value={dir.pool || ''}
        oninput={(e) => { dirs[i].pool = (e.target as HTMLInputElement).value; dirs = dirs; }}
        placeholder="pool"
        class="w-20 px-2 py-1.5 border border-gray-200 rounded text-xs font-mono bg-gray-50 focus:bg-white focus:border-primary outline-none"
      />
      <button
        type="button"
        onclick={() => removeDir(i)}
        class="p-1 text-red-400 hover:text-red-600 opacity-0 group-hover:opacity-100 transition-opacity"
        title="Remove"
      >
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
      </button>
    </div>
  {/each}
  <button type="button" onclick={addDir} class="text-xs text-primary hover:text-primary-dark">+ Add directory</button>
</div>
