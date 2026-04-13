export interface User {
  id: number;
  email: string;
  name: string;
  role: 'admin' | 'user';
  created_at: string;
  updated_at: string;
}

export interface SSHKey {
  id: number;
  user_id: number;
  name: string;
  public_key: string;
  created_at: string;
}

// Plati-generated keypair owned by a user (advanced users).
export interface UserSSHKey {
  id: number;
  user_id: number;
  name: string;
  public_key: string;
  created_at: string;
}

// Response from POST /ssh-keys/generate — private key shown once.
export interface GeneratedKeyResult {
  id: number;
  name: string;
  public_key: string;
  private_key: string;
}

// Response from POST /admin/users/{id}/keys/generate — private key stays on server.
export interface AdminGeneratedUserKeyResult {
  id: number;
  name: string;
  public_key: string;
  created_at: string;
}

// Admin-managed shared keypair.
export interface ManagedSSHKey {
  id: number;
  name: string;
  public_key: string;
  created_at: string;
}

// Response from POST /admin/managed-keys — private key shown once.
export interface GeneratedManagedKeyResult {
  id: number;
  name: string;
  public_key: string;
  private_key: string;
}

export interface Secret {
  id: number;
  user_id: number;
  name: string;
  created_at: string;
  updated_at: string;
}

export interface Server {
  id: number;
  name: string;
  endpoint: string;
  max_instances: number;
  is_online: boolean;
  instance_count?: number;
  created_at: string;
  updated_at: string;
}

export interface Template {
  id: number;
  name: string;
  slug: string;
  description: string;
  image: string;
  profiles: string;
  resources: string;
  /** Non-root login user for the second terminal button. Empty = root only. */
  terminal_user: string;
  /** JSON array of shell commands run via incus exec after instance starts (deprecated). */
  post_create_commands: string;
  /** "normal" | "ephemeral" */
  persistence_mode: string;
  /** JSON array of {path, size, pool} objects */
  persistence_dirs: string;
  /** JSON array of commands run only on first create */
  first_init_commands: string;
  /** JSON array of commands run on rebuild */
  rebuild_commands: string;
  /** JSON array of mixin names included by this template */
  includes: string;
  /** JSON array of {name, dest} repo refs */
  repos: string;
  /** JSON array of health check objects */
  health_checks: string;
  /** JSON object {port, funnel} for Tailscale Serve */
  tailscale_serve: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface MixinFileInfo {
  src: string;
  dest: string;
  mode: string;
}

export interface MixinInfo {
  name: string;
  commands: string[];
  files: MixinFileInfo[];
}

export interface ExecResult {
  output: string;
  error?: string;
}

export interface UserPreferences {
  user_id: number;
  /** "plati" | "personal" */
  ssh_key_mode: string;
  /** "plati" | "personal" */
  tailscale_mode: string;
  updated_at: string;
}

export interface Instance {
  id: number;
  name: string;
  user_id: number;
  template_id: number;
  server_id: number;
  volume_id: number | null;
  incus_name: string;
  status: 'creating' | 'running' | 'stopped' | 'error';
  ip_address: string | null;
  creation_log: string;
  last_active_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface InstanceStats {
  has_git: boolean;
  git_modified: number;
  disk_used: string;
}

export interface SshxURLResult {
  url: string;
}

export interface TailscaleServeRequest {
  port: number;
}

export interface TailscaleServeResult {
  status: string;
  url: string;
  port: number;
}

export interface TailscaleStatusResult {
  connected: boolean;
  dns_name: string;
  machine_name: string;
}

export interface InstanceSecret {
  id: number;
  instance_id: number;
  name: string;
  created_at: string;
  updated_at: string;
}

export interface IncusNetworkAddress {
  family: string;
  address: string;
  scope: string;
}

export interface IncusNetworkInterface {
  addresses: IncusNetworkAddress[];
  rx_bytes: number;
  tx_bytes: number;
}

export interface IncusDetail {
  type: string;
  architecture: string;
  profiles: string[];
  limits_cpu: string;
  limits_memory: string;
  limits_memory_swap: string;
  limits_processes: string;
  boot_autostart: string;
  boot_autostart_delay: string;
  security_nesting: string;
  security_privileged: string;
  cpu_usage_ns: number;
  memory_usage: number;
  memory_peak: number;
  processes: number;
  network: Record<string, IncusNetworkInterface>;
  disk_usage: Record<string, number>;
}

export interface IncusConfigUpdate {
  limits_cpu: string;
  limits_memory: string;
  limits_memory_swap: string;
  limits_processes: string;
  boot_autostart: string;
  boot_autostart_delay: string;
  security_nesting: string;
  security_privileged: string;
}

export interface ImageSummary {
  fingerprint: string;
  aliases: string[];
  size: number;
  description: string;
  properties: Record<string, string>;
}

export interface SetupEntra {
  client_id: string;
  client_secret: string;
  tenant_id: string;
}

export interface SetupServer {
  name: string;
  endpoint: string;
  tls_client_cert: string;
  tls_client_key: string;
  max_instances: number;
}

export interface SetupRequest {
  admin_password: string;
  entra?: SetupEntra;
  server?: SetupServer;
}

export interface VolumeDetail {
  volume_id: number;
  mount_path: string;
  device_name: string;
  volume_name: string;
  pool: string;
  size_gb: number;
  created_at: string;
}

export interface InstanceStorageInfo {
  persistence_mode: string;
  volumes: VolumeDetail[];
}

export interface FileEntry {
  id: string;
  name: string;
  type: 'file' | 'folder';
  size: number;
  date: number;
}

export interface VolumeSnapshot {
  name: string;
  created_at: string;
}

export interface DiskInfo {
  volume_id: number;
  volume_name: string;
  pool: string;
  size_gb: number;
  mount_path: string;
  device_name: string;
  instance_id: number;
  instance_name: string;
  incus_name: string;
  status: string;
  user_id: number;
  user_email: string;
  user_name: string;
  server_id: number;
  server_name: string;
  created_at: string;
}

export interface GitRepo {
  id: number;
  name: string;
  ssh_url: string;
  local_path: string;
  clone_status: 'pending' | 'cloning' | 'ready' | 'error';
  error_message?: string | null;
  last_synced_at?: string | null;
  created_at: string;
  updated_at: string;
}
