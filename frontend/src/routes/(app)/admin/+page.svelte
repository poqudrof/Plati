<script lang="ts">
  import { browser } from '$app/environment';
  import { admin, templates as templatesApi } from '$lib/api';
  import type { Template, Server, User, ManagedSSHKey, GeneratedManagedKeyResult } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let tab: 'templates' | 'servers' | 'users' | 'ssh-keys' = $state('templates');
  let templateList: Template[] = $state([]);
  let serverList: Server[] = $state([]);
  let userList: User[] = $state([]);
  let importJson = $state('');

  // Template editing
  let editingTemplate: Template | null = $state(null);
  let expandedTemplate: number | null = $state(null);

  // User editing
  let editingUser: { id: number; name: string; role: string } | null = $state(null);

  // Managed SSH Keys
  let managedKeyList: ManagedSSHKey[] = $state([]);
  let newKeyName = $state('');
  let generatedKey: GeneratedManagedKeyResult | null = $state(null);
  let assigningKey: { id: number; name: string; assignedIDs: number[] } | null = $state(null);

  async function loadTemplates() {
    try { templateList = await templatesApi.list(); } catch (e: any) { addNotification('error', 'Failed to load templates: ' + e.message); }
  }
  async function loadServers() {
    try { serverList = await admin.servers.list(); } catch (e: any) { addNotification('error', 'Failed to load servers: ' + e.message); }
  }
  async function loadUsers() {
    try { userList = await admin.users.list(); } catch (e: any) { addNotification('error', 'Failed to load users: ' + e.message); }
  }

  // Template actions
  async function deleteTemplate(id: number) {
    if (!confirm('Delete this template?')) return;
    try {
      await admin.templates.delete(id);
      await loadTemplates();
      addNotification('success', 'Template deleted');
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function importTemplate() {
    if (!importJson) return;
    try {
      await admin.templates.import(JSON.parse(importJson));
      importJson = '';
      await loadTemplates();
      addNotification('success', 'Template imported');
    } catch (e: any) { addNotification('error', e.message); }
  }

  function startEditTemplate(tmpl: Template) {
    editingTemplate = { ...tmpl };
  }

  async function saveTemplate() {
    if (!editingTemplate) return;
    try {
      await admin.templates.update(editingTemplate.id, editingTemplate);
      editingTemplate = null;
      await loadTemplates();
      addNotification('success', 'Template updated');
    } catch (e: any) { addNotification('error', e.message); }
  }

  // User actions
  function startEditUser(user: User) {
    editingUser = { id: user.id, name: user.name, role: user.role };
  }

  async function saveUser() {
    if (!editingUser) return;
    try {
      await admin.users.update(editingUser.id, editingUser.name, editingUser.role);
      editingUser = null;
      await loadUsers();
      addNotification('success', 'User updated');
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function deleteUser(user: User) {
    if (!confirm(`Delete user "${user.email}"? This cannot be undone.`)) return;
    try {
      await admin.users.delete(user.id);
      await loadUsers();
      addNotification('success', 'User deleted');
    } catch (e: any) { addNotification('error', e.message); }
  }

  function parseResources(resources: string): Record<string, string> {
    try { return JSON.parse(resources); } catch { return {}; }
  }

  function parseProfiles(profiles: string): string[] {
    try { return JSON.parse(profiles); } catch { return []; }
  }

  async function loadManagedKeys() {
    try { managedKeyList = await admin.managedKeys.list(); } catch (e: any) { addNotification('error', e.message); }
  }

  async function generateManagedKey() {
    if (!newKeyName.trim()) return;
    try {
      generatedKey = await admin.managedKeys.generate(newKeyName.trim());
      newKeyName = '';
      await loadManagedKeys();
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function deleteManagedKey(id: number, name: string) {
    if (!confirm(`Delete managed key "${name}"? Instances using it will lose git access on next rebuild.`)) return;
    try {
      await admin.managedKeys.delete(id);
      await loadManagedKeys();
      addNotification('success', 'Managed key deleted');
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function openAssignUsers(key: ManagedSSHKey) {
    const res = await admin.managedKeys.getUsers(key.id);
    assigningKey = { id: key.id, name: key.name, assignedIDs: res.user_ids };
  }

  async function saveAssignments() {
    if (!assigningKey) return;
    try {
      await admin.managedKeys.setUsers(assigningKey.id, assigningKey.assignedIDs);
      assigningKey = null;
      addNotification('success', 'User assignments updated');
    } catch (e: any) { addNotification('error', e.message); }
  }

  function toggleUserAssignment(userId: number) {
    if (!assigningKey) return;
    const idx = assigningKey.assignedIDs.indexOf(userId);
    if (idx === -1) {
      assigningKey.assignedIDs = [...assigningKey.assignedIDs, userId];
    } else {
      assigningKey.assignedIDs = assigningKey.assignedIDs.filter(id => id !== userId);
    }
  }

  function copyText(text: string, label: string) {
    navigator.clipboard.writeText(text);
    addNotification('success', `${label} copied`);
  }

  if (browser) {
    loadTemplates();
    loadServers();
    loadUsers();
    loadManagedKeys();
  }
</script>

<div>
  <h1 class="text-2xl font-bold mb-6">Admin</h1>

  <div class="flex space-x-4 mb-6 border-b">
    <button onclick={() => tab = 'templates'} class="pb-2 px-1 {tab === 'templates' ? 'border-b-2 border-indigo-600 text-indigo-600' : 'text-gray-500'}">Templates</button>
    <button onclick={() => tab = 'servers'} class="pb-2 px-1 {tab === 'servers' ? 'border-b-2 border-indigo-600 text-indigo-600' : 'text-gray-500'}">Servers</button>
    <button onclick={() => tab = 'users'} class="pb-2 px-1 {tab === 'users' ? 'border-b-2 border-indigo-600 text-indigo-600' : 'text-gray-500'}">Users</button>
    <button onclick={() => tab = 'ssh-keys'} class="pb-2 px-1 {tab === 'ssh-keys' ? 'border-b-2 border-indigo-600 text-indigo-600' : 'text-gray-500'}">SSH Keys</button>
  </div>

  <!-- Templates Tab -->
  {#if tab === 'templates'}
    <div class="space-y-4">
      {#each templateList as tmpl}
        <div class="bg-white border rounded-lg p-4">
          <div class="flex justify-between items-start">
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2">
                <h3 class="font-medium">{tmpl.name}</h3>
                <span class="px-2 py-0.5 rounded text-xs {tmpl.is_active ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-800'}">
                  {tmpl.is_active ? 'Active' : 'Inactive'}
                </span>
              </div>
              <p class="text-sm text-gray-500 mt-1">{tmpl.slug} &middot; {tmpl.image}</p>
              {#if tmpl.description}
                <p class="text-sm text-gray-600 mt-1">{tmpl.description}</p>
              {/if}
              {#if Object.keys(parseResources(tmpl.resources)).length > 0}
                <div class="flex gap-3 mt-2">
                  {#each Object.entries(parseResources(tmpl.resources)) as [key, val]}
                    <span class="text-xs bg-gray-100 text-gray-600 px-2 py-1 rounded">{key}: {val}</span>
                  {/each}
                </div>
              {/if}
            </div>
            <div class="flex gap-2 ml-4">
              <button onclick={() => expandedTemplate = expandedTemplate === tmpl.id ? null : tmpl.id} class="text-gray-500 hover:text-gray-700 text-sm">
                {expandedTemplate === tmpl.id ? 'Collapse' : 'Details'}
              </button>
              <button onclick={() => startEditTemplate(tmpl)} class="text-indigo-600 hover:text-indigo-800 text-sm">Edit</button>
              <button onclick={() => deleteTemplate(tmpl.id)} class="text-red-600 hover:text-red-800 text-sm">Delete</button>
            </div>
          </div>

          {#if expandedTemplate === tmpl.id}
            <div class="mt-4 pt-4 border-t space-y-3">
              <div class="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <span class="text-gray-500">ID:</span> {tmpl.id}
                </div>
                <div>
                  <span class="text-gray-500">Profiles:</span> {parseProfiles(tmpl.profiles).join(', ') || 'None'}
                </div>
                <div>
                  <span class="text-gray-500">Created:</span> {new Date(tmpl.created_at).toLocaleString()}
                </div>
                <div>
                  <span class="text-gray-500">Updated:</span> {new Date(tmpl.updated_at).toLocaleString()}
                </div>
              </div>
              {#if tmpl.cloud_init}
                <div>
                  <p class="text-sm text-gray-500 mb-1">Cloud-Init:</p>
                  <pre class="text-xs bg-gray-50 border rounded p-3 overflow-x-auto max-h-48">{tmpl.cloud_init}</pre>
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/each}

      {#if templateList.length === 0}
        <p class="text-gray-500 text-sm">No templates found.</p>
      {/if}

      <div class="bg-white border rounded-lg p-4">
        <h3 class="font-medium mb-3">Import Template (JSON)</h3>
        <textarea bind:value={importJson} rows="6" class="w-full px-3 py-2 border rounded font-mono text-sm mb-3" placeholder="Paste template JSON here..."></textarea>
        <button onclick={importTemplate} class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700">Import</button>
      </div>
    </div>
  {/if}

  <!-- Servers Tab -->
  {#if tab === 'servers'}
    <div class="space-y-4">
      {#each serverList as server}
        <div class="bg-white border rounded-lg p-4">
          <div class="flex justify-between items-start">
            <div>
              <div class="flex items-center gap-2">
                <h3 class="font-medium">{server.name}</h3>
                <span class="px-2 py-0.5 rounded text-xs {server.is_online ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}">
                  {server.is_online ? 'Online' : 'Offline'}
                </span>
              </div>
              <p class="text-sm text-gray-500 mt-1">{server.endpoint}</p>
            </div>
            <div class="text-right">
              <p class="text-sm font-medium">{server.instance_count || 0} / {server.max_instances}</p>
              <p class="text-xs text-gray-500">instances</p>
            </div>
          </div>
          <div class="mt-3 pt-3 border-t grid grid-cols-2 gap-4 text-sm">
            <div>
              <span class="text-gray-500">ID:</span> {server.id}
            </div>
            <div>
              <span class="text-gray-500">Max Instances:</span> {server.max_instances}
            </div>
            <div>
              <span class="text-gray-500">Created:</span> {new Date(server.created_at).toLocaleString()}
            </div>
            <div>
              <span class="text-gray-500">Updated:</span> {new Date(server.updated_at).toLocaleString()}
            </div>
          </div>
          {#if server.max_instances > 0}
            <div class="mt-3">
              <div class="w-full bg-gray-200 rounded-full h-2">
                <div
                  class="h-2 rounded-full {((server.instance_count || 0) / server.max_instances) > 0.8 ? 'bg-red-500' : 'bg-indigo-500'}"
                  style="width: {Math.min(((server.instance_count || 0) / server.max_instances) * 100, 100)}%"
                ></div>
              </div>
              <p class="text-xs text-gray-500 mt-1">{Math.round(((server.instance_count || 0) / server.max_instances) * 100)}% capacity</p>
            </div>
          {/if}
        </div>
      {/each}

      {#if serverList.length === 0}
        <p class="text-gray-500 text-sm">No servers configured.</p>
      {/if}
    </div>
  {/if}

  <!-- Users Tab -->
  {#if tab === 'users'}
    <div class="bg-white border rounded-lg overflow-hidden">
      <table class="w-full">
        <thead class="bg-gray-50">
          <tr>
            <th class="text-left px-4 py-3 text-sm font-medium text-gray-500">Email</th>
            <th class="text-left px-4 py-3 text-sm font-medium text-gray-500">Name</th>
            <th class="text-left px-4 py-3 text-sm font-medium text-gray-500">Role</th>
            <th class="text-left px-4 py-3 text-sm font-medium text-gray-500">Created</th>
            <th class="text-right px-4 py-3 text-sm font-medium text-gray-500">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y">
          {#each userList as user}
            <tr>
              <td class="px-4 py-3 text-sm">{user.email}</td>
              <td class="px-4 py-3 text-sm">{user.name}</td>
              <td class="px-4 py-3">
                <span class="px-2 py-1 rounded text-xs {user.role === 'admin' ? 'bg-purple-100 text-purple-800' : 'bg-gray-100 text-gray-800'}">
                  {user.role}
                </span>
              </td>
              <td class="px-4 py-3 text-sm text-gray-500">{new Date(user.created_at).toLocaleDateString()}</td>
              <td class="px-4 py-3 text-right">
                <button onclick={() => startEditUser(user)} class="text-indigo-600 hover:text-indigo-800 text-sm mr-2">Edit</button>
                <button onclick={() => deleteUser(user)} class="text-red-600 hover:text-red-800 text-sm">Delete</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
      {#if userList.length === 0}
        <p class="text-gray-500 text-sm p-4">No users found.</p>
      {/if}
    </div>
  {/if}
</div>

<!-- SSH Keys Tab -->
{#if tab === 'ssh-keys'}
  <div class="space-y-4">
    <div class="bg-white border rounded-lg p-4">
      <h3 class="font-medium mb-1">Managed SSH Keys</h3>
      <p class="text-sm text-gray-500 mb-4">
        Generate a keypair here, add the <strong>public key</strong> to a GitHub account or org,
        then assign it to managed users. Their instances will receive the private key automatically —
        no action needed from the user.
      </p>

      <div class="space-y-3 mb-4">
        {#each managedKeyList as key}
          <div class="p-3 bg-gray-50 rounded border">
            <div class="flex justify-between items-start">
              <div>
                <span class="font-medium text-sm">{key.name}</span>
                <span class="text-xs text-gray-400 ml-2">{new Date(key.created_at).toLocaleDateString()}</span>
              </div>
              <div class="flex gap-3 ml-4">
                <button onclick={() => openAssignUsers(key)} class="text-indigo-600 hover:text-indigo-800 text-sm">Assign users</button>
                <button onclick={() => deleteManagedKey(key.id, key.name)} class="text-red-600 hover:text-red-800 text-sm">Delete</button>
              </div>
            </div>
            <div class="flex items-center gap-2 mt-2">
              <code class="text-xs text-gray-500 font-mono truncate flex-1">{key.public_key.substring(0, 60)}…</code>
              <button
                onclick={() => copyText(key.public_key, 'Public key')}
                class="text-xs text-indigo-600 hover:text-indigo-800 border border-indigo-200 rounded px-2 py-0.5 shrink-0"
              >
                Copy public key
              </button>
            </div>
          </div>
        {/each}
        {#if managedKeyList.length === 0}
          <p class="text-gray-500 text-sm">No managed keys yet.</p>
        {/if}
      </div>

      <div class="border-t pt-4 flex gap-3">
        <input
          bind:value={newKeyName}
          placeholder="Key name (e.g. plati-managed)"
          class="flex-1 px-3 py-2 border rounded text-sm"
          onkeydown={(e) => e.key === 'Enter' && generateManagedKey()}
        />
        <button
          onclick={generateManagedKey}
          disabled={!newKeyName.trim()}
          class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700 disabled:opacity-50 text-sm whitespace-nowrap"
        >
          Generate Key
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Generated managed key reveal modal -->
{#if generatedKey}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
    <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4">
      <div class="p-6">
        <h2 class="text-lg font-bold mb-1">Key generated: {generatedKey.name}</h2>
        <p class="text-sm text-amber-700 bg-amber-50 border border-amber-200 rounded p-3 mb-4">
          Save this now — the private key won't be shown again.
          Add the public key to the GitHub account or organization, then assign users below.
        </p>
        <div class="space-y-4">
          <div>
            <div class="flex justify-between items-center mb-1">
              <label class="text-sm font-medium">Public key</label>
              <button onclick={() => copyText(generatedKey!.public_key, 'Public key')} class="text-xs text-indigo-600 hover:underline">Copy</button>
            </div>
            <p class="text-xs text-gray-500 mb-1">Add to GitHub → Settings → SSH and GPG keys.</p>
            <textarea readonly rows="2" class="w-full px-3 py-2 border rounded font-mono text-xs bg-gray-50">{generatedKey.public_key}</textarea>
          </div>
          <div>
            <div class="flex justify-between items-center mb-1">
              <label class="text-sm font-medium">Private key</label>
              <button onclick={() => copyText(generatedKey!.private_key, 'Private key')} class="text-xs text-indigo-600 hover:underline">Copy</button>
            </div>
            <p class="text-xs text-gray-500 mb-1">Store securely as a backup. Not needed for normal operation.</p>
            <textarea readonly rows="6" class="w-full px-3 py-2 border rounded font-mono text-xs bg-gray-50">{generatedKey.private_key}</textarea>
          </div>
        </div>
        <div class="flex justify-end mt-6 pt-4 border-t">
          <button onclick={() => generatedKey = null} class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700">Done</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<!-- Assign users modal -->
{#if assigningKey}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onclick={(e) => { if (e.target === e.currentTarget) assigningKey = null; }}>
    <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4">
      <div class="p-6">
        <h2 class="text-lg font-bold mb-1">Assign users — {assigningKey.name}</h2>
        <p class="text-sm text-gray-500 mb-4">Selected users will have this key's private key injected into their instances on next create or rebuild.</p>
        <div class="space-y-2 max-h-64 overflow-y-auto">
          {#each userList.filter(u => u.role !== 'admin') as user}
            <label class="flex items-center gap-3 p-2 hover:bg-gray-50 rounded cursor-pointer">
              <input
                type="checkbox"
                checked={assigningKey.assignedIDs.includes(user.id)}
                onchange={() => toggleUserAssignment(user.id)}
              />
              <span class="text-sm">{user.email}</span>
              {#if user.name}
                <span class="text-xs text-gray-400">{user.name}</span>
              {/if}
            </label>
          {/each}
          {#if userList.filter(u => u.role !== 'admin').length === 0}
            <p class="text-sm text-gray-500">No non-admin users.</p>
          {/if}
        </div>
        <div class="flex justify-end gap-3 mt-6 pt-4 border-t">
          <button onclick={() => assigningKey = null} class="px-4 py-2 border rounded hover:bg-gray-50">Cancel</button>
          <button onclick={saveAssignments} class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700">Save</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<!-- Edit Template Modal -->
{#if editingTemplate}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onclick={(e) => { if (e.target === e.currentTarget) editingTemplate = null; }}>
    <div class="bg-white rounded-lg shadow-xl w-full max-w-2xl max-h-[90vh] overflow-y-auto mx-4">
      <div class="p-6">
        <h2 class="text-lg font-bold mb-4">Edit Template</h2>
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <input type="text" bind:value={editingTemplate.name} class="w-full px-3 py-2 border rounded" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Slug</label>
            <input type="text" bind:value={editingTemplate.slug} class="w-full px-3 py-2 border rounded" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Description</label>
            <input type="text" bind:value={editingTemplate.description} class="w-full px-3 py-2 border rounded" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Image</label>
            <input type="text" bind:value={editingTemplate.image} class="w-full px-3 py-2 border rounded" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Profiles (JSON array)</label>
            <input type="text" bind:value={editingTemplate.profiles} class="w-full px-3 py-2 border rounded font-mono text-sm" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Resources (JSON object)</label>
            <input type="text" bind:value={editingTemplate.resources} class="w-full px-3 py-2 border rounded font-mono text-sm" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Cloud-Init</label>
            <textarea bind:value={editingTemplate.cloud_init} rows="8" class="w-full px-3 py-2 border rounded font-mono text-sm"></textarea>
          </div>
          <div class="flex items-center gap-2">
            <input type="checkbox" bind:checked={editingTemplate.is_active} id="tmpl-active" />
            <label for="tmpl-active" class="text-sm font-medium text-gray-700">Active</label>
          </div>
        </div>
        <div class="flex justify-end gap-3 mt-6 pt-4 border-t">
          <button onclick={() => editingTemplate = null} class="px-4 py-2 border rounded hover:bg-gray-50">Cancel</button>
          <button onclick={saveTemplate} class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700">Save</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<!-- Edit User Modal -->
{#if editingUser}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onclick={(e) => { if (e.target === e.currentTarget) editingUser = null; }}>
    <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4">
      <div class="p-6">
        <h2 class="text-lg font-bold mb-4">Edit User</h2>
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <input type="text" bind:value={editingUser.name} class="w-full px-3 py-2 border rounded" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Role</label>
            <select bind:value={editingUser.role} class="w-full px-3 py-2 border rounded">
              <option value="user">User</option>
              <option value="admin">Admin</option>
            </select>
          </div>
        </div>
        <div class="flex justify-end gap-3 mt-6 pt-4 border-t">
          <button onclick={() => editingUser = null} class="px-4 py-2 border rounded hover:bg-gray-50">Cancel</button>
          <button onclick={saveUser} class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700">Save</button>
        </div>
      </div>
    </div>
  </div>
{/if}
