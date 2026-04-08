<script lang="ts">
  import type { MixinInfo } from '$lib/api/types';

  interface PersistenceDir { path: string; size: string; pool?: string; }
  interface RepoRef { name: string; dest: string; }
  interface TailscaleServe { port: number; funnel?: boolean; }

  type StepCategory = 'platform' | 'ssh' | 'secrets' | 'mixin' | 'repos' | 'command' | 'sentinel';

  interface Step {
    category: StepCategory;
    label: string;
  }

  let {
    image = '',
    profiles = [],
    resources = {} as Record<string, any>,
    terminalUser = '',
    persistenceMode = 'normal',
    persistenceDirs = [],
    includes = [],
    mixins = [],
    firstInitCommands = [],
    rebuildCommands = [],
    repos = [],
    tailscaleServe = null as TailscaleServe | null,
  }: {
    image: string;
    profiles: string[];
    resources: Record<string, any>;
    terminalUser: string;
    persistenceMode: string;
    persistenceDirs: PersistenceDir[];
    includes: string[];
    mixins: MixinInfo[];
    firstInitCommands: string[];
    rebuildCommands: string[];
    repos: RepoRef[];
    tailscaleServe: TailscaleServe | null;
  } = $props();

  let tab: 'create' | 'rebuild' = $state('create');

  const categoryStyle: Record<StepCategory, { bg: string; text: string; border: string; badge: string }> = {
    platform: { bg: 'bg-slate-50', text: 'text-slate-700', border: 'border-slate-200', badge: 'PLATFORM' },
    ssh:      { bg: 'bg-blue-50', text: 'text-blue-700', border: 'border-blue-200', badge: 'SSH' },
    secrets:  { bg: 'bg-amber-50', text: 'text-amber-700', border: 'border-amber-200', badge: 'SECRETS' },
    mixin:    { bg: 'bg-purple-50', text: 'text-purple-700', border: 'border-purple-200', badge: 'MIXIN' },
    repos:    { bg: 'bg-teal-50', text: 'text-teal-700', border: 'border-teal-200', badge: 'REPOS' },
    command:  { bg: 'bg-green-50', text: 'text-green-700', border: 'border-green-200', badge: 'COMMAND' },
    sentinel: { bg: 'bg-gray-50', text: 'text-gray-500', border: 'border-gray-200', badge: 'SENTINEL' },
  };

  function buildSSHSteps(user: string, isRoot: boolean): Step[] {
    const steps: Step[] = [];
    const who = isRoot ? 'root' : user;
    if (!isRoot) {
      steps.push({ category: 'ssh', label: 'Wait for cloud-init to finish' });
    }
    steps.push({ category: 'ssh', label: `Setup SSH keys for ${who} (authorized_keys + private keys + config)` });
    return steps;
  }

  let createSteps = $derived.by(() => {
    const steps: Step[] = [];

    // 1. Platform: create instance
    const profileStr = profiles.length > 0 ? profiles.join(', ') : 'default';
    const resStr = [resources.cpu && `${resources.cpu} CPU`, resources.memory].filter(Boolean).join(', ');
    steps.push({ category: 'platform', label: `Create instance (${image || 'no image'}) — profiles: [${profileStr}]${resStr ? ` — ${resStr}` : ''}` });

    // 2. Volumes
    if (persistenceMode !== 'ephemeral') {
      for (const dir of persistenceDirs) {
        steps.push({ category: 'platform', label: `Create + attach volume at ${dir.path} (${dir.size})` });
      }
    }

    // 3. Repos bind-mount
    if (repos.length > 0) {
      steps.push({ category: 'platform', label: 'Attach host repos directory as bind-mount' });
    }

    // 4. Start
    steps.push({ category: 'platform', label: 'Start instance + assign IP address' });

    // 5-7. SSH
    steps.push(...buildSSHSteps('root', true));
    if (terminalUser) {
      steps.push(...buildSSHSteps(terminalUser, false));
    }

    // 8. Secrets
    steps.push({ category: 'secrets', label: 'Write environment variables to /etc/profile.d/plati-env.sh' });

    // 9. Mixin file pushes
    const includedMixins = includes.map(name => mixins.find(m => m.name === name)).filter(Boolean) as MixinInfo[];
    for (const m of includedMixins) {
      for (const f of m.files) {
        steps.push({ category: 'mixin', label: `Push ${f.src} \u2192 ${f.dest} (${f.mode})` });
      }
    }

    // 10. Repo copies
    for (const repo of repos) {
      steps.push({ category: 'repos', label: `Copy repo ${repo.name} \u2192 ${repo.dest}` });
    }

    // 11. Mixin commands
    for (const m of includedMixins) {
      for (const cmd of m.commands) {
        const label = cmd.length > 80 ? cmd.slice(0, 77) + '...' : cmd;
        steps.push({ category: 'command', label: `[${m.name}] ${label}` });
      }
    }

    // 12. First init commands
    for (const cmd of firstInitCommands) {
      const label = cmd.length > 80 ? cmd.slice(0, 77) + '...' : cmd;
      steps.push({ category: 'command', label });
    }

    // 13. Tailscale serve
    if (tailscaleServe && tailscaleServe.port > 0) {
      steps.push({ category: 'command', label: 'Wait for Tailscale to connect' });
      const mode = tailscaleServe.funnel ? 'funnel' : 'serve';
      steps.push({ category: 'command', label: `tailscale ${mode} --bg https+insecure://localhost:${tailscaleServe.port}` });
    }

    // 14. Sentinel
    if (persistenceMode !== 'ephemeral' && persistenceDirs.length > 0) {
      steps.push({ category: 'sentinel', label: `Touch ${persistenceDirs[0].path}/.plati-initialized` });
    }

    return steps;
  });

  let rebuildSteps = $derived.by(() => {
    const steps: Step[] = [];

    // SSH
    steps.push(...buildSSHSteps('root', true));
    if (terminalUser) {
      steps.push(...buildSSHSteps(terminalUser, false));
    }

    // Secrets
    steps.push({ category: 'secrets', label: 'Write environment variables to /etc/profile.d/plati-env.sh' });

    // Rebuild commands
    for (const cmd of rebuildCommands) {
      const label = cmd.length > 80 ? cmd.slice(0, 77) + '...' : cmd;
      steps.push({ category: 'command', label });
    }

    return steps;
  });

  let activeSteps = $derived(tab === 'create' ? createSteps : rebuildSteps);
</script>

<div class="space-y-3">
  <!-- Tabs -->
  <div class="flex border-b border-gray-200">
    <button
      type="button"
      class="px-3 py-1.5 text-xs font-medium border-b-2 transition-colors {tab === 'create' ? 'border-primary text-primary' : 'border-transparent text-gray-500 hover:text-gray-700'}"
      onclick={() => tab = 'create'}
    >
      First Create
    </button>
    <button
      type="button"
      class="px-3 py-1.5 text-xs font-medium border-b-2 transition-colors {tab === 'rebuild' ? 'border-primary text-primary' : 'border-transparent text-gray-500 hover:text-gray-700'}"
      onclick={() => tab = 'rebuild'}
    >
      Rebuild
    </button>
  </div>

  <!-- Timeline -->
  <div class="relative">
    <!-- Vertical line -->
    <div class="absolute left-3 top-3 bottom-3 w-px bg-gray-200"></div>

    <div class="space-y-1.5">
      {#each activeSteps as step, i}
        {@const style = categoryStyle[step.category]}
        <div class="flex items-start gap-2.5 relative">
          <!-- Step number circle -->
          <div class="w-6 h-6 rounded-full {style.bg} border {style.border} flex items-center justify-center shrink-0 z-10">
            <span class="text-[10px] font-medium {style.text}">{i + 1}</span>
          </div>
          <!-- Step card -->
          <div class="flex-1 min-w-0 {style.bg} border {style.border} rounded px-2.5 py-1.5">
            <div class="flex items-center gap-1.5">
              <span class="text-[10px] font-semibold uppercase {style.text} shrink-0">{style.badge}</span>
              <span class="text-xs text-gray-700 break-all">{step.label}</span>
            </div>
          </div>
        </div>
      {/each}
    </div>
  </div>

  <p class="text-[10px] text-gray-400 text-center">
    {activeSteps.length} step{activeSteps.length !== 1 ? 's' : ''} total
  </p>
</div>
