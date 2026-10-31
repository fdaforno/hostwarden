// TypeScript mirror of internal/model/model.go and the ad-hoc API payloads.

export type PowerState = 'poweredOn' | 'poweredOff' | 'suspended';
export type VmPowerAction = 'on' | 'off' | 'reset' | 'suspend' | 'shutdown_guest' | 'reboot_guest';
export type HostPowerAction = 'reboot' | 'shutdown';

export interface HostStatus {
  id: string;
  name: string;
  address: string;
  connected: boolean;
  ssh_ok: boolean;
  product?: string;
  version?: string;
  build?: string;
  cpu_model?: string;
  cpu_cores?: number;
  cpus?: number;
  cpu_mhz?: number;
  memory_total_mb?: number;
  memory_used_mb?: number;
  vms_running?: number;
  vms_total?: number;
  uptime_seconds?: number;
  boot_time?: string;
  maintenance_mode: boolean;
  connection_state?: string;
  license_name?: string;
  last_error?: string;
}

export interface Host {
  id: string;
  name: string;
  address: string;
  username: string;
  ssh_port: number;
  ssh_enabled: boolean;
  created_at: string;
  status: HostStatus | null;
}

export interface HostRequest {
  name: string;
  address: string;
  username: string;
  password: string;
  ssh_port: number;
  ssh_enabled: boolean;
}

export interface HostTestResult {
  api: string;
  ssh: string;
}

export interface VmDisk {
  key: number;
  label: string;
  capacity_gb: number;
  thin: boolean;
  mode: string;
  path: string;
  datastore: string;
}

export interface VmNic {
  key: number;
  label: string;
  network: string;
  adapter_type: string;
  mac_address: string;
  connected: boolean;
  start_connected: boolean;
}

export interface Vm {
  host_id: string;
  id: string;
  name: string;
  power_state: PowerState;
  num_cpus: number;
  cores_per_socket: number;
  memory_mb: number;
  guest_id: string;
  guest_full_name: string;
  hardware_version: string;
  uuid: string;
  annotation: string;
  vmx_path: string;
  ip_address?: string;
  guest_hostname?: string;
  tools_status?: string;
  uptime_seconds?: number;
  cpu_usage_mhz?: number;
  guest_memory_mb?: number;
  host_memory_mb?: number;
  datastores: string[] | null;
  networks: string[] | null;
  disks: VmDisk[] | null;
  nics: VmNic[] | null;
  cdrom_iso?: string;
  snapshot_count: number;
  boot_time?: string;
  // Detail only (single-VM endpoint):
  firmware?: string;
  boot_delay_ms?: number;
  cpu_hot_add?: boolean;
  memory_hot_add?: boolean;
  nested_hv?: boolean;
  usb_controller?: UsbController;
  cdroms?: VmCdrom[] | null;
  /** Marked as template: cannot be powered on, meant to be cloned. */
  template?: boolean;
}

export type UsbController = '' | 'usb2' | 'usb3';

export interface VmCdrom {
  key: number;
  label: string;
  /** Empty = client device (no media). */
  iso: string;
  start_connected: boolean;
}

export interface Snapshot {
  id: string;
  name: string;
  path: string;
  description: string;
  create_time?: string;
  has_children: boolean;
}

export interface Datastore {
  name: string;
  type: string;
  capacity_bytes: number;
  free_bytes: number;
  url?: string;
  accessible: boolean;
}

// Disk health (SMART / NVMe), read over SSH by the server.
export interface DiskHealth {
  device_id: string;
  display_name?: string;
  form_factor?: string;
  /** Overall SMART assessment ("OK", "FAIL", ...). Empty when unknown. */
  smart_status?: string;
  reallocated_sectors?: number;
  pending_sectors?: number;
  uncorrectable?: number;
  /** NVMe: Media and Data Integrity Errors. */
  media_errors?: number;
  /** NVMe: percentage of endurance used (0-100). */
  percentage_used?: number;
  /** Degrees Celsius, when reported. */
  temperature?: number;
  temp_ok?: boolean;
  /** Controller exposes no SMART/health (typical of some NVMe). */
  unsupported?: boolean;
  error?: string;
}

export interface DatastoreHealth {
  datastore: string;
  host_id: string;
  devices: DiskHealth[];
}

/** A single active condition worth surfacing at the root summary. */
export interface Alarm {
  /** Source: "disk" so far. */
  kind: string;
  /** "danger" (red, compromising) or "warning". */
  severity: string;
  host_id: string;
  host_name: string;
  /** Object the alarm is about: a datastore for "disk" alarms. */
  target: string;
  message: string;
}

export interface Network {
  name: string;
  type: string;
}

export interface GuestOs {
  id: string;
  name: string;
}

export interface FileInfo {
  name: string;
  /** Parent folder of the entry, not its own path. */
  path: string;
  size: number;
  is_dir: boolean;
  mod_time?: string;
}

export interface CreateVmRequest {
  name: string;
  guest_id: string;
  num_cpus: number;
  memory_mb: number;
  datastore: string;
  disks: { size_gb: number; thin: boolean }[];
  network: string;
  adapter_type: string;
  scsi_controller: string;
  annotation?: string;
  iso_path: string;
  boot_from_cdrom?: boolean;
  /** Expose hardware assisted virtualization (nested virtualization) to the guest OS. */
  nested_hv?: boolean;
  power_on: boolean;
  /** Cloud image VMDK on the same datastore ("cloud-images/x.vmdk"): the boot disk is cloned from it. */
  cloud_image?: string;
  cloud_init?: CloudInitConfig;
}

/** Static IPv4 for the new VM's primary NIC; no guest interface name needed. */
export interface CloudInitNetwork {
  address: string;
  gateway?: string;
  dns_servers?: string[];
}

export interface CloudInitConfig {
  hostname?: string;
  ssh_authorized_keys?: string[];
  users?: { name: string; sudo?: string; shell?: string; password?: string; ssh_authorized_keys?: string[] }[];
  packages?: string[];
  /** NoCloud network-config, applied independently of user_data. */
  network_config?: string;
  /** Automatically bound to the new NIC; mutually exclusive with network_config. */
  network?: CloudInitNetwork;
  /** Raw user-data: replaces generated user-data, but not network_config or metadata. */
  user_data?: string;
}

export interface CloudImageCatalogEntry {
  distro: string;
  ver: string;
  variant: string;
  size: string;
  url: string;
}

/** Only the fields that are set are changed on the VM. */
export interface UpdateVmRequest {
  num_cpus?: number;
  cores_per_socket?: number;
  memory_mb?: number;
  annotation?: string;
  name?: string;
  guest_id?: string;
  firmware?: string;
  boot_delay_ms?: number;
  enter_bios_setup?: boolean;
  cpu_hot_add?: boolean;
  memory_hot_add?: boolean;
  nested_hv?: boolean;
  usb_controller?: UsbController;
  /** Convert to / from template. */
  template?: boolean;
  resize_disks: { key: number; size_gb: number }[];
  /** `datastore` unset = next to the virtual machine. */
  add_disks: { size_gb: number; thin: boolean; datastore?: string }[];
  remove_disk_keys: number[];
  /** Also delete the VMDK files of the removed disks. */
  delete_disk_files: boolean;
  add_nics: { network: string; adapter_type: string }[];
  update_nics: { key: number; network?: string; start_connected?: boolean }[];
  remove_nic_keys: number[];
  /** Empty `iso_path` = client device (no media). */
  add_cdroms: { iso_path: string }[];
  update_cdroms: { key: number; iso_path: string }[];
  remove_cdrom_keys: number[];
}

export interface CreateSnapshotRequest {
  name: string;
  description: string;
  memory: boolean;
  quiesce: boolean;
}

export type MigrationMode = 'cold' | 'suspend' | 'stop_start';

export interface MigrateRequest {
  target_host_id: string;
  target_datastore: string;
  mode?: MigrationMode;
  /** Delete the source files once the virtual machine is up on the target. */
  delete_source?: boolean;
  /** Re-thin the copied disks (the copy fully allocates them). */
  reclaim_thin?: boolean;
  /** Running VM: snapshot it and copy the disks before stopping it (shorter downtime). */
  precopy?: boolean;
}

export interface MigrationCheck {
  power_state: PowerState;
  source_cpu: string;
  target_cpu: string;
  cpu_compatible: boolean;
  /** The target datastore is the volume the VM is already on: nothing is copied. */
  shared: boolean;
  recommended_mode: MigrationMode;
  size_bytes: number;
  provisioned_gb: number;
  target_free_bytes: number;
  blockers: string[];
  warnings: string[];
}

/** Move a VM to another datastore of the same host. */
export interface RelocateRequest {
  target_datastore: string;
  mode?: MigrationMode;
  delete_source?: boolean;
  reclaim_thin?: boolean;
  precopy?: boolean;
}

/** Relocation checks reuse the migration-check shape. */
export type RelocateCheck = MigrationCheck;

export interface CloneRequest {
  name: string;
  /** Unset = datastore of the source. */
  datastore?: string;
  power_on: boolean;
  /** Mark the copy as a template. */
  template: boolean;
}

/** Where a backup goes: a datastore of the VM's host and, optionally, on to another host. */
export interface BackupRequest {
  datastore: string;
  target_host_id?: string;
  target_datastore?: string;
  /** Backups of the VM to keep at the destination (0 = all). */
  keep: number;
}

export interface Backup {
  host_id: string;
  datastore: string;
  vm_name: string;
  /** Folder name, YYYYMMDD-HHMMSS (UTC). */
  stamp: string;
  created: string;
  size_bytes: number;
}

export type ScheduleKind = 'snapshot' | 'backup';
export type ScheduleFrequency = 'hourly' | 'daily' | 'weekly';

export interface Schedule {
  id?: string;
  host_id: string;
  vm_id: string;
  vm_name?: string;
  kind: ScheduleKind;
  frequency: ScheduleFrequency;
  hour: number;
  minute: number;
  /** 0 = Sunday. */
  weekday: number;
  /** IANA zone the times are meant in. */
  timezone: string;
  keep: number;
  backup?: BackupRequest;
  enabled: boolean;
  next_run?: string;
  last_run?: string;
  last_status?: 'ok' | 'error' | '';
  last_error?: string;
}

/** A Hostwarden account. */
export interface User {
  id: string;
  username: string;
  role: string;
  oidc_sub?: string;
  created_at: string;
}

/** OIDC configuration (the client secret is never returned by the API). */
export interface OIDCConfig {
  enabled: boolean;
  issuer: string;
  client_id: string;
  client_secret?: string; // write-only: empty on read
  scopes?: string[];
  claim_name?: string;
  group_roles?: Record<string, string>; // Keycloak group → role ID
  default_role?: string;
}

/** A role with granular permissions. */
export interface Role {
  id: string;
  name: string;
  description?: string;
  builtin?: boolean;
  manage_vms: boolean;
  manage_hosts: boolean;
  manage_storage: boolean;
  manage_users: boolean;
  console_access: boolean;
}

export interface HostService {
  key: string;
  label: string;
  running: boolean;
  /** on = starts with the host. */
  policy: string;
}

export interface HostNic {
  device: string;
  mac: string;
  /** 0 = link down. */
  speed_mb: number;
  driver?: string;
}

export interface VSwitch {
  name: string;
  mtu: number;
  uplinks: string[];
  port_groups: string[];
}

export interface PortGroup {
  name: string;
  vswitch: string;
  vlan: number;
  /** Used by a VMkernel interface of the host: not changeable from here. */
  management: boolean;
  vms: number;
}

export interface HostNetwork {
  vswitches: VSwitch[];
  port_groups: PortGroup[];
  nics: HostNic[];
}

export interface HostTime {
  /** Host clock, UTC. */
  now: string;
  ntp_enabled: boolean;
  ntp_servers: string[];
}

export interface PerfSeries {
  key: string;
  label: string;
  unit: string;
  values: number[];
}

/** Real-time performance (last hour); `series[n].values[i]` belongs to `times[i]`. */
export interface PerfData {
  interval_seconds: number;
  times: string[];
  series: PerfSeries[];
}

/** An entry of the ESXi host's own event log. */
export interface HostEvent {
  time: string;
  category: 'info' | 'warning' | 'error' | 'user' | string;
  message: string;
  user?: string;
  target?: string;
}

/** An entry of the server's task log. */
export interface ServerTask {
  id: number;
  name: string;
  target: string;
  initiator: string;
  status: 'running' | 'ok' | 'error';
  details: string;
  started_at: string;
  finished_at?: string;
}

/** A long operation running on the server (migration, clone, backup). */
export interface JobStatus {
  id: string;
  kind: string;
  vm_name: string;
  status: 'running' | 'ok' | 'error';
  phase: string;
  bytes_total: number;
  bytes_done: number;
  new_vm_id?: string;
  error?: string;
  warnings: string[];
}

export interface DownloadStatus {
  status: 'running' | 'ok' | 'error' | string;
  error?: string;
}

/** A datastore or network as seen from one host (the API has no cross-host identity). */
export interface HostScoped<T> {
  host: Host;
  item: T;
}
