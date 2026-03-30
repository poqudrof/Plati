<script lang="ts">
  import { currentUser } from '$lib/stores/auth';

  const sections = [
    { title: 'Templates', href: '/docs/templates', description: 'Learn how to create and configure instance templates with cloud-init, resources, and profiles.' },
    { title: 'Instances', href: '/docs/instances', description: 'Managing your development workspaces: create, start, stop, rebuild, and connect via SSH.' },
    { title: 'Settings', href: '/docs/settings', description: 'Configure SSH keys and environment secrets for your instances.' },
  ];

  const adminSections = [
    { title: 'Administration', href: '/docs/admin', description: 'Server management, user roles, template imports, and system configuration.' },
  ];
</script>

<div>
  <h1 class="text-2xl font-bold mb-2">Documentation</h1>
  <p class="text-gray-500 mb-8">Everything you need to use and manage Plati.</p>

  <div class="grid gap-4 md:grid-cols-2">
    {#each sections as section}
      <a href={section.href} class="block bg-white border rounded-lg p-6 hover:border-indigo-300 hover:shadow-sm transition">
        <h2 class="text-lg font-semibold mb-2">{section.title}</h2>
        <p class="text-sm text-gray-500">{section.description}</p>
      </a>
    {/each}
    {#if $currentUser?.role === 'admin'}
      {#each adminSections as section}
        <a href={section.href} class="block bg-white border border-purple-200 rounded-lg p-6 hover:border-purple-400 hover:shadow-sm transition">
          <h2 class="text-lg font-semibold mb-2">{section.title}</h2>
          <p class="text-sm text-gray-500">{section.description}</p>
        </a>
      {/each}
    {/if}
  </div>
</div>
