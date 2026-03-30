<script lang="ts">
  import { browser } from '$app/environment';

  let { instanceId }: { instanceId: number } = $props();

  let terminalEl: HTMLDivElement | undefined = $state();
  let connected = $state(false);
  let error = $state('');

  let ws: WebSocket | null = null;
  let term: any = null;
  let fitAddon: any = null;
  let resizeObserver: ResizeObserver | null = null;

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
    const wsUrl = `${proto}//${window.location.host}/api/v1/instances/${instanceId}/terminal`;

    ws = new WebSocket(wsUrl);

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

  // Cleanup on component destroy using $effect
  $effect(() => {
    return () => {
      resizeObserver?.disconnect();
      ws?.close();
      term?.dispose();
    };
  });
</script>

<div class="space-y-3">
  <div class="flex items-center gap-3">
    {#if !connected}
      <button onclick={connect}
        class="px-4 py-2 bg-gray-800 text-green-400 rounded hover:bg-gray-700 font-mono text-sm">
        Open Terminal
      </button>
    {:else}
      <button onclick={disconnect}
        class="px-4 py-2 bg-red-700 text-white rounded hover:bg-red-600 text-sm">
        Close Terminal
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
