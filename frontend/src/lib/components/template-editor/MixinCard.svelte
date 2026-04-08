<script lang="ts">
  import type { MixinInfo } from '$lib/api/types';

  let { mixin, included = false, ontoggle }: {
    mixin: MixinInfo;
    included?: boolean;
    ontoggle?: (name: string, checked: boolean) => void;
  } = $props();

  let showDetails = $state(false);
</script>

<div class="border rounded-lg transition-colors {included ? 'border-purple-300 bg-purple-50/50' : 'border-gray-200 bg-gray-50/50'}">
  <div class="flex items-center gap-3 px-3 py-2.5">
    <input
      type="checkbox"
      checked={included}
      onchange={(e) => ontoggle?.(mixin.name, (e.target as HTMLInputElement).checked)}
      class="rounded border-gray-300 text-purple-600 focus:ring-purple-500"
    />
    <div class="flex-1 min-w-0">
      <span class="text-sm font-medium {included ? 'text-purple-900' : 'text-gray-600'}">{mixin.name}</span>
      <span class="text-xs text-gray-400 ml-2">
        {mixin.commands.length} cmd{mixin.commands.length !== 1 ? 's' : ''}
        {#if mixin.files.length > 0}
          &middot; {mixin.files.length} file{mixin.files.length !== 1 ? 's' : ''}
        {/if}
      </span>
    </div>
    <button
      type="button"
      class="text-xs text-gray-400 hover:text-gray-600"
      onclick={() => showDetails = !showDetails}
    >
      {showDetails ? 'Hide' : 'Details'}
    </button>
  </div>

  {#if showDetails}
    <div class="px-3 pb-3 pt-1 border-t {included ? 'border-purple-200' : 'border-gray-200'} space-y-2">
      {#if mixin.files.length > 0}
        <div>
          <p class="text-xs font-medium text-gray-500 mb-1">Files pushed:</p>
          {#each mixin.files as file}
            <div class="text-xs font-mono text-gray-600 flex items-center gap-1 py-0.5">
              <span class="text-purple-500">&#8594;</span>
              <span>{file.dest}</span>
              <span class="text-gray-400">({file.mode})</span>
            </div>
          {/each}
        </div>
      {/if}
      {#if mixin.commands.length > 0}
        <div>
          <p class="text-xs font-medium text-gray-500 mb-1">Commands:</p>
          {#each mixin.commands as cmd, i}
            <div class="text-xs font-mono text-gray-600 py-0.5 flex gap-1.5">
              <span class="text-gray-400 shrink-0">{i + 1}.</span>
              <span class="break-all">{cmd}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>
