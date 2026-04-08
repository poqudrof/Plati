<script lang="ts">
  let { commands = $bindable([]), label = 'Commands' }: {
    commands: string[];
    label?: string;
  } = $props();

  let newCmd = $state('');

  function addCommand() {
    const cmd = newCmd.trim();
    if (!cmd) return;
    commands = [...commands, cmd];
    newCmd = '';
  }

  function removeCommand(index: number) {
    commands = commands.filter((_, i) => i !== index);
  }

  function moveUp(index: number) {
    if (index === 0) return;
    const arr = [...commands];
    [arr[index - 1], arr[index]] = [arr[index], arr[index - 1]];
    commands = arr;
  }

  function moveDown(index: number) {
    if (index >= commands.length - 1) return;
    const arr = [...commands];
    [arr[index], arr[index + 1]] = [arr[index + 1], arr[index]];
    commands = arr;
  }
</script>

<div class="space-y-2">
  {#if commands.length === 0}
    <p class="text-xs text-gray-400 italic">No {label.toLowerCase()} configured</p>
  {/if}
  {#each commands as cmd, i}
    <div class="flex items-start gap-1.5 group">
      <span class="text-xs text-gray-400 mt-2 w-5 text-right shrink-0">{i + 1}.</span>
      <input
        type="text"
        value={cmd}
        oninput={(e) => { commands[i] = (e.target as HTMLInputElement).value; commands = commands; }}
        class="flex-1 px-2 py-1.5 border border-gray-200 rounded text-xs font-mono bg-gray-50 focus:bg-white focus:border-primary focus:ring-1 focus:ring-primary/20 outline-none"
      />
      <div class="flex gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
        <button type="button" onclick={() => moveUp(i)} class="p-1 text-gray-400 hover:text-gray-600" title="Move up" disabled={i === 0}>
          <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15l7-7 7 7"/></svg>
        </button>
        <button type="button" onclick={() => moveDown(i)} class="p-1 text-gray-400 hover:text-gray-600" title="Move down" disabled={i >= commands.length - 1}>
          <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/></svg>
        </button>
        <button type="button" onclick={() => removeCommand(i)} class="p-1 text-red-400 hover:text-red-600" title="Remove">
          <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
        </button>
      </div>
    </div>
  {/each}
  <div class="flex gap-2">
    <input
      type="text"
      bind:value={newCmd}
      placeholder="Add command..."
      class="flex-1 px-2 py-1.5 border border-dashed border-gray-300 rounded text-xs font-mono focus:border-primary focus:ring-1 focus:ring-primary/20 outline-none"
      onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addCommand(); } }}
    />
    <button type="button" onclick={addCommand} class="px-2 py-1.5 text-xs bg-gray-100 hover:bg-gray-200 rounded text-gray-600">Add</button>
  </div>
</div>
