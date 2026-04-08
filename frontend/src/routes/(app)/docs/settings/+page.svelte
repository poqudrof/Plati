<script lang="ts">
</script>

<div class="max-w-3xl">
  <div class="mb-6">
    <a href="/docs" class="text-sm text-primary hover:text-primary-dark">&larr; Back to Docs</a>
  </div>

  <h1 class="text-2xl font-bold mb-2">Settings</h1>
  <p class="text-gray-500 mb-8">Configure SSH keys and environment secrets that are injected into your instances.</p>

  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">SSH Keys</h2>
    <div class="bg-white border rounded-lg p-4 space-y-3 text-sm text-gray-600">
      <p>SSH keys let you connect to your instances via SSH. Add your public key in <a href="/settings" class="text-primary underline">Settings</a>.</p>
      <h3 class="font-medium text-gray-700 mt-2">Generating a key</h3>
      <pre class="bg-gray-50 rounded p-3 text-xs font-mono">ssh-keygen -t ed25519 -C "your-email@example.com"
cat ~/.ssh/id_ed25519.pub</pre>
      <p>Copy the output and paste it as a new SSH key in Plati.</p>
      <div class="bg-amber-50 border border-amber-200 rounded p-3 text-amber-800 text-xs">
        <b>Important:</b> SSH keys are injected into instances at <b>creation time</b> via cloud-init. If you add a key after creating an instance, you need to <b>rebuild</b> that instance for the new key to take effect.
      </div>
    </div>
  </section>

  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Environment Secrets</h2>
    <div class="bg-white border rounded-lg p-4 space-y-3 text-sm text-gray-600">
      <p>Secrets are encrypted key-value pairs stored in the database. They can be used to pass API keys, tokens, or other sensitive values to your instances.</p>
      <p>Secrets are encrypted at rest using AES-256-GCM. The encryption key is stored in the server config file.</p>
      <h3 class="font-medium text-gray-700 mt-2">Example secrets</h3>
      <ul class="list-disc list-inside space-y-1 ml-2">
        <li><code class="bg-gray-100 px-1 rounded">GITHUB_TOKEN</code> — Personal access token for private repos</li>
        <li><code class="bg-gray-100 px-1 rounded">NPM_TOKEN</code> — Registry auth token</li>
        <li><code class="bg-gray-100 px-1 rounded">AWS_ACCESS_KEY_ID</code> — Cloud credentials</li>
      </ul>
    </div>
  </section>
</div>
