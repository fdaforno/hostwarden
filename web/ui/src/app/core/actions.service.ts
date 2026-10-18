import { Injectable, inject } from '@angular/core';
import { Router } from '@angular/router';

import { ApiService, errorMessage } from './api.service';
import { DialogService } from './dialog.service';
import { InventoryService } from './inventory.service';
import { Backup, BackupRequest, CloneRequest, Host, HostPowerAction, JobStatus, MigrateRequest, RelocateRequest, Vm, VmPowerAction } from './models';
import { TaskService } from './task.service';

const JOB_POLL_MS = 3000;

const VM_POWER_TASK: Record<VmPowerAction, string> = {
  on: 'Power On virtual machine',
  off: 'Power Off virtual machine',
  suspend: 'Suspend virtual machine',
  reset: 'Reset virtual machine',
  shutdown_guest: 'Initiate guest OS shutdown',
  reboot_guest: 'Initiate guest OS reboot',
};

/**
 * Object actions shared by menus, toolbars and grids. Failures are recorded in
 * Recent Tasks (which opens itself), so callers never need to handle errors.
 */
@Injectable({ providedIn: 'root' })
export class ActionsService {
  private readonly api = inject(ApiService);
  private readonly tasks = inject(TaskService);
  private readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);
  private readonly router = inject(Router);

  async vmPower(vm: Vm, action: VmPowerAction): Promise<void> {
    if (action === 'off') {
      const ok = await this.dialogs.confirm({
        title: 'Confirm Power Off',
        message: `Power off ${vm.name}? The guest operating system is not shut down gracefully and data may be lost.`,
        okLabel: 'Yes',
        danger: true,
      });
      if (!ok) return;
    }
    await this.track(VM_POWER_TASK[action], vm.name, () => this.api.powerVm(vm.host_id, vm.id, action));
  }

  /** Opens the VM's web console in a floating window over the current page. */
  openConsole(vm: Vm): void {
    this.dialogs.console.set({ hostId: vm.host_id, vmId: vm.id });
  }

  /** Opens a VM's web console in a new browser tab. */
  openConsoleTab(hostId: string, vmId: string): void {
    const url = this.router.serializeUrl(this.router.createUrlTree(['/console', hostId, vmId]));
    window.open(`#${url}`, '_blank', 'noopener');
  }

  /**
   * Starts a migration and follows it in Recent Tasks until it ends. The job
   * runs on the server, so it continues even if this page is closed.
   */
  async migrateVm(vm: Vm, req: MigrateRequest, targetHostName: string): Promise<void> {
    let newId = '';
    const done = await this.track('Migrate virtual machine', `${vm.name} to ${targetHostName}`, async () => {
      const job = await this.followJob(await this.api.migrate(vm.host_id, vm.id, req));
      newId = job.new_vm_id ?? '';
    });
    // Follow the VM to its new host when the user is still looking at the old one.
    if (done && newId && this.router.url.startsWith(`/vm/${vm.host_id}/${vm.id}/`)) {
      void this.router.navigate(['/vm', req.target_host_id, newId]);
    }
  }

  /**
   * Starts a relocation to another datastore of the same host and follows it
   * in Recent Tasks until it ends.
   */
  async relocateVm(vm: Vm, req: RelocateRequest, targetDatastore: string): Promise<void> {
    let newId = '';
    const done = await this.track('Relocate virtual machine', `${vm.name} to ${targetDatastore}`, async () => {
      const job = await this.followJob(await this.api.relocate(vm.host_id, vm.id, req));
      newId = job.new_vm_id ?? '';
    });
    // The VM stays on the same host, but its MoRef changes: follow the new id.
    if (done && newId && this.router.url.startsWith(`/vm/${vm.host_id}/${vm.id}/`)) {
      void this.router.navigate(['/vm', vm.host_id, newId]);
    }
  }

  /** Clones a powered-off VM (optionally as a template) and follows the job in Recent Tasks. */
  async cloneVm(vm: Vm, req: CloneRequest): Promise<void> {
    await this.track(req.template ? 'Clone virtual machine to template' : 'Clone virtual machine', `${vm.name} to ${req.name}`, async () =>
      this.followJob(await this.api.clone(vm.host_id, vm.id, req)),
    );
  }

  /** Backs a VM up now (running VMs are not stopped) and follows the job in Recent Tasks. */
  async backupVm(vm: Vm, req: BackupRequest): Promise<void> {
    await this.track('Back up virtual machine', vm.name, async () => this.followJob(await this.api.backupVm(vm.host_id, vm.id, req)));
  }

  /** Creates a new VM from a backup. */
  async restoreBackup(b: Backup, name: string, datastore: string, powerOn: boolean): Promise<void> {
    await this.track('Restore virtual machine from backup', `${b.vm_name} to ${name}`, async () =>
      this.followJob(await this.api.restoreBackup(b, name, datastore, powerOn)),
    );
  }

  /** Converts a powered-off VM to a template, or a template back to a VM. */
  async setTemplate(vm: Vm, template: boolean): Promise<void> {
    await this.track(template ? 'Mark virtual machine as template' : 'Mark as virtual machine', vm.name, () =>
      this.api.updateVm(vm.host_id, vm.id, {
        template,
        resize_disks: [],
        add_disks: [],
        remove_disk_keys: [],
        delete_disk_files: false,
        add_nics: [],
        update_nics: [],
        remove_nic_keys: [],
        add_cdroms: [],
        update_cdroms: [],
        remove_cdrom_keys: [],
      }),
    );
  }

  /** Waits for a server-side job to end (its progress is shown by its task); throws when the job failed. */
  private async followJob(job: JobStatus): Promise<JobStatus> {
    while (job.status === 'running') {
      await new Promise((r) => setTimeout(r, JOB_POLL_MS));
      job = await this.api.job(job.id);
    }
    if (job.status === 'error') throw new Error(job.error || 'The operation failed');
    return job;
  }

  async deleteVm(vm: Vm): Promise<void> {
    if (vm.power_state !== 'poweredOff') {
      await this.dialogs.confirm({
        title: 'Delete from Disk',
        message: `${vm.name} must be powered off before it can be deleted.`,
        okLabel: 'OK',
      });
      return;
    }
    const ok = await this.dialogs.confirm({
      title: 'Confirm Delete',
      message: `Delete ${vm.name} and all of its files (${vm.vmx_path || 'datastore'}) from disk? This action is irreversible.`,
      okLabel: 'Yes',
      danger: true,
    });
    if (!ok) return;
    const done = await this.track('Delete virtual machine', vm.name, () => this.api.deleteVm(vm.host_id, vm.id));
    if (done) void this.router.navigate(['/host', vm.host_id, 'vms']);
  }

  async removeAllSnapshots(vm: Vm): Promise<void> {
    const ok = await this.dialogs.confirm({
      title: 'Delete All Snapshots',
      message: `Delete all snapshots of ${vm.name}? The virtual disks are consolidated.`,
      okLabel: 'Delete All',
      danger: true,
    });
    if (ok) await this.track('Remove all snapshots', vm.name, () => this.api.removeAllSnapshots(vm.host_id, vm.id));
  }

  async hostPower(host: Host, action: HostPowerAction): Promise<void> {
    const label = action === 'reboot' ? 'Reboot' : 'Shut Down';
    const reason = await this.dialogs.prompt({
      title: `${label} Host`,
      message: `${label} ${host.name}? Running virtual machines are not migrated.`,
      okLabel: 'OK',
      danger: true,
      promptLabel: 'Log a reason for this operation',
      promptDefault: 'Hostwarden UI',
    });
    if (reason === null) return;
    await this.track(`Initiate host ${action}`, host.name, () => this.api.hostPower(host.id, action, reason));
  }

  async setMaintenance(host: Host, enabled: boolean): Promise<void> {
    if (enabled) {
      const ok = await this.dialogs.confirm({
        title: 'Enter Maintenance Mode',
        message: `Put ${host.name} in maintenance mode? Its virtual machines must already be powered off or migrated: none is stopped for you.`,
        okLabel: 'OK',
      });
      if (!ok) return;
    }
    await this.track(enabled ? 'Enter maintenance mode' : 'Exit maintenance mode', host.name, () => this.api.setMaintenance(host.id, enabled));
  }

  async removeHost(host: Host): Promise<void> {
    const ok = await this.dialogs.confirm({
      title: 'Remove from Inventory',
      message: `Remove ${host.name} from Hostwarden? Virtual machines on the ESXi host are not touched.`,
      okLabel: 'Yes',
      danger: true,
    });
    if (!ok) return;
    const done = await this.track('Remove host', host.name, () => this.api.deleteHost(host.id));
    if (done) void this.router.navigate(['/root', 'hosts']);
  }

  async testHost(host: Host): Promise<void> {
    // A read-only check: not a task, the outcome is shown directly.
    let message: string;
    try {
      const r = await this.api.testHost(host.id);
      message = `API: ${r.api}. SSH: ${r.ssh}.`;
    } catch (e) {
      message = errorMessage(e);
    }
    await this.dialogs.confirm({ title: `Test Connection - ${host.name}`, message, okLabel: 'Close' });
  }

  /** Runs op as a task then refreshes the inventory. Returns false when the task failed. */
  async track(name: string, target: string, op: () => Promise<unknown>): Promise<boolean> {
    try {
      await this.tasks.run(name, target, op);
      return true;
    } catch {
      return false;
    } finally {
      void this.inventory.refresh();
    }
  }
}
