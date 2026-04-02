<script lang="ts">
  const referenceTemplate = `name: My Template          # Display name shown in the UI
slug: my-template          # Unique ID: lowercase letters, digits, hyphens
description: What this template provides

# Incus image. Append /cloud for cloud-init support (Ubuntu, Debian, Fedora).
image: images:ubuntu/24.04/cloud

profiles:
  - default                # Incus profiles (networking, storage, etc.)
  - docker                 # Add Docker-in-Docker support

resources:
  cpu: 2                   # vCPU cores
  memory: 4GB              # RAM limit
  disk: 20GB               # Persistent /workspace volume size

# ── Optional ────────────────────────────────────────────────────────

# Non-root user for the secondary terminal button in the UI.
terminal_user: ubuntu

# Persistence: "normal" creates volumes, "ephemeral" wipes on rebuild.
persistence:
  mode: normal
  directories:
    - path: /workspace
      size: 20GB

# Commands run only on first create (when no sentinel exists).
first_init_commands:
  - apt-get update -y
  - apt-get install -y git curl

# Commands run on rebuild (when sentinel exists).
rebuild_commands:
  - echo "Rebuild complete"

# Reuse shared mixin commands from mixins/ directory.
includes:
  - tailscale   # Tailscale VPN with auto-naming
  - sshx        # SSHX collaborative terminal
  - docker      # Docker Engine

# Cloud-init YAML — only runs on /cloud images.
cloud_init: |
  #cloud-config
  packages:
    - git
    - curl
  runcmd:
    - echo "setup complete"`;

  const minimalExample = `name: Bare Ubuntu
slug: bare-ubuntu
description: Minimal Ubuntu 24.04 with no pre-installed tools
image: images:ubuntu/24.04
profiles:
  - default
resources:
  cpu: 1
  memory: 1GB
  disk: 5GB`;

  const noCloudInitExample = `name: Alpine Dev
slug: alpine-dev
description: Alpine Linux 3.20 with git and curl
image: images:alpine/3.20
profiles:
  - default
resources:
  cpu: 1
  memory: 1GB
  disk: 10GB
post_create_commands:
  - apk add --no-cache git curl openssh-client
  - mkdir -p /workspace`;

  const cloudInitExample = `name: Python Dev
slug: python-dev
description: Python 3.12 development environment
image: images:ubuntu/24.04/cloud
profiles:
  - default
resources:
  cpu: 2
  memory: 4GB
  disk: 20GB
cloud_init: |
  #cloud-config
  packages:
    - git
    - python3
    - python3-pip
    - python3-venv
  runcmd:
    - pip3 install poetry`;

  const dockerExample = `name: Docker Dev
slug: docker-dev
description: Ubuntu 24.04 with Docker Engine
image: images:ubuntu/24.04/cloud
profiles:
  - default
  - docker          # Incus profile with security.nesting=true
resources:
  cpu: 2
  memory: 4GB
  disk: 20GB
terminal_user: ubuntu
includes:
  - docker          # Installs Docker CE via mixin
  - tailscale
  - sshx
persistence:
  mode: normal
  directories:
    - path: /workspace
      size: 20GB
first_init_commands:
  - apt-get update -y
  - apt-get install -y git curl`;

  const gpuExample = `name: Ubuntu GPU
slug: ubuntu-gpu
description: Ubuntu 24.04 with Docker and Nvidia GPU
image: images:ubuntu/24.04/cloud
profiles:
  - default
  - docker          # security.nesting for Docker
  - nvidia          # GPU passthrough (setup-gpu-profile.sh)
resources:
  cpu: 4
  memory: 8GB
  disk: 40GB
terminal_user: ubuntu
includes:
  - tailscale
  - sshx
  - docker          # Must come before nvidia
  - nvidia          # Configures Docker GPU runtime
persistence:
  mode: normal
  directories:
    - path: /workspace
      size: 40GB
first_init_commands:
  - apt-get update -y
  - apt-get install -y git curl wget build-essential`;

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
  <p class="text-gray-500 mb-3">Templates define the base configuration for development instances. Each template is a YAML file placed in the <code class="bg-gray-100 px-1 rounded">templates/</code> directory configured in <code class="bg-gray-100 px-1 rounded">plati.yaml</code>. Templates are loaded at startup and shown in the instance creation dialog.</p>
  <p class="text-gray-500 mb-8">Images come from the Incus public <code class="bg-gray-100 px-1 rounded">images:</code> remote. Common choices: <code class="bg-gray-100 px-1 rounded">images:ubuntu/24.04</code>, <code class="bg-gray-100 px-1 rounded">images:debian/12</code>, <code class="bg-gray-100 px-1 rounded">images:alpine/3.20</code>. Check <b>Admin &gt; Images</b> for everything available on your server.</p>

  <!-- Initialization Order -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-3">Initialization Order</h2>
    <p class="text-gray-600 text-sm mb-4">Every instance goes through two phases after creation:</p>
    <div class="space-y-3">
      <div class="bg-white border rounded-lg p-4">
        <h3 class="text-sm font-semibold mb-1">Phase 1 — cloud-init <span class="font-normal text-gray-500">(only on <code class="bg-gray-100 px-1 rounded">/cloud</code> images)</span></h3>
        <p class="text-sm text-gray-600">Runs on first boot via the cloud-init daemon. Handles package install, <code class="bg-gray-100 px-1 rounded">runcmd</code>, <code class="bg-gray-100 px-1 rounded">write_files</code>, and user creation. Not available on Alpine or other non-cloud images.</p>
      </div>
      <div class="bg-white border rounded-lg p-4">
        <h3 class="text-sm font-semibold mb-1">Phase 2 — incus exec <span class="font-normal text-gray-500">(all images)</span></h3>
        <p class="text-sm text-gray-600 mb-2">Runs after the instance starts, always, regardless of image type. Injects:</p>
        <ul class="space-y-1 text-sm text-gray-600 list-disc list-inside">
          <li>SSH <code class="bg-gray-100 px-1 rounded">authorized_keys</code> for root (and <code class="bg-gray-100 px-1 rounded">terminal_user</code> if set)</li>
          <li>Private key files into <code class="bg-gray-100 px-1 rounded">~/.ssh/</code></li>
          <li>Secrets as env vars in <code class="bg-gray-100 px-1 rounded">/etc/profile.d/plati-env.sh</code></li>
          <li><code class="bg-gray-100 px-1 rounded">post_create_commands</code> — your custom shell commands</li>
        </ul>
      </div>
    </div>
    <p class="text-xs text-gray-400 mt-3">Use <code class="bg-gray-100 px-1 rounded">post_create_commands</code> for setup that must work on all images (including Alpine). Use <code class="bg-gray-100 px-1 rounded">cloud_init</code> when you need first-boot ordering guarantees or <code class="bg-gray-100 px-1 rounded">write_files</code>.</p>
  </section>

  <!-- Mixins -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-3">Mixins</h2>
    <p class="text-gray-600 text-sm mb-4">Mixins are reusable blocks of <code class="bg-gray-100 px-1 rounded">post_create_commands</code> shared across templates. Instead of copy-pasting install steps, declare a mixin with <code class="bg-gray-100 px-1 rounded">includes</code> and the commands are merged in at import time.</p>
    <div class="bg-white border rounded-lg p-4 space-y-3 text-sm text-gray-600">
      <p><b>Location.</b> Mixin files live in the <code class="bg-gray-100 px-1 rounded">mixins/</code> subdirectory of your templates directory (e.g. <code class="bg-gray-100 px-1 rounded">config/templates/mixins/tailscale.yaml</code>). Each file has a <code class="bg-gray-100 px-1 rounded">name</code> and a <code class="bg-gray-100 px-1 rounded">post_create_commands</code> list.</p>
      <p><b>Include order.</b> Mixin commands are prepended to the template's own <code class="bg-gray-100 px-1 rounded">post_create_commands</code>, in the order listed under <code class="bg-gray-100 px-1 rounded">includes</code>. The template's own commands always run last.</p>
      <p><b>Resolved at import time.</b> Mixins are merged when templates are loaded at startup. The database stores the fully-merged command list — no mixin concept leaks into the instance creation path.</p>
      <p><b>Built-in mixins.</b> <code class="bg-gray-100 px-1 rounded">tailscale</code> — installs Tailscaled and calls <code class="bg-gray-100 px-1 rounded">tailscale up</code> with the instance name as hostname. <code class="bg-gray-100 px-1 rounded">sshx</code> — installs the SSHX binary and registers it as a systemd service. <code class="bg-gray-100 px-1 rounded">docker</code> — installs Docker CE from the official repo. <code class="bg-gray-100 px-1 rounded">nvidia</code> — installs nvidia-container-toolkit and configures the Docker GPU runtime (must be listed after <code class="bg-gray-100 px-1 rounded">docker</code>).</p>
    </div>
  </section>

  <!-- Template Reference -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-3">Template Reference</h2>
    <p class="text-gray-600 text-sm mb-3">All available fields with inline descriptions. Only the first six fields (<code class="bg-gray-100 px-1 rounded">name</code> through <code class="bg-gray-100 px-1 rounded">resources</code>) are required.</p>
    <div class="bg-white border rounded-lg overflow-hidden">
      <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
        <span class="text-sm font-medium text-gray-700">all-fields.yaml</span>
        <button onclick={() => copyToClipboard(referenceTemplate, 'reference')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
          {copied === 'reference' ? 'Copied!' : 'Copy YAML'}
        </button>
      </div>
      <pre class="p-4 text-xs overflow-x-auto"><code>{referenceTemplate}</code></pre>
    </div>
  </section>

  <!-- Examples -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-3">Examples</h2>

    <div class="space-y-6">
      <!-- Minimum -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
          <div>
            <h3 class="font-medium text-sm">Minimum</h3>
            <p class="text-xs text-gray-500 mt-0.5">Only required fields — no provisioning</p>
          </div>
          <button onclick={() => copyToClipboard(minimalExample, 'minimal')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
            {copied === 'minimal' ? 'Copied!' : 'Copy YAML'}
          </button>
        </div>
        <pre class="p-4 text-xs overflow-x-auto"><code>{minimalExample}</code></pre>
      </div>

      <!-- Without cloud-init -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
          <div>
            <h3 class="font-medium text-sm">Without cloud-init</h3>
            <p class="text-xs text-gray-500 mt-0.5">Alpine Linux — uses <code class="bg-gray-100 px-1 rounded">post_create_commands</code> for setup</p>
          </div>
          <button onclick={() => copyToClipboard(noCloudInitExample, 'noci')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
            {copied === 'noci' ? 'Copied!' : 'Copy YAML'}
          </button>
        </div>
        <pre class="p-4 text-xs overflow-x-auto"><code>{noCloudInitExample}</code></pre>
      </div>

      <!-- With cloud-init -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
          <div>
            <h3 class="font-medium text-sm">With cloud-init</h3>
            <p class="text-xs text-gray-500 mt-0.5">Ubuntu <code class="bg-gray-100 px-1 rounded">/cloud</code> image — packages and commands on first boot</p>
          </div>
          <button onclick={() => copyToClipboard(cloudInitExample, 'ci')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
            {copied === 'ci' ? 'Copied!' : 'Copy YAML'}
          </button>
        </div>
        <pre class="p-4 text-xs overflow-x-auto"><code>{cloudInitExample}</code></pre>
      </div>

      <!-- Docker -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
          <div>
            <h3 class="font-medium text-sm">With Docker</h3>
            <p class="text-xs text-gray-500 mt-0.5">Docker Engine via mixin + <code class="bg-gray-100 px-1 rounded">docker</code> profile</p>
          </div>
          <button onclick={() => copyToClipboard(dockerExample, 'docker')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
            {copied === 'docker' ? 'Copied!' : 'Copy YAML'}
          </button>
        </div>
        <pre class="p-4 text-xs overflow-x-auto"><code>{dockerExample}</code></pre>
      </div>

      <!-- GPU -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 bg-gray-50 border-b">
          <div>
            <h3 class="font-medium text-sm">With Nvidia GPU</h3>
            <p class="text-xs text-gray-500 mt-0.5">GPU passthrough — requires <code class="bg-gray-100 px-1 rounded">setup-gpu-profile.sh</code> on the host</p>
          </div>
          <button onclick={() => copyToClipboard(gpuExample, 'gpu')} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
            {copied === 'gpu' ? 'Copied!' : 'Copy YAML'}
          </button>
        </div>
        <pre class="p-4 text-xs overflow-x-auto"><code>{gpuExample}</code></pre>
      </div>
    </div>
  </section>

  <!-- Tips -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-3">Tips</h2>
    <div class="bg-white border rounded-lg p-4 space-y-3 text-sm text-gray-600">
      <p><b>Slug must be unique.</b> If you add a template with a slug that already exists, the duplicate will be skipped on reload. Rename the slug or delete the conflicting file to resolve.</p>
      <p><b>Keep cloud-init fast.</b> Long cloud-init scripts delay instance startup. For heavy toolchains, consider building a custom Incus image instead.</p>
      <p><b>Persistence modes.</b> Use <code class="bg-gray-100 px-1 rounded">persistence.mode: normal</code> (default) for persistent workspace volumes that survive rebuilds. Use <code class="bg-gray-100 px-1 rounded">ephemeral</code> for throwaway instances where all storage is wiped on rebuild.</p>
      <p><b>Workspace volume.</b> In <code class="bg-gray-100 px-1 rounded">normal</code> mode, persistent volumes are mounted per the <code class="bg-gray-100 px-1 rounded">persistence.directories</code> list. A sentinel file distinguishes first init from rebuild.</p>
      <p><b>Profiles.</b> Use Incus profiles to configure networking, storage pools, and device passthrough. The <code class="bg-gray-100 px-1 rounded">default</code> profile provides a bridged network. Add <code class="bg-gray-100 px-1 rounded">docker</code> for Docker support and <code class="bg-gray-100 px-1 rounded">nvidia</code> for GPU passthrough.</p>
      <p><b>GPU setup.</b> Run <code class="bg-gray-100 px-1 rounded">scripts/setup-gpu-profile.sh</code> on the Incus host to create the <code class="bg-gray-100 px-1 rounded">nvidia</code> profile, then use it in your template.</p>
    </div>
  </section>
</div>
