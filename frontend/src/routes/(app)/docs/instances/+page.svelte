<script lang="ts">
</script>

<div class="max-w-3xl">
  <div class="mb-6">
    <a href="/docs" class="text-sm text-primary hover:text-primary-dark">&larr; Back to Docs</a>
  </div>

  <h1 class="text-2xl font-bold mb-2">Instances</h1>
  <p class="text-gray-500 mb-8">Instances are your personal development workspaces running as Incus containers on a remote server.</p>

  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Lifecycle</h2>
    <div class="bg-white border rounded-lg p-4">
      <ol class="space-y-3 text-sm text-gray-600">
        <li><b>1. Create</b> — Pick a name and a template. Plati selects a server with capacity, creates the persistent volume(s) the template declares, launches the container, and injects your SSH keys.</li>
        <li><b>2. Running</b> — The instance is up and reachable via SSH. Your files live in your home directory (<code class="bg-gray-100 px-1 rounded">/home/ubuntu</code> on most templates), which is the persistent volume.</li>
        <li><b>3. Stop</b> — Shuts the instance down gracefully. The persistent volume is preserved. Instances are auto-stopped after the configured inactivity timeout (default 4 hours).</li>
        <li><b>4. Start</b> — Boots a stopped instance back up. All data on the persistent volume is intact.</li>
        <li><b>5. Rebuild</b> — Destroys and recreates the container from the same template, but <b>keeps the persistent volume</b>. Useful to get a fresh OS while keeping your project files.</li>
        <li><b>6. Delete</b> — Permanently removes the instance <b>and its persistent volume</b>. This is irreversible.</li>
      </ol>
    </div>
  </section>

  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Connecting via SSH</h2>
    <div class="bg-white border rounded-lg p-4 space-y-3 text-sm text-gray-600">
      <p>Once your instance is running and has an IP address, connect using:</p>
      <pre class="bg-gray-50 rounded p-3 text-xs font-mono">ssh ubuntu@&lt;instance-ip&gt;</pre>
      <p>The exact command is shown on the instance detail page. For it to work:</p>
      <ul class="list-disc list-inside space-y-1 ml-2">
        <li>Add your SSH public key in <a href="/settings" class="text-primary underline">Settings</a> <b>before</b> creating the instance (keys are injected at creation time).</li>
        <li>If you added a key after creating the instance, <b>rebuild</b> it to inject the new key.</li>
        <li>The default user depends on the image. Ubuntu images use <code class="bg-gray-100 px-1 rounded">ubuntu</code>, Debian uses <code class="bg-gray-100 px-1 rounded">debian</code>, etc.</li>
      </ul>
    </div>
  </section>

  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Status Reference</h2>
    <div class="bg-white border rounded-lg overflow-hidden">
      <table class="w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="text-left px-4 py-3 font-medium text-gray-600">Status</th>
            <th class="text-left px-4 py-3 font-medium text-gray-600">Meaning</th>
          </tr>
        </thead>
        <tbody class="divide-y">
          <tr>
            <td class="px-4 py-2"><span class="px-2 py-0.5 rounded text-xs bg-yellow-100 text-yellow-800">creating</span></td>
            <td class="px-4 py-2 text-gray-600">Instance is being provisioned.</td>
          </tr>
          <tr>
            <td class="px-4 py-2"><span class="px-2 py-0.5 rounded text-xs bg-green-100 text-green-800">running</span></td>
            <td class="px-4 py-2 text-gray-600">Instance is up and reachable.</td>
          </tr>
          <tr>
            <td class="px-4 py-2"><span class="px-2 py-0.5 rounded text-xs bg-gray-100 text-gray-800">stopped</span></td>
            <td class="px-4 py-2 text-gray-600">Instance is shut down. Start it to resume.</td>
          </tr>
          <tr>
            <td class="px-4 py-2"><span class="px-2 py-0.5 rounded text-xs bg-red-100 text-red-800">error</span></td>
            <td class="px-4 py-2 text-gray-600">Something went wrong. Check server logs or try rebuilding.</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</div>
