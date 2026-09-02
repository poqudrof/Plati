<script lang="ts">
  import { browser } from '$app/environment';
  import { instances } from '$lib/api';
  import type { SleepSettings } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  interface Props {
    instanceId: number;
    instanceStatus: string;
    createdAt: string;
  }

  let { instanceId, instanceStatus, createdAt }: Props = $props();

  let settings = $state<SleepSettings | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let error = $state('');

  // Presets cover the useful range; anything else goes through "Custom".
  // 0 is the sentinel for "follow the platform default".
  const PRESETS = [
    { minutes: 0, label: 'Platform default' },
    { minutes: 30, label: '30 minutes' },
    { minutes: 60, label: '1 hour' },
    { minutes: 120, label: '2 hours' },
    { minutes: 240, label: '4 hours' },
    { minutes: 480, label: '8 hours' },
    { minutes: 720, label: '12 hours' },
    { minutes: 1440, label: '24 hours' },
    { minutes: 4320, label: '3 days' }
  ];

  let customMinutes = $state(0);
  let showCustom = $state(false);

  // A ticking clock so the countdown stays honest without polling the API.
  let now = $state(Date.now());
  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(t);
  });

  $effect(() => {
    if (browser && instanceId) load();
  });

  async function load() {
    loading = true;
    error = '';
    try {
      settings = await instances.sleepSettings(instanceId);
      showCustom = !PRESETS.some(p => p.minutes === settings!.timeout_minutes);
      customMinutes = settings.timeout_minutes;
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function save(patch: { disabled?: boolean; timeout_minutes?: number }) {
    saving = true;
    try {
      settings = await instances.updateSleepSettings(instanceId, patch);
      addNotification('success', 'Auto-stop settings saved');
    } catch (e: any) {
      addNotification('error', e.message);
      await load(); // the UI must not keep showing a value the server refused
    } finally {
      saving = false;
    }
  }

  async function resetTimer() {
    saving = true;
    try {
      settings = await instances.resetSleepTimer(instanceId);
      addNotification('success', 'Auto-stop timer reset');
    } catch (e: any) {
      addNotification('error', e.message);
    } finally {
      saving = false;
    }
  }

  function onPresetChange(value: string) {
    if (value === 'custom') {
      showCustom = true;
      customMinutes = settings?.effective_minutes ?? 240;
      return;
    }
    showCustom = false;
    save({ timeout_minutes: Number(value) });
  }

  function humanMinutes(m: number): string {
    if (m <= 0) return '—';
    if (m < 60) return `${m} min`;
    const h = Math.floor(m / 60);
    const rest = m % 60;
    if (h < 24) return rest ? `${h} h ${rest} min` : `${h} h`;
    const d = Math.floor(h / 24);
    const hr = h % 24;
    return hr ? `${d} d ${hr} h` : `${d} d`;
  }

  function formatDate(iso: string | null): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

  // Minutes left before the worker may stop the instance; null when nothing is scheduled.
  let minutesLeft = $derived.by(() => {
    if (!settings?.sleeps_at) return null;
    return Math.round((new Date(settings.sleeps_at).getTime() - now) / 60_000);
  });

  let selectValue = $derived(showCustom ? 'custom' : String(settings?.timeout_minutes ?? 0));
</script>

<div class="p-6 space-y-6">
  <div>
    <h3 class="text-base font-semibold mb-1">Status</h3>
    <p class="text-sm text-gray-500 leading-relaxed">
      Current state of the instance and its auto-stop policy.
    </p>
  </div>

  <!-- ── State summary ── -->
  <div class="grid gap-4 sm:grid-cols-3">
    <div class="card-static p-4">
      <p class="text-xs uppercase tracking-wide text-gray-400 mb-1">State</p>
      <p class="text-sm font-medium">
        {#if instanceStatus === 'running'}
          <span class="inline-flex items-center gap-1.5">
            <span class="w-2 h-2 rounded-full bg-primary"></span> Running
          </span>
        {:else if instanceStatus === 'stopped'}
          <span class="inline-flex items-center gap-1.5">
            <span class="w-2 h-2 rounded-full bg-gray-400"></span> Stopped
          </span>
        {:else}
          <span class="capitalize">{instanceStatus}</span>
        {/if}
      </p>
    </div>
    <div class="card-static p-4">
      <p class="text-xs uppercase tracking-wide text-gray-400 mb-1">Created</p>
      <p class="text-sm font-medium">{formatDate(createdAt)}</p>
    </div>
    <div class="card-static p-4">
      <p class="text-xs uppercase tracking-wide text-gray-400 mb-1">Timer started</p>
      <p class="text-sm font-medium">{formatDate(settings?.last_active_at ?? null)}</p>
    </div>
  </div>

  {#if loading}
    <p class="text-sm text-gray-500">Loading auto-stop settings…</p>
  {:else if error}
    <p class="text-sm text-red-600">{error}</p>
  {:else if settings}
    <!-- ── Auto-stop ── -->
    <div class="card-static p-6 space-y-5">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h4 class="text-sm font-semibold text-gray-900">Auto-stop</h4>
          <p class="text-sm text-gray-500 leading-relaxed mt-1">
            Plati stops an instance once its timer runs out, to free the host's memory and CPU.
            Incus itself never stops anything on its own.
          </p>
        </div>
        <!-- The switch reads "auto-stop on", which is the inverse of the stored `disabled`. -->
        <button
          role="switch"
          aria-checked={!settings.disabled}
          aria-label="Auto-stop enabled"
          disabled={saving}
          onclick={() => save({ disabled: !settings!.disabled })}
          class="relative shrink-0 w-12 h-6 rounded-full transition-colors disabled:opacity-50
            {settings.disabled ? 'bg-gray-300' : 'bg-primary'}"
        >
          <span
            class="absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full transition-transform
              {settings.disabled ? '' : 'translate-x-6'}"
          ></span>
        </button>
      </div>

      {#if settings.disabled}
        <div class="p-3 bg-secondary-50 border border-secondary/30 rounded-md">
          <p class="text-sm text-gray-700">
            Auto-stop is <span class="font-semibold">off</span>. This instance keeps running until
            you stop it yourself.
          </p>
        </div>
      {:else}
        <div class="space-y-4">
          <div>
            <label for="sleep-timeout" class="block text-sm font-medium text-gray-700 mb-1">
              Stop after
            </label>
            <select
              id="sleep-timeout"
              value={selectValue}
              disabled={saving}
              onchange={(e) => onPresetChange(e.currentTarget.value)}
              class="input max-w-xs"
            >
              {#each PRESETS as p}
                <option value={String(p.minutes)}>
                  {p.minutes === 0 ? `${p.label} (${humanMinutes(settings.default_minutes)})` : p.label}
                </option>
              {/each}
              <option value="custom">Custom…</option>
            </select>
          </div>

          {#if showCustom}
            <div class="flex items-end gap-2">
              <div>
                <label for="sleep-custom" class="block text-sm font-medium text-gray-700 mb-1">
                  Minutes
                </label>
                <input
                  id="sleep-custom"
                  type="number"
                  min="1"
                  max="43200"
                  bind:value={customMinutes}
                  class="input w-32"
                />
              </div>
              <button
                class="btn-primary btn-sm"
                disabled={saving || !customMinutes || customMinutes < 1}
                onclick={() => save({ timeout_minutes: Number(customMinutes) })}
              >
                Apply
              </button>
            </div>
          {/if}

          <div class="p-4 bg-primary-50 rounded-md">
            {#if minutesLeft === null}
              <p class="text-sm text-gray-700">
                Nothing scheduled — the timer starts when the instance starts.
              </p>
            {:else if minutesLeft <= 0}
              <p class="text-sm text-gray-700">
                Due to stop — the worker sweeps every {settings.check_interval_minutes} minutes.
              </p>
            {:else}
              <p class="text-sm text-gray-900">
                Stops in <span class="font-semibold">{humanMinutes(minutesLeft)}</span>,
                around <span class="font-mono">{formatDate(settings.sleeps_at)}</span>.
              </p>
              <p class="text-xs text-gray-500 mt-1">
                Checked every {settings.check_interval_minutes} minutes, so it may run up to
                {settings.check_interval_minutes} minutes late.
              </p>
            {/if}
          </div>

          <div class="flex items-center gap-3 border-t pt-4">
            <button class="btn-outline btn-sm" disabled={saving} onclick={resetTimer}>
              Reset timer
            </button>
            <p class="text-xs text-gray-400">
              The timer counts from the last start, not from your last keystroke: SSH and the web
              terminal do not push it back. Reset it to buy another
              {humanMinutes(settings.effective_minutes)}.
            </p>
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>
