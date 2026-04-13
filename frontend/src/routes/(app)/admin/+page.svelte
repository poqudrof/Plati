<script lang="ts">
  import { browser } from '$app/environment';
  import { admin, templates as templatesApi } from '$lib/api';
  import type { Template, Server, User, ManagedSSHKey, GeneratedManagedKeyResult, GitRepo, UserSSHKey, AdminGeneratedUserKeyResult } from '$lib/api/types';
  import { addNotification } from '$lib/stores/notifications';

  let tab: 'templates' | 'servers' | 'users' | 'ssh-keys' | 'settings' | 'repos' = $state('templates');
  let templateList: Template[] = $state([]);
  let serverList: Server[] = $state([]);
  let userList: User[] = $state([]);
  let importYaml = $state('');

  // Git repos
  let repos: GitRepo[] = $state([]);
  let repoServerKey = $state('');
  let newSSHURL = $state('');
  let reposLoading = $state(false);

  // Template editing
  let editingTemplate: Template | null = $state(null);
  let expandedTemplate: number | null = $state(null);

  // User editing
  let editingUser: { id: number; name: string; role: string } | null = $state(null);
  let creatingUser: { email: string; name: string; password: string; isAdmin: boolean } | null = $state(null);

  // Managed SSH Keys
  let managedKeyList: ManagedSSHKey[] = $state([]);
  let newKeyName = $state('');
  let generatedKey: GeneratedManagedKeyResult | null = $state(null);
  let assigningKey: { id: number; name: string; assignedIDs: number[] } | null = $state(null);
  let serverGitKeyID = $state(0);

  // Per-user SSH key management
  let managingKeysForUser: User | null = $state(null);
  let userKeys: UserSSHKey[] = $state([]);
  let newUserKeyName = $state('');
  let userKeyGenerating = $state(false);

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
    if (!importYaml) return;
    try {
      await admin.templates.import(importYaml);
      importYaml = '';
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

  async function createUser() {
    if (!creatingUser) return;
    try {
      await admin.users.create(
        creatingUser.email,
        creatingUser.name,
        creatingUser.password,
        creatingUser.isAdmin ? 'admin' : 'user'
      );
      creatingUser = null;
      await loadUsers();
      addNotification('success', 'User created');
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

  async function loadServerGitKey() {
    try {
      const result = await admin.repos.getServerKey();
      serverGitKeyID = result.managed_key_id ?? 0;
    } catch { serverGitKeyID = 0; }
  }

  async function useAsServerKey(keyID: number) {
    try {
      await admin.repos.setServerManagedKey(keyID);
      serverGitKeyID = keyID;
      addNotification('success', 'Key set as server git key');
    } catch (e: any) { addNotification('error', e.message); }
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

  async function openUserKeys(user: User) {
    managingKeysForUser = user;
    newUserKeyName = '';
    try {
      userKeys = await admin.users.listKeys(user.id);
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function generateUserKey() {
    if (!managingKeysForUser || !newUserKeyName.trim()) return;
    userKeyGenerating = true;
    try {
      const key = await admin.users.generateKey(managingKeysForUser.id, newUserKeyName.trim());
      newUserKeyName = '';
      userKeys = await admin.users.listKeys(managingKeysForUser.id);
      addNotification('success', `Key "${key.name}" generated — public key ready to copy`);
    } catch (e: any) { addNotification('error', e.message); }
    finally { userKeyGenerating = false; }
  }

  async function deleteUserKey(keyId: number) {
    if (!managingKeysForUser) return;
    if (!confirm('Delete this key? Instances using it will lose access on next rebuild.')) return;
    try {
      await admin.users.deleteKey(managingKeysForUser.id, keyId);
      userKeys = await admin.users.listKeys(managingKeysForUser.id);
      addNotification('success', 'Key deleted');
    } catch (e: any) { addNotification('error', e.message); }
  }

  function copyText(text: string, label: string) {
    navigator.clipboard.writeText(text);
    addNotification('success', `${label} copied`);
  }

  function parsePostCreateCommands(json: string): string {
    try {
      const cmds: string[] = JSON.parse(json || '[]');
      return cmds.join('\n');
    } catch { return ''; }
  }

  function serializePostCreateCommands(text: string): string {
    const cmds = text.split('\n').filter(l => l.trim() !== '');
    return JSON.stringify(cmds);
  }

  // Admin settings
  let tailscaleConfigured = $state(false);
  let tailscaleInput = $state('');
  let savingTailscale = $state(false);

  async function loadAdminSettings() {
    try {
      const res = await admin.settings.getTailscaleKey();
      tailscaleConfigured = res.configured;
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function saveTailscaleKey() {
    if (!tailscaleInput.trim()) return;
    savingTailscale = true;
    try {
      await admin.settings.setTailscaleKey(tailscaleInput.trim());
      tailscaleInput = '';
      tailscaleConfigured = true;
      addNotification('success', 'Platform Tailscale key saved');
    } catch (e: any) { addNotification('error', e.message); } finally { savingTailscale = false; }
  }

  async function deleteTailscaleKey() {
    if (!confirm('Remove the platform Tailscale auth key? Instances using it will not join Tailscale on next rebuild.')) return;
    try {
      await admin.settings.deleteTailscaleKey();
      tailscaleConfigured = false;
      addNotification('success', 'Platform Tailscale key removed');
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function loadRepos() {
    try { repos = await admin.repos.list(); } catch (e: any) { addNotification('error', 'Failed to load repos: ' + e.message); }
  }

  async function loadRepoServerKey() {
    try {
      const result = await admin.repos.getServerKey();
      repoServerKey = result.public_key;
    } catch { repoServerKey = ''; }
  }

  async function addRepo() {
    if (!newSSHURL.trim()) return;
    reposLoading = true;
    try {
      await admin.repos.add(newSSHURL.trim());
      newSSHURL = '';
      await loadRepos();
      addNotification('success', 'Repository added and cloning started');
    } catch (e: any) {
      addNotification('error', 'Failed to add repo: ' + e.message);
    } finally {
      reposLoading = false;
    }
  }

  async function deleteRepo(id: number, name: string) {
    if (!confirm(`Delete repo "${name}"? This will remove the local clone.`)) return;
    try {
      await admin.repos.delete(id);
      await loadRepos();
      addNotification('success', 'Repository deleted');
    } catch (e: any) { addNotification('error', e.message); }
  }

  async function syncRepo(id: number) {
    try {
      await admin.repos.sync(id);
      addNotification('success', 'Sync started');
      setTimeout(loadRepos, 2000);
    } catch (e: any) { addNotification('error', e.message); }
  }

  function repoStatusColor(status: string) {
    switch (status) {
      case 'ready': return 'text-green-600 bg-green-50';
      case 'cloning': return 'text-blue-600 bg-blue-50';
      case 'error': return 'text-red-600 bg-red-50';
      default: return 'text-gray-600 bg-gray-50';
    }
  }

  if (browser) {
    loadTemplates();
    loadServers();
    loadUsers();
    loadManagedKeys();
    loadAdminSettings();
    loadServerGitKey();
    loadRepos();
    loadRepoServerKey();
  }
</script>

<div>
  <h1 class="text-2xl font-bold mb-6">Admin</h1>

  <div class="flex space-x-4 mb-6 border-b">
    <button onclick={() => tab = 'templates'} class="pb-2 px-1 {tab === 'templates' ? 'border-b-2 border-primary text-primary' : 'text-gray-500'}">Templates</button>
    <button onclick={() => tab = 'servers'} class="pb-2 px-1 {tab === 'servers' ? 'border-b-2 border-primary text-primary' : 'text-gray-500'}">Servers</button>
    <button onclick={() => tab = 'users'} class="pb-2 px-1 {tab === 'users' ? 'border-b-2 border-primary text-primary' : 'text-gray-500'}">Users</button>
    <button onclick={() => tab = 'ssh-keys'} class="pb-2 px-1 {tab === 'ssh-keys' ? 'border-b-2 border-primary text-primary' : 'text-gray-500'}">SSH Keys</button>
    <button onclick={() => tab = 'settings'} class="pb-2 px-1 {tab === 'settings' ? 'border-b-2 border-primary text-primary' : 'text-gray-500'}">Settings</button>
    <button onclick={() => tab = 'repos'} class="pb-2 px-1 {tab === 'repos' ? 'border-b-2 border-primary text-primary' : 'text-gray-500'}">Git Repos</button>
    <a href="/admin/disks" class="pb-2 px-1 text-gray-500 hover:text-primary">Disks</a>
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
              <a href="/admin/templates/{tmpl.id}/debug" class="text-purple-600 hover:text-purple-800 text-sm">Debug</a>
              <a href="/admin/templates/{tmpl.id}" class="text-primary hover:text-primary-dark text-sm">Edit</a>
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
            </div>
          {/if}
        </div>
      {/each}

      {#if templateList.length === 0}
        <p class="text-gray-500 text-sm">No templates found.</p>
      {/if}

      <div class="bg-white border rounded-lg p-4">
        <h3 class="font-medium mb-3">Import Template (YAML)</h3>
        <textarea bind:value={importYaml} rows="6" class="w-full px-3 py-2 border rounded font-mono text-sm mb-3" placeholder="Paste template YAML here..."></textarea>
        <button onclick={importTemplate} class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark">Import</button>
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
                  class="h-2 rounded-full {((server.instance_count || 0) / server.max_instances) > 0.8 ? 'bg-red-500' : 'bg-primary'}"
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

  <!-- Settings Tab -->
  {#if tab === 'settings'}
    <div class="space-y-4">
      <div class="bg-white border rounded-lg p-4">
        <h3 class="font-medium mb-1">Tailscale Platform Auth Key</h3>
        <p class="text-sm text-gray-500 mb-4">
          This key is injected as <code class="bg-gray-100 px-1 rounded">TAILSCALE_AUTH_KEY</code> into instances
          whose users have Tailscale mode set to <em>Platform key</em>. The value is encrypted at rest and never exposed in the UI.
        </p>
        <div class="flex items-center gap-2 mb-4">
          <span class="text-sm font-medium">Status:</span>
          {#if tailscaleConfigured}
            <span class="px-2 py-0.5 rounded text-xs bg-green-100 text-green-800">Configured</span>
            <button onclick={deleteTailscaleKey} class="text-red-600 hover:text-red-800 text-sm ml-2">Remove</button>
          {:else}
            <span class="px-2 py-0.5 rounded text-xs bg-gray-100 text-gray-800">Not configured</span>
          {/if}
        </div>
        <div class="flex gap-3">
          <input
            bind:value={tailscaleInput}
            type="password"
            placeholder="tskey-auth-…"
            class="flex-1 px-3 py-2 border rounded text-sm font-mono"
          />
          <button
            onclick={saveTailscaleKey}
            disabled={savingTailscale || !tailscaleInput.trim()}
            class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm whitespace-nowrap"
          >
            {tailscaleConfigured ? 'Replace Key' : 'Save Key'}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <!-- Git Repos Tab -->
  {#if tab === 'repos'}
    <div class="space-y-8">
      <!-- Add repo -->
      <section class="border rounded-lg p-6 space-y-4">
        <h2 class="text-lg font-semibold">Add Repository</h2>
        <form onsubmit={(e) => { e.preventDefault(); addRepo(); }} class="flex gap-2">
          <input
            type="text"
            bind:value={newSSHURL}
            placeholder="git@github.com:org/repo.git"
            class="flex-1 border rounded px-3 py-2 text-sm font-mono"
            disabled={reposLoading}
          />
          <button
            type="submit"
            disabled={reposLoading || !newSSHURL.trim()}
            class="px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
          >Add</button>
        </form>
      </section>

      <!-- Repo list -->
      <section class="space-y-3">
        {#if repos.length === 0}
          <p class="text-gray-500 text-sm">No repositories registered.</p>
        {:else}
          {#each repos as repo (repo.id)}
            <div class="border rounded-lg p-4 space-y-2">
              <div class="flex items-center justify-between gap-4">
                <div class="min-w-0">
                  <div class="font-medium">{repo.name}</div>
                  <div class="text-xs text-gray-500 font-mono truncate">{repo.ssh_url}</div>
                </div>
                <div class="flex items-center gap-2 shrink-0">
                  <span class="px-2 py-0.5 rounded text-xs font-medium {repoStatusColor(repo.clone_status)}">
                    {repo.clone_status}
                  </span>
                  <button
                    onclick={() => syncRepo(repo.id)}
                    class="px-3 py-1 text-sm border rounded hover:bg-gray-50"
                    title="Pull latest"
                  >Sync</button>
                  <button
                    onclick={() => deleteRepo(repo.id, repo.name)}
                    class="px-3 py-1 text-sm text-red-600 border border-red-200 rounded hover:bg-red-50"
                  >Delete</button>
                </div>
              </div>
              {#if repo.clone_status === 'error' && repo.error_message}
                <pre class="text-xs text-red-700 bg-red-50 p-2 rounded overflow-x-auto">{repo.error_message}</pre>
              {/if}
              {#if repo.last_synced_at}
                <div class="text-xs text-gray-400">Last synced: {new Date(repo.last_synced_at).toLocaleString()}</div>
              {/if}
            </div>
          {/each}
        {/if}
      </section>

      <!-- Server SSH Key -->
      <section class="border-t pt-6">
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <p class="text-xs font-medium text-gray-500 uppercase tracking-wide mb-1">Server SSH Key</p>
            {#if repoServerKey}
              <p class="text-xs text-gray-400 mb-1">Add as a deploy key (read-only) on each GitHub repo you want to cache.</p>
              <div class="flex items-center gap-2">
                <code class="text-xs bg-gray-100 px-2 py-1 rounded font-mono truncate max-w-md">{repoServerKey}</code>
                <button onclick={() => copyText(repoServerKey, 'Server key')} class="text-xs text-primary hover:text-primary-dark border border-primary/20 rounded px-2 py-0.5 shrink-0">Copy</button>
              </div>
            {:else}
              <p class="text-xs text-gray-400">No server key configured. Set one in the SSH Keys tab.</p>
            {/if}
          </div>
        </div>
      </section>
    </div>
  {/if}

  <!-- Users Tab -->
  {#if tab === 'users'}
    <div class="flex justify-end mb-3">
      <button
        onclick={() => creatingUser = { email: '', name: '', password: '', isAdmin: false }}
        class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark text-sm"
      >
        Create User
      </button>
    </div>
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
                <button onclick={() => openUserKeys(user)} class="text-gray-600 hover:text-gray-800 text-sm mr-2">Keys</button>
                <button onclick={() => startEditUser(user)} class="text-primary hover:text-primary-dark text-sm mr-2">Edit</button>
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
          <div class="p-3 rounded border {serverGitKeyID === key.id ? 'bg-primary-50 border-primary/30' : 'bg-gray-50'}">
            <div class="flex justify-between items-start">
              <div class="flex items-center gap-2">
                <span class="font-medium text-sm">{key.name}</span>
                {#if serverGitKeyID === key.id}
                  <span class="text-xs bg-primary-50 text-primary-dark px-1.5 py-0.5 rounded font-medium">Server git key</span>
                {/if}
                <span class="text-xs text-gray-400">{new Date(key.created_at).toLocaleDateString()}</span>
              </div>
              <div class="flex gap-3 ml-4 shrink-0">
                {#if serverGitKeyID !== key.id}
                  <button onclick={() => useAsServerKey(key.id)} class="text-primary hover:text-primary-dark text-sm">Use as server key</button>
                {/if}
                <button onclick={() => openAssignUsers(key)} class="text-primary hover:text-primary-dark text-sm">Assign users</button>
                <button onclick={() => deleteManagedKey(key.id, key.name)} class="text-red-600 hover:text-red-800 text-sm">Delete</button>
              </div>
            </div>
            <div class="flex items-center gap-2 mt-2">
              <code class="text-xs text-gray-500 font-mono truncate flex-1">{key.public_key.substring(0, 60)}…</code>
              <button
                onclick={() => copyText(key.public_key, 'Public key')}
                class="text-xs text-primary hover:text-primary-dark border border-primary/20 rounded px-2 py-0.5 shrink-0"
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
          class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm whitespace-nowrap"
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
              <button onclick={() => copyText(generatedKey!.public_key, 'Public key')} class="text-xs text-primary hover:underline">Copy</button>
            </div>
            <p class="text-xs text-gray-500 mb-1">Add to GitHub → Settings → SSH and GPG keys.</p>
            <textarea readonly rows="2" class="w-full px-3 py-2 border rounded font-mono text-xs bg-gray-50">{generatedKey.public_key}</textarea>
          </div>
          <div>
            <div class="flex justify-between items-center mb-1">
              <label class="text-sm font-medium">Private key</label>
              <button onclick={() => copyText(generatedKey!.private_key, 'Private key')} class="text-xs text-primary hover:underline">Copy</button>
            </div>
            <p class="text-xs text-gray-500 mb-1">Store securely as a backup. Not needed for normal operation.</p>
            <textarea readonly rows="6" class="w-full px-3 py-2 border rounded font-mono text-xs bg-gray-50">{generatedKey.private_key}</textarea>
          </div>
        </div>
        <div class="flex justify-end mt-6 pt-4 border-t">
          <button onclick={() => generatedKey = null} class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark">Done</button>
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
          <button onclick={saveAssignments} class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark">Save</button>
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
            <label class="block text-sm font-medium text-gray-700 mb-1">Terminal User</label>
            <input type="text" bind:value={editingTemplate.terminal_user} placeholder="e.g. ubuntu (leave empty for root only)" class="w-full px-3 py-2 border rounded text-sm" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Post-create commands (one per line, run via Incus exec)</label>
            <textarea
              rows="4"
              class="w-full px-3 py-2 border rounded font-mono text-sm"
              placeholder="apk add --no-cache git&#10;mkdir -p /workspace"
              value={parsePostCreateCommands(editingTemplate.post_create_commands)}
              oninput={(e) => { editingTemplate!.post_create_commands = serializePostCreateCommands((e.target as HTMLTextAreaElement).value); }}
            ></textarea>
            <p class="text-xs text-gray-400 mt-1">Each non-empty line is run as a separate shell command inside the instance.</p>
          </div>
          <div class="flex items-center gap-2">
            <input type="checkbox" bind:checked={editingTemplate.is_active} id="tmpl-active" />
            <label for="tmpl-active" class="text-sm font-medium text-gray-700">Active</label>
          </div>
        </div>
        <div class="flex justify-end gap-3 mt-6 pt-4 border-t">
          <button onclick={() => editingTemplate = null} class="px-4 py-2 border rounded hover:bg-gray-50">Cancel</button>
          <button onclick={saveTemplate} class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark">Save</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<!-- Create User Modal -->
{#if creatingUser}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onclick={(e) => { if (e.target === e.currentTarget) creatingUser = null; }}>
    <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4">
      <div class="p-6">
        <h2 class="text-lg font-bold mb-4">Create User</h2>
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Email</label>
            <input type="email" bind:value={creatingUser.email} class="w-full px-3 py-2 border rounded" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <input type="text" bind:value={creatingUser.name} class="w-full px-3 py-2 border rounded" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Password</label>
            <input type="password" bind:value={creatingUser.password} class="w-full px-3 py-2 border rounded" />
          </div>
          <div class="flex items-center gap-2">
            <input type="checkbox" bind:checked={creatingUser.isAdmin} id="create-user-admin" />
            <label for="create-user-admin" class="text-sm font-medium text-gray-700">Admin</label>
          </div>
        </div>
        <div class="flex justify-end gap-3 mt-6 pt-4 border-t">
          <button onclick={() => creatingUser = null} class="px-4 py-2 border rounded hover:bg-gray-50">Cancel</button>
          <button onclick={createUser} class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark">Create</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<!-- Manage User Keys Modal -->
{#if managingKeysForUser}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onclick={(e) => { if (e.target === e.currentTarget) managingKeysForUser = null; }}>
    <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4">
      <div class="p-6">
        <h2 class="text-lg font-bold mb-1">SSH Keys — {managingKeysForUser.email}</h2>
        <p class="text-sm text-gray-500 mb-4">
          Generate a keypair on behalf of this user. The private key is stored encrypted on the server
          and injected automatically into their instances. Copy the public key to add to GitHub or a git repo.
        </p>

        <div class="space-y-3 mb-4">
          {#each userKeys as key}
            <div class="p-3 bg-gray-50 rounded border">
              <div class="flex justify-between items-start">
                <div class="min-w-0 flex-1">
                  <span class="font-medium text-sm">{key.name}</span>
                  <span class="text-xs text-gray-400 ml-2">{new Date(key.created_at).toLocaleDateString()}</span>
                </div>
                <button onclick={() => deleteUserKey(key.id)} class="text-red-600 hover:text-red-800 text-sm ml-4 shrink-0">Delete</button>
              </div>
              <div class="flex items-center gap-2 mt-2">
                <code class="text-xs text-gray-500 font-mono truncate flex-1">{key.public_key.substring(0, 60)}…</code>
                <button
                  onclick={() => copyText(key.public_key, 'Public key')}
                  class="text-xs text-primary hover:text-primary-dark border border-primary/20 rounded px-2 py-0.5 shrink-0"
                >Copy public key</button>
              </div>
            </div>
          {/each}
          {#if userKeys.length === 0}
            <p class="text-sm text-gray-500">No keys yet.</p>
          {/if}
        </div>

        <div class="border-t pt-4 flex gap-3">
          <input
            bind:value={newUserKeyName}
            placeholder="Key name (e.g. plati-{managingKeysForUser.name || 'user'})"
            class="flex-1 px-3 py-2 border rounded text-sm"
            onkeydown={(e) => e.key === 'Enter' && generateUserKey()}
          />
          <button
            onclick={generateUserKey}
            disabled={userKeyGenerating || !newUserKeyName.trim()}
            class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark disabled:opacity-50 text-sm whitespace-nowrap"
          >
            {userKeyGenerating ? 'Generating…' : 'Generate Key'}
          </button>
        </div>

        <div class="flex justify-end mt-6 pt-4 border-t">
          <button onclick={() => managingKeysForUser = null} class="px-4 py-2 border rounded hover:bg-gray-50">Close</button>
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
          <button onclick={saveUser} class="px-4 py-2 bg-primary text-white rounded hover:bg-primary-dark">Save</button>
        </div>
      </div>
    </div>
  </div>
{/if}
