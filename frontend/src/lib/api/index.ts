import { api } from './client';
import type { User, SSHKey, Secret, InstanceSecret, InstanceStats, InstanceStorageInfo, SshxURLResult, TailscaleServeResult, TailscaleStatusResult, Template, Instance, Server, ImageSummary, SetupRequest, UserSSHKey, GeneratedKeyResult, ManagedSSHKey, GeneratedManagedKeyResult, AdminGeneratedUserKeyResult, IncusDetail, IncusConfigUpdate, UserPreferences, MixinInfo, ExecResult, GitRepo, FileEntry, VolumeSnapshot, DiskInfo, ProfileCheck, ApiKey, GeneratedApiKeyResult } from './types';

// Setup
export const setup = {
  status: () => api.get<{ setup_completed: boolean }>('/api/setup/status'),
  complete: (data: SetupRequest) => api.post<{ message: string; needs_restart: boolean }>('/api/setup/complete', data)
};

// Auth
export const auth = {
  login: (password: string, email?: string) => api.post<{ message: string }>('/auth/login', { password, ...(email ? { email } : {}) }),
  logout: () => api.post<{ message: string }>('/auth/logout'),
  me: () => api.get<User>('/auth/me')
};

// User
export const users = {
  profile: () => api.get<User>('/api/v1/profile'),
  preferences: () => api.get<UserPreferences>('/api/v1/preferences'),
  updatePreferences: (p: { ssh_key_mode: string; tailscale_mode: string }) =>
    api.put<UserPreferences>('/api/v1/preferences', p),
  listUserKeys: () => api.get<UserSSHKey[]>('/api/v1/user-keys'),
  generateUserKey: (name: string) =>
    api.post<GeneratedKeyResult>('/api/v1/user-keys/generate', { name }),
  deleteUserKey: (id: number) => api.del(`/api/v1/user-keys/${id}`),
  listSSHKeys: () => api.get<SSHKey[]>('/api/v1/ssh-keys'),
  createSSHKey: (name: string, public_key: string) =>
    api.post<SSHKey>('/api/v1/ssh-keys', { name, public_key }),
  deleteSSHKey: (id: number) => api.del(`/api/v1/ssh-keys/${id}`),
  listSecrets: () => api.get<Secret[]>('/api/v1/secrets'),
  createSecret: (name: string, value: string) =>
    api.post<{ id: number }>('/api/v1/secrets', { name, value }),
  updateSecret: (id: number, value: string) =>
    api.put(`/api/v1/secrets/${id}`, { value }),
  deleteSecret: (id: number) => api.del(`/api/v1/secrets/${id}`),
  // API keys: view + regenerate own. Only admins can mint a new one — see admin.users.apiKeys.
  listApiKeys: () => api.get<ApiKey[]>('/api/v1/api-keys'),
  regenerateApiKey: (id: number) =>
    api.post<GeneratedApiKeyResult>(`/api/v1/api-keys/${id}/regenerate`)
};

// Disks (current user)
export const disks = {
  list: () => api.get<DiskInfo[]>('/api/v1/disks')
};

// Templates
export const templates = {
  list: () => api.get<Template[]>('/api/v1/templates'),
  get: (id: number) => api.get<Template>(`/api/v1/templates/${id}`),
  exportJSON: (id: number) => api.get<unknown>(`/api/v1/templates/${id}/export`)
};

// Instances
export const instances = {
  list: () => api.get<Instance[]>('/api/v1/instances'),
  get: (id: number) => api.get<Instance>(`/api/v1/instances/${id}`),
  create: (name: string, template_id: number, opts?: { ssh_key_mode?: string; tailscale_mode?: string }) =>
    api.post<Instance>('/api/v1/instances', { name, template_id, ...opts }),
  start: (id: number) => api.post(`/api/v1/instances/${id}/start`),
  stop: (id: number) => api.post(`/api/v1/instances/${id}/stop`),
  rebuild: (id: number) => api.post(`/api/v1/instances/${id}/rebuild`),
  delete: (id: number) => api.del(`/api/v1/instances/${id}`),
  stats: (id: number) => api.get<InstanceStats>(`/api/v1/instances/${id}/stats`),
  volumes: (id: number) => api.get<InstanceStorageInfo>(`/api/v1/instances/${id}/volumes`),
  sshxUrl: (id: number) => api.get<SshxURLResult>(`/api/v1/instances/${id}/sshx-url`),
  duplicate: (id: number) => api.post<Instance>(`/api/v1/instances/${id}/duplicate`),
  tailscaleServe: (id: number, port: number) =>
    api.post<TailscaleServeResult>(`/api/v1/instances/${id}/tailscale-serve`, { port }),
  tailscaleServeStatus: (id: number) =>
    api.get<TailscaleServeResult>(`/api/v1/instances/${id}/tailscale-serve`),
  tailscaleServeOff: (id: number) =>
    api.del(`/api/v1/instances/${id}/tailscale-serve`),
  tailscaleStatus: (id: number) =>
    api.get<TailscaleStatusResult>(`/api/v1/instances/${id}/tailscale-status`),
  listSecrets: (id: number) => api.get<InstanceSecret[]>(`/api/v1/instances/${id}/secrets`),
  createSecret: (id: number, name: string, value: string) =>
    api.post<{ id: number }>(`/api/v1/instances/${id}/secrets`, { name, value }),
  updateSecret: (id: number, secretId: number, value: string) =>
    api.put(`/api/v1/instances/${id}/secrets/${secretId}`, { value }),
  deleteSecret: (id: number, secretId: number) =>
    api.del(`/api/v1/instances/${id}/secrets/${secretId}`),
  // Storage
  browseDirectory: (id: number, path: string) =>
    api.get<FileEntry[]>(`/api/v1/instances/${id}/storage/browse?path=${encodeURIComponent(path)}`),
  downloadFileUrl: (id: number, path: string) =>
    `/api/v1/instances/${id}/storage/download?path=${encodeURIComponent(path)}`,
  downloadDirUrl: (id: number, path: string) =>
    `/api/v1/instances/${id}/storage/download-dir?path=${encodeURIComponent(path)}`,
  listSnapshots: (id: number, volId: number) =>
    api.get<VolumeSnapshot[]>(`/api/v1/instances/${id}/storage/volumes/${volId}/snapshots`),
  createSnapshot: (id: number, volId: number, name: string) =>
    api.post<{ message: string }>(`/api/v1/instances/${id}/storage/volumes/${volId}/snapshots`, { name }),
  deleteSnapshot: (id: number, volId: number, snapshotName: string) =>
    api.del(`/api/v1/instances/${id}/storage/volumes/${volId}/snapshots/${encodeURIComponent(snapshotName)}`),
  restoreSnapshot: (id: number, volId: number, snapshotName: string) =>
    api.post<{ message: string }>(`/api/v1/instances/${id}/storage/volumes/${volId}/snapshots/${encodeURIComponent(snapshotName)}/restore`)
};

// Admin
export const admin = {
  templates: {
    create: (tmpl: Partial<Template>) => api.post<Template>('/api/v1/admin/templates', tmpl),
    update: (id: number, tmpl: Partial<Template>) => api.put<Template>(`/api/v1/admin/templates/${id}`, tmpl),
    delete: (id: number) => api.del(`/api/v1/admin/templates/${id}`),
    import: (yaml: string) => api.postRaw<Template>('/api/v1/admin/templates/import', yaml, 'text/yaml'),
    listMixins: () => api.get<MixinInfo[]>('/api/v1/admin/templates/mixins'),
    debugCreate: (id: number) => api.post<Instance>(`/api/v1/admin/templates/${id}/debug`, {}),
    duplicate: (id: number, name: string, slug: string) => api.post<Template>(`/api/v1/admin/templates/${id}/duplicate`, { name, slug }),
    exportYAML: (id: number) => api.get<{ yaml: string }>(`/api/v1/admin/templates/${id}/export-yaml`),
    updateFromYAML: (id: number, yaml: string) => api.post<Template>(`/api/v1/admin/templates/${id}/update-from-yaml`, { yaml }),
    getDebugInstance: (id: number) => api.get<Instance>(`/api/v1/admin/templates/${id}/debug/instance`),
    saveToDisk: (id: number) => api.post<{ message: string }>(`/api/v1/admin/templates/${id}/save-to-disk`),
    reloadFromDisk: (id: number) => api.post<Template>(`/api/v1/admin/templates/${id}/reload-from-disk`, {}),
    checkProfiles: (id: number) => api.get<ProfileCheck[]>(`/api/v1/admin/templates/${id}/profiles/check`)
  },
  servers: {
    list: () => api.get<Server[]>('/api/v1/admin/servers')
  },
  images: {
    list: (server?: string) => api.get<ImageSummary[]>(`/api/v1/admin/images${server ? `?server=${server}` : ''}`)
  },
  users: {
    list: () => api.get<User[]>('/api/v1/admin/users'),
    create: (email: string, name: string, password: string, role: 'admin' | 'user') =>
      api.post<User>('/api/v1/admin/users', { email, name, password, role }),
    update: (id: number, name: string, role: string) =>
      api.put(`/api/v1/admin/users/${id}`, { name, role }),
    delete: (id: number) => api.del(`/api/v1/admin/users/${id}`),
    listKeys: (id: number) => api.get<UserSSHKey[]>(`/api/v1/admin/users/${id}/keys`),
    generateKey: (id: number, name: string) =>
      api.post<AdminGeneratedUserKeyResult>(`/api/v1/admin/users/${id}/keys/generate`, { name }),
    deleteKey: (userId: number, keyId: number) =>
      api.del(`/api/v1/admin/users/${userId}/keys/${keyId}`),
    apiKeys: {
      list: (userId: number) => api.get<ApiKey[]>(`/api/v1/admin/users/${userId}/api-keys`),
      create: (userId: number, name: string) =>
        api.post<GeneratedApiKeyResult>(`/api/v1/admin/users/${userId}/api-keys`, { name }),
      regenerate: (userId: number, keyId: number) =>
        api.post<GeneratedApiKeyResult>(`/api/v1/admin/users/${userId}/api-keys/${keyId}/regenerate`)
    }
  },
  instances: {
    list: () => api.get<Instance[]>('/api/v1/admin/instances'),
    create: (name: string, template_id: number, user_id: number) =>
      api.post<Instance>('/api/v1/admin/instances', { name, template_id, user_id }),
    incusInfo: (id: number) => api.get<IncusDetail>(`/api/v1/admin/instances/${id}/incus-info`),
    updateIncusConfig: (id: number, config: IncusConfigUpdate) =>
      api.put(`/api/v1/admin/instances/${id}/incus-config`, config),
    exec: (id: number, command: string) =>
      api.post<ExecResult>(`/api/v1/admin/instances/${id}/exec`, { command }),
    reapplySetup: (id: number) =>
      api.post<ExecResult>(`/api/v1/admin/instances/${id}/reapply-setup`)
  },
  managedKeys: {
    list: () => api.get<ManagedSSHKey[]>('/api/v1/admin/managed-keys'),
    generate: (name: string) =>
      api.post<GeneratedManagedKeyResult>('/api/v1/admin/managed-keys', { name }),
    delete: (id: number) => api.del(`/api/v1/admin/managed-keys/${id}`),
    getUsers: (id: number) => api.get<{ user_ids: number[] }>(`/api/v1/admin/managed-keys/${id}/users`),
    setUsers: (id: number, user_ids: number[]) =>
      api.put(`/api/v1/admin/managed-keys/${id}/users`, { user_ids })
  },
  settings: {
    getTailscaleKey: () => api.get<{ configured: boolean }>('/api/v1/admin/settings/tailscale-key'),
    setTailscaleKey: (value: string) => api.put('/api/v1/admin/settings/tailscale-key', { value }),
    deleteTailscaleKey: () => api.del('/api/v1/admin/settings/tailscale-key')
  },
  disks: {
    list: () => api.get<DiskInfo[]>('/api/v1/admin/disks')
  },
  repos: {
    list: () => api.get<GitRepo[]>('/api/v1/admin/repos'),
    add: (ssh_url: string) => api.post<GitRepo>('/api/v1/admin/repos', { ssh_url }),
    delete: (id: number) => api.del(`/api/v1/admin/repos/${id}`),
    rename: (id: number, name: string) => api.put(`/api/v1/admin/repos/${id}`, { name }),
    sync: (id: number) => api.post<{ status: string; command: string; output: string }>(`/api/v1/admin/repos/${id}/sync`),
    getServerKey: () => api.get<{ public_key: string; managed_key_id?: number }>('/api/v1/admin/repos/server-key'),
    generateServerKey: () => api.post<{ public_key: string }>('/api/v1/admin/repos/server-key/generate'),
    setServerManagedKey: (managed_key_id: number) =>
      api.put<{ public_key: string; managed_key_id: number }>('/api/v1/admin/repos/server-key/managed', { managed_key_id })
  }
};
