import { Component, inject, input } from '@angular/core';
import { Router } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { ActionsService, DialogService, Vm } from '../core';

/** The ACTIONS dropdown of a virtual machine. */
@Component({
  selector: 'app-vm-actions',
  imports: [ClarityModule],
  template: `
    <clr-dropdown>
      <button class="btn btn-link btn-sm fc-actions" clrDropdownTrigger>
        Actions <cds-icon shape="angle" direction="down" size="12"></cds-icon>
      </button>
      <clr-dropdown-menu *clrIfOpen clrPosition="bottom-left">
        <h4 class="dropdown-header">Actions - {{ vm().name }}</h4>
        @if (vm().template) {
          <button clrDropdownItem (click)="dialogs.cloneVm.set({ vm: vm(), template: false })">New VM from This Template...</button>
          <button clrDropdownItem (click)="actions.setTemplate(vm(), false)">Convert to Virtual Machine</button>
          <div class="dropdown-divider" role="separator"></div>
        }
        <button clrDropdownItem [clrDisabled]="!on()" (click)="actions.openConsole(vm())">Open console</button>
        <button clrDropdownItem [clrDisabled]="!on()" (click)="actions.openConsoleTab(vm().host_id, vm().id)">Open console in a new tab</button>
        <div class="dropdown-divider" role="separator"></div>
        <clr-dropdown>
          <button clrDropdownTrigger>Power</button>
          <clr-dropdown-menu *clrIfOpen clrPosition="right-top">
            <button clrDropdownItem [clrDisabled]="on() || !!vm().template" (click)="actions.vmPower(vm(), 'on')">Power On</button>
            <button clrDropdownItem [clrDisabled]="!on()" (click)="actions.vmPower(vm(), 'off')">Power Off</button>
            <button clrDropdownItem [clrDisabled]="!on()" (click)="actions.vmPower(vm(), 'suspend')">Suspend</button>
            <button clrDropdownItem [clrDisabled]="!on()" (click)="actions.vmPower(vm(), 'reset')">Reset</button>
            <div class="dropdown-divider" role="separator"></div>
            <button clrDropdownItem [clrDisabled]="!on()" (click)="actions.vmPower(vm(), 'shutdown_guest')">Shut Down Guest OS</button>
            <button clrDropdownItem [clrDisabled]="!on()" (click)="actions.vmPower(vm(), 'reboot_guest')">Restart Guest OS</button>
          </clr-dropdown-menu>
        </clr-dropdown>
        <clr-dropdown>
          <button clrDropdownTrigger>Snapshots</button>
          <clr-dropdown-menu *clrIfOpen clrPosition="right-top">
            <button clrDropdownItem (click)="dialogs.takeSnapshot.set(vm())">Take Snapshot...</button>
            <button clrDropdownItem (click)="manageSnapshots()">Manage Snapshots</button>
            <button clrDropdownItem [clrDisabled]="!vm().snapshot_count" (click)="actions.removeAllSnapshots(vm())">Delete All Snapshots</button>
          </clr-dropdown-menu>
        </clr-dropdown>
        <div class="dropdown-divider" role="separator"></div>
        @if (!vm().template) {
          <clr-dropdown>
            <button clrDropdownTrigger>Clone</button>
            <clr-dropdown-menu *clrIfOpen clrPosition="right-top">
              <button clrDropdownItem [clrDisabled]="!off()" (click)="dialogs.cloneVm.set({ vm: vm(), template: false })">Clone to Virtual Machine...</button>
              <button clrDropdownItem [clrDisabled]="!off()" (click)="dialogs.cloneVm.set({ vm: vm(), template: true })">Clone to Template...</button>
              <button clrDropdownItem [clrDisabled]="!off()" (click)="actions.setTemplate(vm(), true)">Convert to Template</button>
            </clr-dropdown-menu>
          </clr-dropdown>
        }
        @if (!vm().template) {
          <clr-dropdown>
            <button clrDropdownTrigger>Backup</button>
            <clr-dropdown-menu *clrIfOpen clrPosition="right-top">
              <button clrDropdownItem (click)="dialogs.protectVm.set({ vm: vm(), schedule: false })">Back Up Now...</button>
              <button clrDropdownItem (click)="dialogs.protectVm.set({ vm: vm(), schedule: true })">Schedule Backup or Snapshot...</button>
            </clr-dropdown-menu>
          </clr-dropdown>
        }
        <button clrDropdownItem (click)="dialogs.migrateVm.set(vm())">Migrate...</button>
        <button clrDropdownItem (click)="dialogs.relocateVm.set(vm())">Relocate...</button>
        <button clrDropdownItem (click)="dialogs.editVm.set(vm())">Edit Settings...</button>
        <div class="dropdown-divider" role="separator"></div>
        <button clrDropdownItem (click)="actions.deleteVm(vm())">Delete from Disk</button>
      </clr-dropdown-menu>
    </clr-dropdown>
  `,
})
export class VmActionsComponent {
  protected readonly actions = inject(ActionsService);
  protected readonly dialogs = inject(DialogService);
  private readonly router = inject(Router);

  readonly vm = input.required<Vm>();

  protected on(): boolean {
    return this.vm().power_state === 'poweredOn';
  }

  protected off(): boolean {
    return this.vm().power_state === 'poweredOff';
  }

  protected manageSnapshots(): void {
    void this.router.navigate(['/vm', this.vm().host_id, this.vm().id, 'snapshots']);
  }
}
