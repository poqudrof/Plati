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
  cloud_init: string;
  is_active: boolean;
  created_at: string;
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
  last_active_at: string | null;
  created_at: string;
  updated_at: string;
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
  cpu_usage_ns: number;
  memory_usage: number;
  memory_peak: number;
  processes: number;
  network: Record<string, IncusNetworkInterface>;
  disk_usage: Record<string, number>;
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
