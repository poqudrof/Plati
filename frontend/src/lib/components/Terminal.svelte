<script lang="ts">
  import { browser } from '$app/environment';

  let {
    instanceId,
    user = 'root',
    label = '',
    wsUrl = ''
  }: { instanceId: number; user?: string; label?: string; wsUrl?: string } = $props();

  let terminalEl: HTMLDivElement | undefined = $state();
  let connected = $state(false);
  let error = $state('');

  let ws: WebSocket | null = null;
  let term: any = null;
  let fitAddon: any = null;
  let resizeObserver: ResizeObserver | null = null;

  // Visual context: root gets an amber badge, other users get a green badge
  let isRoot = $derived(user === 'root');
  let displayLabel = $derived(label || (isRoot ? 'root' : user));

  async function connect() {
    if (!browser || !terminalEl) return;

    error = '';

    const { Terminal } = await import('@xterm/xterm');
    const { FitAddon } = await import('@xterm/addon-fit');
    await import('@xterm/xterm/css/xterm.css');

    term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
      theme: {
        background: '#1e1e2e',
        foreground: '#cdd6f4',
        cursor: '#f5e0dc',
        selectionBackground: '#585b70',
      }
    });

    fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalEl);
    fitAddon.fit();

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const computedUrl = `${proto}//${window.location.host}/api/v1/instances/${instanceId}/terminal?user=${encodeURIComponent(user)}`;

    ws = new WebSocket(wsUrl || computedUrl);

    ws.onopen = () => {
      connected = true;
      term.focus();
    };

    ws.onmessage = (ev: MessageEvent) => {
      term.write(ev.data);
    };

    ws.onclose = () => {
      connected = false;
      term?.write('\r\n\x1b[31m[Connection closed]\x1b[0m\r\n');
    };

    ws.onerror = () => {
      error = 'WebSocket connection failed';
      connected = false;
    };

    term.onData((data: string) => {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(data);
      }
    });

    resizeObserver = new ResizeObserver(() => {
      fitAddon?.fit();
    });
    resizeObserver.observe(terminalEl);
  }

  function disconnect() {
    resizeObserver?.disconnect();
    resizeObserver = null;
    ws?.close();
    ws = null;
    term?.dispose();
    term = null;
    fitAddon = null;
    connected = false;
  }

  $effect(() => {
    return () => {
      resizeObserver?.disconnect();
      ws?.close();
      term?.dispose();
    };
  });
</script>

<div class="space-y-2">
  <div class="flex items-center gap-2">
    <!-- Context badge -->
    <span class="px-2 py-0.5 rounded text-xs font-mono font-semibold
      {isRoot ? 'bg-amber-100 text-amber-800 border border-amber-300' : 'bg-green-100 text-green-800 border border-green-300'}">
      {displayLabel}
    </span>

    {#if !connected}
      <button onclick={connect}
        class="px-3 py-1.5 rounded text-sm font-medium
          {isRoot
            ? 'bg-gray-800 text-amber-400 hover:bg-gray-700 border border-amber-900/40'
            : 'bg-gray-800 text-green-400 hover:bg-gray-700 border border-green-900/40'}">
        Open Terminal
      </button>
    {:else}
      <button onclick={disconnect}
        class="px-3 py-1.5 bg-red-700 text-white rounded hover:bg-red-600 text-sm">
        Close
      </button>
      <span class="text-xs text-green-600 font-medium">Connected</span>
    {/if}

    {#if error}
      <span class="text-xs text-red-600">{error}</span>
    {/if}
  </div>

  <div bind:this={terminalEl}
    class="rounded-lg overflow-hidden border border-gray-700"
    style="height: 400px; {connected ? '' : 'display:none'}"
  ></div>
</div>
