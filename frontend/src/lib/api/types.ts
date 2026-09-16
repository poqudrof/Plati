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

// A registered public key as seen from an instance, with install status.
export interface InstanceAuthorizedKey {
  id: number;
  name: string;
  public_key: string;
  present: boolean;
  /** Whose key this is — the instance owner, or a server administrator. */
  owner_name: string;
  owner_email?: string;
  is_admin: boolean;
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

// API key for programmatic access (agents/scripts). The secret itself is
// never returned except once, right after create/regenerate.
export interface ApiKey {
  id: number;
  user_id: number;
  name: string;
  key_prefix: string;
  last_used_at?: string;
  revoked_at?: string;
  created_at: string;
}

// Response from create/regenerate — the plaintext key is shown only once.
export interface GeneratedApiKeyResult {
  id: number;
  name: string;
  key_prefix: string;
  key: string;
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
  /** Account the mixin's commands run as: "root" (default) or "user" (the template's terminal_user). */
  run_as: 'root' | 'user';
  /** Incus instance config keys this mixin requires (e.g. security.nesting for Docker). */
  incus_config?: Record<string, string>;
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
  /** Auto-stop policy. 0 minutes means "follow the platform default". */
  sleep_disabled: boolean;
  sleep_timeout_minutes: number;
  /** Admin-set resource overrides. Empty means "inherit the template's value". */
  limits_cpu: string;
  limits_memory: string;
}

/**
 * An instance's resource limits and where each one comes from, so the UI can say
 * "from the template" or "set by an administrator" rather than showing a bare number.
 */
export interface InstanceResources {
  template_cpu: string;
  template_memory: string;
  /** Per-instance overrides; empty when the template's value is inherited. */
  override_cpu: string;
  override_memory: string;
  /** What Plati hands Incus. */
  effective_cpu: string;
  effective_memory: string;
  /** Read-only: Plati has no volume resize path, and the root fs is not size-limited. */
  template_disk: string;
  /** Whether the caller may change these — admins only. */
  editable: boolean;
  /** False when the value is stored but Incus has not taken it yet. */
  applied: boolean;
  warning?: string;
}

/** Limits to set on one instance. An empty field clears the override. */
export interface ResourceUpdate {
  limits_cpu: string;
  limits_memory: string;
}

/** Auto-stop (sleep) policy of one instance, resolved against the platform default. */
export interface SleepSettings {
  disabled: boolean;
  /** Per-instance override in minutes; 0 = follow default_minutes. */
  timeout_minutes: number;
  default_minutes: number;
  effective_minutes: number;
  /** How often the sleep worker sweeps — the worst-case overshoot on the deadline. */
  check_interval_minutes: number;
  status: string;
  last_active_at: string | null;
  /** When the worker becomes eligible to stop it; null when nothing is scheduled. */
  sleeps_at: string | null;
}

/** An instance as listed by the admin dashboard: owner and template resolved. */
export interface AdminInstance extends Instance {
  user_email: string;
  user_name: string;
  template_name: string;
}

/** Result of a rename: the instance, plus whether the Incus container followed. */
export interface RenameResult extends Instance {
  container_renamed: boolean;
  container_rename_error?: string;
  restarted: boolean;
  /** Hostname reported from inside the instance, when one was asked for. */
  system_hostname?: string;
}

export interface RenameOptions {
  /** Rename the Incus container too — stops and restarts the instance. */
  rename_container?: boolean;
  /** Rename the hostname inside Ubuntu — needs the instance running. */
  rename_system_hostname?: boolean;
}

export interface InstanceStats {
  has_git: boolean;
  git_modified: number;
  disk_used: string;
}

export interface SshxURLResult {
  url: string;
}

/** A hand-entered link. Pinned ones are listed on the dashboard card. */
export interface CustomLink {
  label: string;
  url: string;
  pinned: boolean;
}

/** Stored state behind the Links tab: which built-in links are pinned, and the custom ones. */
export interface InstanceLinkSettings {
  hostname: boolean;
  openvscode: boolean;
  sshx: boolean;
  custom: CustomLink[];
}

/** PUT body: omitted fields keep their value; `custom`, when present, replaces the list. */
export type InstanceLinksUpdate = Partial<Omit<InstanceLinkSettings, 'custom'>> & {
  custom?: (Omit<CustomLink, 'pinned'> & { pinned?: boolean })[];
};

export interface ResolvedLink {
  /** `hostname` is https://<tailnet name>/, openable only when something serves 443. */
  kind: 'hostname' | 'openvscode' | 'sshx' | 'custom';
  label: string;
  /** Empty when the link cannot be opened right now — `note` says why. */
  url: string;
  /** The tool's systemd unit is active (always true for custom and hostname links). */
  active: boolean;
  pinned: boolean;
  /** Position in `settings.custom` for a custom link, -1 otherwise. */
  index: number;
  note?: string;
}

export interface InstanceLinksResult {
  settings: InstanceLinkSettings;
  /** Every link, pinned or not. */
  links: ResolvedLink[];
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
  /** What `tailscale up` said when the machine is still logged out. */
  login_output?: string;
  /** tailscale CLI present in the instance. */
  installed: boolean;
  /** tailscaled running. */
  daemon_active: boolean;
}

export interface ProfileCheck {
  profile: string;
  server: string;
  exists: boolean;
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
