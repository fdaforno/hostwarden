import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, firstValueFrom } from 'rxjs';

import { AuthService } from './auth.service';
import {
  Alarm,
  Backup,
  BackupRequest,
  CloneRequest,
  CloudImageCatalogEntry,
  CreateSnapshotRequest,
  CreateVmRequest,
  Datastore,
  DatastoreHealth,
  DownloadStatus,
  FileInfo,
  GuestOs,
  Host,
  HostEvent,
  HostNetwork,
  HostPowerAction,
  HostRequest,
  HostService,
  HostStatus,
  HostTestResult,
  HostTime,
  JobStatus,
  MigrateRequest,
  MigrationCheck,
  Network,
  RelocateCheck,
  RelocateRequest,
  OIDCConfig,
  PerfData,
  PowerState,
  Role,
  Schedule,
  ServerTask,
  Snapshot,
  UpdateVmRequest,
  User,
  Vm,
  VmPowerAction,
} from './models';

const BASE = '/api/v1';

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

@Injectable({ providedIn: 'root' })
export class ApiService {
  private readonly http = inject(HttpClient);
  private readonly auth = inject(AuthService);

  // ---- auth ----
  async login(username: string, password: string): Promise<void> {
    const res = await this.send(
      this.http.post<{ token: string; username: string; role: string; must_change_password: boolean }>(`${BASE}/auth/login`, { username, password }),
      false,
    );
    this.auth.setSession(res.token, res.username, res.role, res.must_change_password);
  }

  async logout(): Promise<void> {
    try {
      await this.send(this.http.post(`${BASE}/auth/logout`, {}), false);
    } finally {
      this.auth.clearSession();
    }
  }

  async changePassword(current: string, next: string): Promise<void> {
    await this.send(this.http.post(`${BASE}/auth/password`, { current, new: next }), false);
    this.auth.setMustChangePassword(false);
  }

  // ---- users (administrators only) ----
  users(): Promise<User[]> {
    return this.send(this.http.get<User[] | null>(`${BASE}/users`)).then((r) => r ?? []);
  }
  createUser(username: string, password: string, role: string): Promise<User> {
    return this.send(this.http.post<User>(`${BASE}/users`, { username, password, role }));
  }
  updateUser(id: string, change: { role?: string; password?: string }): Promise<unknown> {
    return this.send(this.http.put(`${BASE}/users/${encodeURIComponent(id)}`, change));
  }
  deleteUser(id: string): Promise<unknown> {
    return this.send(this.http.delete(`${BASE}/users/${encodeURIComponent(id)}`));
  }

  // ---- OIDC configuration (administrators only) ----
  oidcConfig(): Promise<OIDCConfig> {
    return this.send(this.http.get<OIDCConfig>(`${BASE}/oidc/config`));
  }
  updateOIDCConfig(cfg: OIDCConfig): Promise<OIDCConfig> {
    return this.send(this.http.put<OIDCConfig>(`${BASE}/oidc/config`, cfg));
  }

  // ---- roles (administrators only for write) ----
  roles(): Promise<Role[]> {
    return this.send(this.http.get<Role[] | null>(`${BASE}/roles`)).then((r) => r ?? []);
  }
  createRole(role: Role): Promise<Role> {
    return this.send(this.http.post<Role>(`${BASE}/roles`, role));
  }
  updateRole(id: string, role: Role): Promise<Role> {
    return this.send(this.http.put<Role>(`${BASE}/roles/${encodeURIComponent(id)}`, role));
  }
  deleteRole(id: string): Promise<unknown> {
    return this.send(this.http.delete(`${BASE}/roles/${encodeURIComponent(id)}`));
  }

  // ---- hosts ----
  hosts(): Promise<Host[]> {
    return this.send(this.http.get<Host[]>(`${BASE}/hosts`));
  }
  hostStatus(hid: string): Promise<HostStatus> {
    return this.send(this.http.get<HostStatus>(this.host(hid)));
  }
  createHost(req: HostRequest): Promise<Host> {
    return this.send(this.http.post<Host>(`${BASE}/hosts`, req));
  }
  updateHost(hid: string, req: Partial<HostRequest>): Promise<Host> {
    return this.send(this.http.put<Host>(this.host(hid), req));
  }
  deleteHost(hid: string): Promise<unknown> {
    return this.send(this.http.delete(this.host(hid)));
  }
  testHost(hid: string): Promise<HostTestResult> {
    return this.send(this.http.get<HostTestResult>(`${this.host(hid)}/test`));
  }
  hostPower(hid: string, action: HostPowerAction, reason: string): Promise<unknown> {
    return this.send(this.http.post(`${this.host(hid)}/power`, { action, reason }));
  }
  esxcli(hid: string, command: string): Promise<{ output: string }> {
    return this.send(this.http.post<{ output: string }>(`${this.host(hid)}/esxcli`, { command }));
  }
  datastores(hid: string): Promise<Datastore[]> {
    return this.send(this.http.get<Datastore[] | null>(`${this.host(hid)}/datastores`)).then((r) => r ?? []);
  }
  /** Health (SMART/NVMe) of the devices backing one datastore. */
  datastoreHealth(hid: string, ds: string): Promise<DatastoreHealth> {
    return this.send(this.http.get<DatastoreHealth>(`${this.ds(hid, ds)}/health`));
  }
  networks(hid: string): Promise<Network[]> {
    return this.send(this.http.get<Network[] | null>(`${this.host(hid)}/networks`)).then((r) => r ?? []);
  }
  guestOs(hid: string): Promise<GuestOs[]> {
    return this.send(this.http.get<GuestOs[] | null>(`${this.host(hid)}/guest-os`)).then((r) => r ?? []);
  }

  // ---- datastore files ----
  files(hid: string, ds: string, path: string): Promise<FileInfo[]> {
    return this.send(this.http.get<FileInfo[] | null>(`${this.ds(hid, ds)}/files`, { params: { path } })).then(
      (r) => r ?? [],
    );
  }
  /** Adds an existing VM to the inventory from its .vmx file on a datastore. */
  registerVm(hid: string, datastore: string, path: string): Promise<Vm> {
    return this.send(this.http.post<Vm>(`${this.host(hid)}/vms/register`, { datastore, path }));
  }
  /** Content of a datastore file (administrators only). */
  downloadFile(hid: string, ds: string, path: string): Promise<Blob> {
    return this.send(this.http.get(`${this.ds(hid, ds)}/files/download`, { params: { path }, responseType: 'blob' }));
  }
  deleteFile(hid: string, ds: string, path: string): Promise<unknown> {
    return this.send(this.http.delete(`${this.ds(hid, ds)}/files`, { params: { path } }));
  }
  uploadFile(hid: string, ds: string, path: string, file: File): Promise<unknown> {
    return this.send(this.http.post(`${this.ds(hid, ds)}/files/upload`, file, { params: { path } }));
  }
  downloadIso(hid: string, ds: string, url: string, folder: string, name: string): Promise<unknown> {
    return this.send(this.http.post(`${this.ds(hid, ds)}/iso/download`, { url, folder, name }));
  }
  downloadIsoStatus(hid: string, ds: string, folder: string, name: string): Promise<DownloadStatus> {
    return this.send(
      this.http.get<DownloadStatus>(`${this.ds(hid, ds)}/iso/download/status`, { params: { folder, name } }),
    );
  }

  // ---- cloud images ----
  cloudImageCatalog(): Promise<CloudImageCatalogEntry[]> {
    return this.send(this.http.get<CloudImageCatalogEntry[] | null>(`${BASE}/cloud-images`)).then((r) => r ?? []);
  }
  /** Downloads and converts an image on the server, then stores it as `name` (e.g. "cloud-images/x.vmdk"). */
  downloadCloudImage(hid: string, ds: string, url: string, name: string): Promise<unknown> {
    return this.send(this.http.post(`${this.ds(hid, ds)}/cloud-image/download`, { url, name }));
  }

  // ---- vms ----
  vms(hid: string): Promise<Vm[]> {
    return this.send(this.http.get<Vm[] | null>(`${this.host(hid)}/vms`)).then((r) => r ?? []);
  }
  vm(hid: string, vmid: string): Promise<Vm> {
    return this.send(this.http.get<Vm>(this.vmUrl(hid, vmid)));
  }
  createVm(hid: string, req: CreateVmRequest): Promise<Vm> {
    return this.send(this.http.post<Vm>(`${this.host(hid)}/vms`, req));
  }
  updateVm(hid: string, vmid: string, req: UpdateVmRequest): Promise<Vm> {
    return this.send(this.http.put<Vm>(this.vmUrl(hid, vmid), req));
  }
  deleteVm(hid: string, vmid: string): Promise<unknown> {
    return this.send(this.http.delete(this.vmUrl(hid, vmid)));
  }
  powerVm(hid: string, vmid: string, action: VmPowerAction): Promise<{ power_state: PowerState }> {
    return this.send(this.http.post<{ power_state: PowerState }>(`${this.vmUrl(hid, vmid)}/power`, { action }));
  }

  // ---- migration between hosts ----
  migrateCheck(hid: string, vmid: string, req: MigrateRequest): Promise<MigrationCheck> {
    return this.send(this.http.post<MigrationCheck>(`${this.vmUrl(hid, vmid)}/migrate/check`, req));
  }
  migrate(hid: string, vmid: string, req: MigrateRequest): Promise<JobStatus> {
    return this.send(this.http.post<JobStatus>(`${this.vmUrl(hid, vmid)}/migrate`, req));
  }
  // ---- relocation to another datastore of the same host ----
  relocateCheck(hid: string, vmid: string, req: RelocateRequest): Promise<RelocateCheck> {
    return this.send(this.http.post<RelocateCheck>(`${this.vmUrl(hid, vmid)}/relocate/check`, req));
  }
  relocate(hid: string, vmid: string, req: RelocateRequest): Promise<JobStatus> {
    return this.send(this.http.post<JobStatus>(`${this.vmUrl(hid, vmid)}/relocate`, req));
  }
  clone(hid: string, vmid: string, req: CloneRequest): Promise<JobStatus> {
    return this.send(this.http.post<JobStatus>(`${this.vmUrl(hid, vmid)}/clone`, req));
  }
  /** Status of a background job started by migrate() or clone(). */
  job(id: string): Promise<JobStatus> {
    return this.send(this.http.get<JobStatus>(`${BASE}/jobs/${encodeURIComponent(id)}`));
  }

  // ---- backups and schedules ----
  backupVm(hid: string, vmid: string, req: BackupRequest): Promise<JobStatus> {
    return this.send(this.http.post<JobStatus>(`${this.vmUrl(hid, vmid)}/backup`, req));
  }
  backups(): Promise<Backup[]> {
    return this.send(this.http.get<Backup[] | null>(`${BASE}/backups`)).then((r) => r ?? []);
  }
  deleteBackup(b: Backup): Promise<unknown> {
    return this.send(this.http.delete(this.backupUrl(b)));
  }
  restoreBackup(b: Backup, name: string, datastore: string, power_on: boolean): Promise<JobStatus> {
    return this.send(this.http.post<JobStatus>(`${this.backupUrl(b)}/restore`, { name, datastore, power_on }));
  }
  schedules(): Promise<Schedule[]> {
    return this.send(this.http.get<Schedule[] | null>(`${BASE}/schedules`)).then((r) => r ?? []);
  }
  saveSchedule(s: Schedule): Promise<Schedule> {
    return this.send(s.id ? this.http.put<Schedule>(`${BASE}/schedules/${encodeURIComponent(s.id)}`, s) : this.http.post<Schedule>(`${BASE}/schedules`, s));
  }
  deleteSchedule(id: string): Promise<unknown> {
    return this.send(this.http.delete(`${BASE}/schedules/${encodeURIComponent(id)}`));
  }
  runSchedule(id: string): Promise<unknown> {
    return this.send(this.http.post(`${BASE}/schedules/${encodeURIComponent(id)}/run`, {}));
  }

  // ---- host configuration ----
  services(hid: string): Promise<HostService[]> {
    return this.send(this.http.get<HostService[] | null>(`${this.host(hid)}/services`)).then((r) => r ?? []);
  }
  setService(hid: string, key: string, req: { action?: 'start' | 'stop' | 'restart'; policy?: 'on' | 'off' }): Promise<unknown> {
    return this.send(this.http.post(`${this.host(hid)}/services/${encodeURIComponent(key)}`, req));
  }
  setMaintenance(hid: string, enabled: boolean): Promise<unknown> {
    return this.send(this.http.post(`${this.host(hid)}/maintenance`, { enabled }));
  }
  hostNetwork(hid: string): Promise<HostNetwork> {
    return this.send(this.http.get<HostNetwork>(`${this.host(hid)}/network`));
  }
  addPortGroup(hid: string, name: string, vswitch: string, vlan: number): Promise<unknown> {
    return this.send(this.http.post(`${this.host(hid)}/network/portgroups`, { name, vswitch, vlan }));
  }
  setPortGroupVlan(hid: string, name: string, vlan: number): Promise<unknown> {
    return this.send(this.http.put(`${this.host(hid)}/network/portgroups/${encodeURIComponent(name)}`, { vlan }));
  }
  removePortGroup(hid: string, name: string): Promise<unknown> {
    return this.send(this.http.delete(`${this.host(hid)}/network/portgroups/${encodeURIComponent(name)}`));
  }
  addVSwitch(hid: string, name: string, uplinks: string[]): Promise<unknown> {
    return this.send(this.http.post(`${this.host(hid)}/network/vswitches`, { name, uplinks }));
  }
  removeVSwitch(hid: string, name: string): Promise<unknown> {
    return this.send(this.http.delete(`${this.host(hid)}/network/vswitches/${encodeURIComponent(name)}`));
  }
  hostTime(hid: string): Promise<HostTime> {
    return this.send(this.http.get<HostTime>(`${this.host(hid)}/time`));
  }
  setHostTime(hid: string, ntp_enabled: boolean, ntp_servers: string[]): Promise<unknown> {
    return this.send(this.http.put(`${this.host(hid)}/time`, { ntp_enabled, ntp_servers }));
  }

  // ---- monitoring (of the host, or of one VM when vmid is given) ----
  performance(hid: string, vmid?: string): Promise<PerfData> {
    return this.send(this.http.get<PerfData>(`${vmid ? this.vmUrl(hid, vmid) : this.host(hid)}/performance`));
  }
  events(hid: string, vmid?: string): Promise<HostEvent[]> {
    return this.send(this.http.get<HostEvent[] | null>(`${vmid ? this.vmUrl(hid, vmid) : this.host(hid)}/events`)).then((r) => r ?? []);
  }

  // ---- recent tasks ----
  tasks(): Promise<ServerTask[]> {
    return this.send(this.http.get<ServerTask[] | null>(`${BASE}/tasks`)).then((r) => r ?? []);
  }

  // ---- alarms (active disk-health conditions, aggregated server-side) ----
  alarms(): Promise<Alarm[]> {
    return this.send(this.http.get<Alarm[] | null>(`${BASE}/alarms`)).then((r) => r ?? []);
  }
  /** Trigger an immediate disk-health probe and return the fresh alarms. */
  refreshAlarms(): Promise<Alarm[]> {
    return this.send(this.http.post<Alarm[] | null>(`${BASE}/alarms/refresh`, null)).then((r) => r ?? []);
  }

  // ---- snapshots ----
  snapshots(hid: string, vmid: string): Promise<Snapshot[]> {
    return this.send(this.http.get<Snapshot[] | null>(`${this.vmUrl(hid, vmid)}/snapshots`)).then((r) => r ?? []);
  }
  createSnapshot(hid: string, vmid: string, req: CreateSnapshotRequest): Promise<unknown> {
    return this.send(this.http.post(`${this.vmUrl(hid, vmid)}/snapshots`, req));
  }
  revertSnapshot(hid: string, vmid: string, snapId: string): Promise<unknown> {
    return this.send(this.http.post(`${this.vmUrl(hid, vmid)}/snapshots/${encodeURIComponent(snapId)}/revert`, {}));
  }
  deleteSnapshot(hid: string, vmid: string, snapId: string): Promise<unknown> {
    return this.send(this.http.delete(`${this.vmUrl(hid, vmid)}/snapshots/${encodeURIComponent(snapId)}`));
  }
  removeAllSnapshots(hid: string, vmid: string): Promise<unknown> {
    return this.send(this.http.post(`${this.vmUrl(hid, vmid)}/snapshots/remove-all`, {}));
  }

  // ---- internals ----
  private host(hid: string): string {
    return `${BASE}/hosts/${encodeURIComponent(hid)}`;
  }
  private backupUrl(b: Backup): string {
    return `${this.host(b.host_id)}/backups/${encodeURIComponent(b.datastore)}/${encodeURIComponent(b.vm_name)}/${encodeURIComponent(b.stamp)}`;
  }
  private ds(hid: string, ds: string): string {
    return `${this.host(hid)}/datastores/${encodeURIComponent(ds)}`;
  }
  private vmUrl(hid: string, vmid: string): string {
    return `${this.host(hid)}/vms/${encodeURIComponent(vmid)}`;
  }

  /** Converts HTTP failures to ApiError and ends the session on 401. */
  private async send<T>(obs: Observable<T>, logoutOn401 = true): Promise<T> {
    try {
      return await firstValueFrom(obs);
    } catch (e) {
      if (e instanceof HttpErrorResponse) {
        if (e.status === 401 && logoutOn401) {
          this.auth.clearSession();
          throw new ApiError('Session expired', 401);
        }
        const body = e.error as { error?: string } | null;
        throw new ApiError(body?.error || (e.status ? `HTTP ${e.status}` : 'Server unreachable'), e.status);
      }
      throw e;
    }
  }
}
