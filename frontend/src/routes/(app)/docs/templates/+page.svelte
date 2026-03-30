<script lang="ts">
  const pythonExample = `{
  "name": "Python Development",
  "slug": "python-dev",
  "description": "Python 3.12 development environment with common tools",
  "image": "images:ubuntu/24.04",
  "profiles": ["default"],
  "resources": {
    "cpu": 2,
    "memory": "4GB",
    "disk": "20GB"
  },
  "cloud_init": "#cloud-config\\npackages:\\n  - git\\n  - curl\\n  - build-essential\\n  - python3.12\\n  - python3.12-venv\\n  - python3-pip\\nruncmd:\\n  - update-alternatives --install /usr/bin/python python /usr/bin/python3.12 1\\n  - pip install poetry\\n"
}`;

  const minimalExample = `{
  "name": "Minimal Ubuntu",
  "slug": "minimal",
  "description": "Bare Ubuntu 24.04 with just git and curl",
  "image": "images:ubuntu/24.04",
  "profiles": ["default"],
  "resources": {
    "cpu": 1,
    "memory": "2GB",
    "disk": "10GB"
  },
  "cloud_init": "#cloud-config\\npackages:\\n  - git\\n  - curl\\n"
}`;

  const nodeExample = `{
  "name": "Node.js Development",
  "slug": "node-dev",
  "description": "Node.js 22 LTS development environment",
  "image": "images:ubuntu/24.04",
  "profiles": ["default"],
  "resources": {
    "cpu": 2,
    "memory": "4GB",
    "disk": "20GB"
  },
  "cloud_init": "#cloud-config\\npackages:\\n  - git\\n  - curl\\n  - build-essential\\nruncmd:\\n  - curl -fsSL https://deb.nodesource.com/setup_22.x | bash -\\n  - apt-get install -y nodejs\\n  - npm install -g pnpm\\n"
}`;

  const golangExample = `{
  "name": "Go Development",
  "slug": "go-dev",
  "description": "Go 1.23 development environment",
  "image": "images:ubuntu/24.04",
  "profiles": ["default"],
  "resources": {
    "cpu": 2,
    "memory": "4GB",
    "disk": "20GB"
  },
  "cloud_init": "#cloud-config\\npackages:\\n  - git\\n  - curl\\n  - build-essential\\nruncmd:\\n  - curl -fsSL https://go.dev/dl/go1.23.4.linux-amd64.tar.gz | tar -C /usr/local -xzf -\\n  - echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh\\n"
}`;

  let copied = $state('');

  function copyToClipboard(text: string, label: string) {
    navigator.clipboard.writeText(text);
    copied = label;
    setTimeout(() => copied = '', 2000);
  }
</script>

<div class="max-w-3xl">
  <div class="mb-6">
    <a href="/docs" class="text-sm text-indigo-600 hover:text-indigo-800">&larr; Back to Docs</a>
  </div>

  <h1 class="text-2xl font-bold mb-2">Templates</h1>
  <p class="text-gray-500 mb-8">Templates define the base configuration for development instances. Each template specifies an OS image, resources, and a cloud-init script for automated provisioning.</p>

  <!-- Schema reference -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Template Schema</h2>
    <div class="bg-white border rounded-lg overflow-hidden">
      <table class="w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="text-left px-4 py-3 font-medium text-gray-600">Field</th>
            <th class="text-left px-4 py-3 font-medium text-gray-600">Type</th>
            <th class="text-left px-4 py-3 font-medium text-gray-600">Description</th>
          </tr>
        </thead>
        <tbody class="divide-y">
          <tr><td class="px-4 py-2 font-mono text-indigo-700">name</td><td class="px-4 py-2">string</td><td class="px-4 py-2 text-gray-600">Display name shown in the UI</td></tr>
          <tr><td class="px-4 py-2 font-mono text-indigo-700">slug</td><td class="px-4 py-2">string</td><td class="px-4 py-2 text-gray-600">Unique identifier (lowercase, hyphens). Must be unique across all templates</td></tr>
          <tr><td class="px-4 py-2 font-mono text-indigo-700">description</td><td class="px-4 py-2">string</td><td class="px-4 py-2 text-gray-600">Short description shown on the template card</td></tr>
          <tr><td class="px-4 py-2 font-mono text-indigo-700">image</td><td class="px-4 py-2">string</td><td class="px-4 py-2 text-gray-600">Incus image reference (e.g. <code class="bg-gray-100 px-1 rounded">images:ubuntu/24.04</code>)</td></tr>
          <tr><td class="px-4 py-2 font-mono text-indigo-700">profiles</td><td class="px-4 py-2">string[]</td><td class="px-4 py-2 text-gray-600">Incus profiles to apply. Usually <code class="bg-gray-100 px-1 rounded">["default"]</code></td></tr>
          <tr><td class="px-4 py-2 font-mono text-indigo-700">resources.cpu</td><td class="px-4 py-2">number</td><td class="px-4 py-2 text-gray-600">Number of CPU cores</td></tr>
          <tr><td class="px-4 py-2 font-mono text-indigo-700">resources.memory</td><td class="px-4 py-2">string</td><td class="px-4 py-2 text-gray-600">Memory limit (e.g. <code class="bg-gray-100 px-1 rounded">4GB</code>)</td></tr>
          <tr><td class="px-4 py-2 font-mono text-indigo-700">resources.disk</td><td class="px-4 py-2">string</td><td class="px-4 py-2 text-gray-600">Workspace volume size (e.g. <code class="bg-gray-100 px-1 rounded">20GB</code>)</td></tr>
          <tr><td class="px-4 py-2 font-mono text-indigo-700">cloud_init</td><td class="px-4 py-2">string</td><td class="px-4 py-2 text-gray-600">Cloud-init YAML for provisioning packages, running commands, etc.</td></tr>
        </tbody>
      </table>
    </div>
  </section>

  <!-- Images -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Available Images</h2>
    <p class="text-gray-600 text-sm mb-3">Incus supports images from the public <code class="bg-gray-100 px-1 rounded">images:</code> remote. Common choices:</p>
    <div class="bg-white border rounded-lg p-4">
      <ul class="space-y-2 text-sm">
        <li><code class="bg-gray-100 px-2 py-0.5 rounded">images:ubuntu/24.04</code> — Ubuntu 24.04 LTS (recommended)</li>
        <li><code class="bg-gray-100 px-2 py-0.5 rounded">images:ubuntu/22.04</code> — Ubuntu 22.04 LTS</li>
        <li><code class="bg-gray-100 px-2 py-0.5 rounded">images:debian/12</code> — Debian 12 Bookworm</li>
        <li><code class="bg-gray-100 px-2 py-0.5 rounded">images:fedora/40</code> — Fedora 40</li>
        <li><code class="bg-gray-100 px-2 py-0.5 rounded">images:alpine/3.20</code> — Alpine Linux 3.20</li>
      </ul>
      <p class="text-xs text-gray-400 mt-3">The full list depends on your Incus server's configured remotes. Check <b>Admin &gt; Images</b> for available images on your server.</p>
    </div>
  </section>

  <!-- Cloud-init -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Cloud-init</h2>
    <p class="text-gray-600 text-sm mb-3">The <code class="bg-gray-100 px-1 rounded">cloud_init</code> field accepts standard <a href="https://cloudinit.readthedocs.io/" class="text-indigo-600 underline" target="_blank" rel="noopener">cloud-init</a> YAML. It runs on first boot to install packages, create files, and execute setup commands. In the JSON template, newlines are escaped as <code class="bg-gray-100 px-1 rounded">\n</code>.</p>
    <div class="bg-white border rounded-lg p-4">
      <h3 class="text-sm font-medium mb-2">Common cloud-init modules</h3>
      <ul class="space-y-1 text-sm text-gray-600">
        <li><code class="bg-gray-100 px-1 rounded">packages</code> — List of apt/dnf packages to install</li>
        <li><code class="bg-gray-100 px-1 rounded">runcmd</code> — Shell commands to run (in order, as root)</li>
        <li><code class="bg-gray-100 px-1 rounded">write_files</code> — Create files with specific content and permissions</li>
        <li><code class="bg-gray-100 px-1 rounded">snap</code> — Install snap packages</li>
      </ul>
      <p class="text-xs text-gray-400 mt-3">Note: Plati automatically injects user SSH keys via cloud-init. You don't need to handle SSH key setup in your template.</p>
    </div>
  </section>

  <!-- Examples -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Examples</h2>
    <p class="text-gray-600 text-sm mb-4">Copy any example below and paste it into <b>Admin &gt; Templates &gt; Import Template (JSON)</b> to add it.</p>

    <div class="space-y-6">
      <!-- Minimal -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
          <h3 class="font-medium text-sm">Minimal Ubuntu</h3>
          <button onclick={() => copyToClipboard(minimalExample, 'minimal')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
            {copied === 'minimal' ? 'Copied!' : 'Copy JSON'}
          </button>
        </div>
        <pre class="p-4 text-xs overflow-x-auto"><code>{minimalExample}</code></pre>
      </div>

      <!-- Python -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
          <h3 class="font-medium text-sm">Python Development</h3>
          <button onclick={() => copyToClipboard(pythonExample, 'python')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
            {copied === 'python' ? 'Copied!' : 'Copy JSON'}
          </button>
        </div>
        <pre class="p-4 text-xs overflow-x-auto"><code>{pythonExample}</code></pre>
      </div>

      <!-- Node.js -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
          <h3 class="font-medium text-sm">Node.js Development</h3>
          <button onclick={() => copyToClipboard(nodeExample, 'node')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
            {copied === 'node' ? 'Copied!' : 'Copy JSON'}
          </button>
        </div>
        <pre class="p-4 text-xs overflow-x-auto"><code>{nodeExample}</code></pre>
      </div>

      <!-- Go -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
          <h3 class="font-medium text-sm">Go Development</h3>
          <button onclick={() => copyToClipboard(golangExample, 'golang')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
            {copied === 'golang' ? 'Copied!' : 'Copy JSON'}
          </button>
        </div>
        <pre class="p-4 text-xs overflow-x-auto"><code>{golangExample}</code></pre>
      </div>
    </div>
  </section>

  <!-- Tips -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Tips</h2>
    <div class="bg-white border rounded-lg p-4 space-y-3 text-sm text-gray-600">
      <p><b>Slug must be unique.</b> If you import a template with a slug that already exists, the import will be skipped. Delete the existing one first to re-import.</p>
      <p><b>Keep cloud-init fast.</b> Long cloud-init scripts delay instance startup. For heavy toolchains, consider building a custom Incus image instead.</p>
      <p><b>Workspace volume.</b> Every instance gets a persistent volume mounted at <code class="bg-gray-100 px-1 rounded">/workspace</code>. The <code class="bg-gray-100 px-1 rounded">resources.disk</code> field sets its size. This volume survives rebuilds.</p>
      <p><b>Profiles.</b> Use Incus profiles to configure networking, storage pools, and device passthrough. The <code class="bg-gray-100 px-1 rounded">default</code> profile usually provides a bridged network and root disk.</p>
    </div>
  </section>
</div>
