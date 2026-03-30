import { api } from './client';
import type { User, SSHKey, Secret, InstanceSecret, Template, Instance, Server, ImageSummary, SetupRequest, UserSSHKey, GeneratedKeyResult, ManagedSSHKey, GeneratedManagedKeyResult, IncusDetail } from './types';

// Setup
export const setup = {
  status: () => api.get<{ setup_completed: boolean }>('/api/setup/status'),
  complete: (data: SetupRequest) => api.post<{ message: string; needs_restart: boolean }>('/api/setup/complete', data)
};

// Auth
export const auth = {
  login: (password: string) => api.post<{ message: string }>('/auth/login', { password }),
  logout: () => api.post<{ message: string }>('/auth/logout'),
  me: () => api.get<User>('/auth/me')
};

// User
export const users = {
  profile: () => api.get<User>('/api/v1/profile'),
  listSSHKeys: () => api.get<UserSSHKey[]>('/api/v1/ssh-keys'),
  generateSSHKey: (name: string) =>
    api.post<GeneratedKeyResult>('/api/v1/ssh-keys/generate', { name }),
  deleteSSHKey: (id: number) => api.del(`/api/v1/ssh-keys/${id}`),
  listSecrets: () => api.get<Secret[]>('/api/v1/secrets'),
  createSecret: (name: string, value: string) =>
    api.post<{ id: number }>('/api/v1/secrets', { name, value }),
  updateSecret: (id: number, value: string) =>
    api.put(`/api/v1/secrets/${id}`, { value }),
  deleteSecret: (id: number) => api.del(`/api/v1/secrets/${id}`)
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
  create: (name: string, template_id: number) =>
    api.post<Instance>('/api/v1/instances', { name, template_id }),
  start: (id: number) => api.post(`/api/v1/instances/${id}/start`),
  stop: (id: number) => api.post(`/api/v1/instances/${id}/stop`),
  rebuild: (id: number) => api.post(`/api/v1/instances/${id}/rebuild`),
  delete: (id: number) => api.del(`/api/v1/instances/${id}`),
  listSecrets: (id: number) => api.get<InstanceSecret[]>(`/api/v1/instances/${id}/secrets`),
  createSecret: (id: number, name: string, value: string) =>
    api.post<{ id: number }>(`/api/v1/instances/${id}/secrets`, { name, value }),
  updateSecret: (id: number, secretId: number, value: string) =>
    api.put(`/api/v1/instances/${id}/secrets/${secretId}`, { value }),
  deleteSecret: (id: number, secretId: number) =>
    api.del(`/api/v1/instances/${id}/secrets/${secretId}`)
};

// Admin
export const admin = {
  templates: {
    create: (tmpl: Partial<Template>) => api.post<Template>('/api/v1/admin/templates', tmpl),
    update: (id: number, tmpl: Partial<Template>) => api.put<Template>(`/api/v1/admin/templates/${id}`, tmpl),
    delete: (id: number) => api.del(`/api/v1/admin/templates/${id}`),
    import: (json: unknown) => api.post<Template>('/api/v1/admin/templates/import', json)
  },
  servers: {
    list: () => api.get<Server[]>('/api/v1/admin/servers')
  },
  images: {
    list: (server?: string) => api.get<ImageSummary[]>(`/api/v1/admin/images${server ? `?server=${server}` : ''}`)
  },
  users: {
    list: () => api.get<User[]>('/api/v1/admin/users'),
    update: (id: number, name: string, role: string) =>
      api.put(`/api/v1/admin/users/${id}`, { name, role }),
    delete: (id: number) => api.del(`/api/v1/admin/users/${id}`)
  },
  instances: {
    list: () => api.get<Instance[]>('/api/v1/admin/instances'),
    create: (name: string, template_id: number, user_id: number) =>
      api.post<Instance>('/api/v1/admin/instances', { name, template_id, user_id }),
    incusInfo: (id: number) => api.get<IncusDetail>(`/api/v1/admin/instances/${id}/incus-info`)
  },
  managedKeys: {
    list: () => api.get<ManagedSSHKey[]>('/api/v1/admin/managed-keys'),
    generate: (name: string) =>
      api.post<GeneratedManagedKeyResult>('/api/v1/admin/managed-keys', { name }),
    delete: (id: number) => api.del(`/api/v1/admin/managed-keys/${id}`),
    getUsers: (id: number) => api.get<{ user_ids: number[] }>(`/api/v1/admin/managed-keys/${id}/users`),
    setUsers: (id: number, user_ids: number[]) =>
      api.put(`/api/v1/admin/managed-keys/${id}/users`, { user_ids })
  }
};
