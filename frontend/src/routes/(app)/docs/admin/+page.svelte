<script lang="ts">
  const configExample = `server:
  host: "0.0.0.0"
  port: 8080
  frontend_url: "http://localhost:5173"

auth:
  entra_client_id: ""          # Optional: Microsoft Entra app ID
  entra_client_secret: ""      # Optional: Entra client secret
  entra_tenant_id: ""          # Optional: Azure AD tenant ID
  admin_password_hash: "..."   # bcrypt hash (set during setup)
  jwt_secret: "..."            # Auto-generated 32-byte hex
  jwt_lifetime_hours: 24

database:
  path: "plati.db"             # SQLite database file

secret_encryption_key: "..."   # Auto-generated 32-byte hex for AES-256-GCM

servers:                       # Incus servers list
  - name: "local"
    endpoint: "https://127.0.0.1:8443"
    tls_client_cert: "/home/user/.config/incus/client.crt"
    tls_client_key: "/home/user/.config/incus/client.key"
    max_instances: 50

sleep_timeout: "4h"            # Auto-stop after inactivity
templates_dir: "templates"     # Relative to config file directory`;

  const entraSteps = [
    'Go to the Azure Portal → Microsoft Entra ID → App registrations → New registration.',
    'Set the redirect URI to http://<your-host>:8080/auth/callback (Web type).',
    'Copy the Application (client) ID and Directory (tenant) ID.',
    'Under Certificates & secrets, create a new client secret and copy the value.',
    'Add all three values to your config file or re-run setup.',
    'Restart the Plati server for changes to take effect.',
  ];

  const incusSteps = [
    'Install Incus on the target server: sudo apt install incus (Ubuntu/Debian).',
    'Initialize Incus: sudo incus admin init (accept defaults or customize storage/network).',
    'Generate a client certificate on the Plati host: incus remote add <server> <endpoint> (follow trust prompts).',
    'The client cert/key are saved to ~/.config/incus/client.crt and client.key.',
    'Add the server to your Plati config under the servers list.',
    'Restart Plati. Check Admin → Servers to verify the connection shows "Online".',
  ];

  let copied = $state(false);

  function copyConfig() {
    navigator.clipboard.writeText(configExample);
    copied = true;
    setTimeout(() => copied = false, 2000);
  }
</script>

<div class="max-w-3xl">
  <div class="mb-6">
    <a href="/docs" class="text-sm text-indigo-600 hover:text-indigo-800">&larr; Back to Docs</a>
  </div>

  <h1 class="text-2xl font-bold mb-2">Administration</h1>
  <p class="text-gray-500 mb-8">Server configuration, user management, and system setup.</p>

  <!-- Config file -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Configuration File</h2>
    <p class="text-gray-600 text-sm mb-3">Plati is configured via <code class="bg-gray-100 px-1 rounded">config/plati.yaml</code>. The setup wizard writes this file automatically, but you can edit it directly. Restart the server after changes.</p>
    <div class="bg-white border rounded-lg overflow-hidden">
      <div class="flex items-center justify-between px-4 py-2 bg-gray-50 border-b">
        <span class="text-xs font-medium text-gray-500">config/plati.yaml</span>
        <button onclick={copyConfig} class="text-xs px-3 py-1 rounded bg-indigo-50 text-indigo-600 hover:bg-indigo-100">
          {copied ? 'Copied!' : 'Copy'}
        </button>
      </div>
      <pre class="p-4 text-xs overflow-x-auto"><code>{configExample}</code></pre>
    </div>
  </section>

  <!-- Entra setup -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Microsoft Entra ID Setup</h2>
    <p class="text-gray-600 text-sm mb-3">Entra ID (formerly Azure AD) enables Single Sign-On for your organization. This is optional — admin password login always works.</p>
    <div class="bg-white border rounded-lg p-4">
      <ol class="space-y-2 text-sm text-gray-600">
        {#each entraSteps as step, i}
          <li><span class="inline-flex items-center justify-center w-5 h-5 rounded-full bg-indigo-100 text-indigo-700 text-xs font-medium mr-2">{i + 1}</span>{step}</li>
        {/each}
      </ol>
      <div class="mt-4 bg-blue-50 border border-blue-200 rounded p-3 text-xs text-blue-800">
        <b>Required API permissions:</b> OpenID Connect scopes <code class="bg-blue-100 px-1 rounded">openid</code>, <code class="bg-blue-100 px-1 rounded">profile</code>, and <code class="bg-blue-100 px-1 rounded">email</code>. No admin consent required.
      </div>
    </div>
  </section>

  <!-- Incus server setup -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Incus Server Setup</h2>
    <p class="text-gray-600 text-sm mb-3">Plati connects to one or more Incus servers to run development instances. Each server needs TLS mutual authentication.</p>
    <div class="bg-white border rounded-lg p-4">
      <ol class="space-y-2 text-sm text-gray-600">
        {#each incusSteps as step, i}
          <li><span class="inline-flex items-center justify-center w-5 h-5 rounded-full bg-indigo-100 text-indigo-700 text-xs font-medium mr-2">{i + 1}</span>{step}</li>
        {/each}
      </ol>
      <div class="mt-4 bg-amber-50 border border-amber-200 rounded p-3 text-xs text-amber-800">
        <b>Local server shortcut:</b> If Incus runs on the same machine as Plati, the client cert is usually at <code class="bg-amber-100 px-1 rounded">~/.config/incus/client.crt</code> and <code class="bg-amber-100 px-1 rounded">~/.config/incus/client.key</code>. The endpoint is <code class="bg-amber-100 px-1 rounded">https://127.0.0.1:8443</code>.
      </div>
    </div>
  </section>

  <!-- User management -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">User Management</h2>
    <div class="bg-white border rounded-lg p-4 space-y-3 text-sm text-gray-600">
      <p>Users are created automatically when they sign in via Entra ID. The admin user (<code class="bg-gray-100 px-1 rounded">admin@plati.local</code>) is created during initial setup.</p>
      <h3 class="font-medium text-gray-700">Roles</h3>
      <ul class="space-y-1 ml-2">
        <li><span class="px-2 py-0.5 rounded text-xs bg-purple-100 text-purple-800">admin</span> — Full access: manage templates, servers, users, and all instances.</li>
        <li><span class="px-2 py-0.5 rounded text-xs bg-gray-100 text-gray-800">user</span> — Can create and manage their own instances, SSH keys, and secrets.</li>
      </ul>
      <p>Promote a user to admin from <a href="/admin" class="text-indigo-600 underline">Admin → Users</a> by editing their role.</p>
    </div>
  </section>

  <!-- Templates admin -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Managing Templates</h2>
    <div class="bg-white border rounded-lg p-4 space-y-3 text-sm text-gray-600">
      <p>Templates can be managed from <a href="/admin" class="text-indigo-600 underline">Admin → Templates</a>:</p>
      <ul class="list-disc list-inside space-y-1 ml-2">
        <li><b>Import:</b> Paste a template JSON into the import form. See <a href="/docs/templates" class="text-indigo-600 underline">Template docs</a> for the schema and examples.</li>
        <li><b>Delete:</b> Removes the template. Existing instances using it are not affected.</li>
        <li><b>Auto-import:</b> JSON files placed in the <code class="bg-gray-100 px-1 rounded">config/templates/</code> directory are imported automatically on server start (duplicates are skipped by slug).</li>
      </ul>
    </div>
  </section>

  <!-- Troubleshooting -->
  <section class="mb-10">
    <h2 class="text-xl font-semibold mb-4">Troubleshooting</h2>
    <div class="bg-white border rounded-lg p-4 space-y-4 text-sm text-gray-600">
      <div>
        <h3 class="font-medium text-gray-700">Server shows "Offline" in admin panel</h3>
        <p>Check that the Incus endpoint is reachable, the TLS cert/key paths are correct, and the client certificate is trusted by the Incus server. Restart Plati after fixing.</p>
      </div>
      <div>
        <h3 class="font-medium text-gray-700">No templates visible after login</h3>
        <p>Templates must be imported by an admin. Go to Admin → Templates and import one, or place JSON files in <code class="bg-gray-100 px-1 rounded">config/templates/</code> and restart.</p>
      </div>
      <div>
        <h3 class="font-medium text-gray-700">Can't SSH into instance</h3>
        <p>Verify your SSH key was added <b>before</b> the instance was created. If not, rebuild the instance. Also check that the instance status is "running" and has an IP address.</p>
      </div>
      <div>
        <h3 class="font-medium text-gray-700">401 errors on admin pages</h3>
        <p>Your session may have expired. Log out and log back in. If the server was recently restarted, the JWT secret may have changed, requiring a fresh login.</p>
      </div>
    </div>
  </section>
</div>
