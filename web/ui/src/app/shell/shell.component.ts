import { Component, OnDestroy, OnInit, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink, RouterOutlet } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { ApiService, AuthService, DialogService, InventoryService, TaskService } from '../core';
import { HostFormDialogComponent } from '../features/host/host-form-dialog.component';
import { IsoDownloadDialogComponent } from '../features/datastore/iso-download-dialog.component';
import { ProtectVmDialogComponent } from '../features/vm/protect-vm-dialog.component';
import { CloneVmDialogComponent } from '../features/vm/clone-vm-dialog.component';
import { EditVmDialogComponent } from '../features/vm/edit-vm-dialog.component';
import { MigrateVmDialogComponent } from '../features/vm/migrate-vm-dialog.component';
import { RelocateVmDialogComponent } from '../features/vm/relocate-vm-dialog.component';
import { NewVmWizardComponent } from '../features/vm/new-vm-wizard.component';
import { SnapshotDialogComponent } from '../features/vm/snapshot-dialog.component';
import { ConsoleWindowComponent } from '../features/console/console-window.component';
import { ChangePasswordDialogComponent } from '../features/admin/change-password-dialog.component';
import { ConfirmDialogComponent } from './confirm-dialog.component';
import { InventoryTreeComponent } from './inventory-tree.component';
import { RecentTasksComponent } from './recent-tasks.component';

/** Application frame: header, object navigator, content area, Recent Tasks and the global dialogs. */
@Component({
  selector: 'app-shell',
  imports: [
    ClarityModule,
    FormsModule,
    RouterLink,
    RouterOutlet,
    InventoryTreeComponent,
    RecentTasksComponent,
    ConfirmDialogComponent,
    ChangePasswordDialogComponent,
    HostFormDialogComponent,
    NewVmWizardComponent,
    EditVmDialogComponent,
    CloneVmDialogComponent,
    ProtectVmDialogComponent,
    MigrateVmDialogComponent,
    RelocateVmDialogComponent,
    SnapshotDialogComponent,
    IsoDownloadDialogComponent,
    ConsoleWindowComponent,
  ],
  template: `
    <clr-main-container>
      <clr-header class="fc-header">
        <div class="branding">
          <a routerLink="/root" class="nav-link">
            <svg class="fc-logo" viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
              <rect x="2" y="3" width="20" height="5" rx="1.5" />
              <rect x="2" y="9.5" width="14" height="5" rx="1.5" />
              <rect x="2" y="16" width="8" height="5" rx="1.5" />
            </svg>
            <span class="title">Hostwarden</span>
          </a>
        </div>
        <form class="fc-search" (ngSubmit)="search()">
          <label for="fc-search">
            <cds-icon shape="search" size="16"></cds-icon>
            <input id="fc-search" type="text" name="q" placeholder="Search virtual machines" [(ngModel)]="query" />
          </label>
        </form>
        <div class="header-actions">
          <a class="nav-link nav-icon" role="button" tabindex="0" title="Refresh" aria-label="Refresh" (click)="inventory.refresh()" (keydown.enter)="inventory.refresh()">
            <cds-icon shape="refresh" [class.spin]="inventory.loading()"></cds-icon>
          </a>
          <clr-dropdown>
            <button type="button" class="nav-text" clrDropdownTrigger>
              {{ auth.username() }}{{ auth.isAdmin() ? '' : ' (read-only)' }} <cds-icon shape="angle" direction="down" size="12"></cds-icon>
            </button>
            <clr-dropdown-menu *clrIfOpen clrPosition="bottom-right">
              <button type="button" clrDropdownItem (click)="dialogs.changePassword.set(true)">Change Password...</button>
              @if (auth.isAdmin()) {
                <button type="button" clrDropdownItem routerLink="/admin/users">Users and Roles</button>
                <button type="button" clrDropdownItem routerLink="/admin/auth">Authentication</button>
              }
              <div class="dropdown-divider" role="separator"></div>
              <button type="button" clrDropdownItem (click)="tasks.clear()">Clear Recent Tasks</button>
              <div class="dropdown-divider" role="separator"></div>
              <button type="button" clrDropdownItem (click)="api.logout()">Logout</button>
            </clr-dropdown-menu>
          </clr-dropdown>
        </div>
      </clr-header>

      @if (auth.mustChangePassword()) {
        <clr-alert clrAlertType="warning" [clrAlertAppLevel]="true" [clrAlertClosable]="false">
          <clr-alert-item>
            <span class="alert-text">You are signed in with the default password. Anyone who can reach this page can sign in too.</span>
            <div class="alert-actions"><button class="btn alert-action" (click)="dialogs.changePassword.set(true)">Change Password</button></div>
          </clr-alert-item>
        </clr-alert>
      }
      <div class="body">
        <aside><app-inventory-tree /></aside>
        <section>
          <div class="content"><router-outlet /></div>
          <app-recent-tasks />
        </section>
      </div>
    </clr-main-container>

    <app-confirm-dialog />
    @if (dialogs.changePassword()) {
      <app-change-password-dialog />
    }
    @if (dialogs.hostForm(); as f) {
      <app-host-form-dialog [host]="f.host" />
    }
    @if (dialogs.newVm(); as n) {
      <app-new-vm-wizard [hostId]="n.hostId" />
    }
    @if (dialogs.editVm(); as vm) {
      <app-edit-vm-dialog [vm]="vm" />
    }
    @if (dialogs.protectVm(); as p) {
      <app-protect-vm-dialog [vm]="p.vm" [schedule]="p.schedule" [edit]="p.edit" />
    }
    @if (dialogs.cloneVm(); as c) {
      <app-clone-vm-dialog [vm]="c.vm" [template]="c.template" />
    }
    @if (dialogs.migrateVm(); as vm) {
      <app-migrate-vm-dialog [vm]="vm" />
    }
    @if (dialogs.relocateVm(); as vm) {
      <app-relocate-vm-dialog [vm]="vm" />
    }
    @if (dialogs.takeSnapshot(); as vm) {
      <app-snapshot-dialog [vm]="vm" />
    }
    @if (dialogs.isoDownload(); as iso) {
      <app-iso-download-dialog [hostId]="iso.hostId" [datastore]="iso.datastore" />
    }
    @if (dialogs.console()) {
      <!-- Loaded on first use: the console brings the remote display library with it. -->
      @defer {
        <app-console-window />
      }
    }
  `,
  styles: `
    .branding .title {
      font-size: 0.8rem;
      font-weight: 400;
    }
    .branding .fc-logo {
      margin-right: 0.4rem;
      fill: var(--fc-brand);
    }
    .fc-search {
      flex: 0 1 22rem;
      min-width: 8rem;
      margin: 0 auto;
    }
    .fc-search label {
      display: flex;
      align-items: center;
      gap: 0.3rem;
      height: 1.4rem;
      margin-top: 0.5rem;
      padding: 0 0.4rem;
      border-radius: 3px;
      background: rgba(255, 255, 255, 0.12);
      color: #fff;
    }
    .fc-search input {
      flex: 1;
      border: 0;
      outline: 0;
      background: transparent;
      color: #fff;
      font-size: 0.65rem;
    }
    .fc-search input::placeholder {
      color: rgba(255, 255, 255, 0.65);
    }
    .spin {
      animation: fc-spin 1s linear infinite;
    }
    @keyframes fc-spin {
      to {
        transform: rotate(360deg);
      }
    }
    .body {
      display: flex;
      flex: 1 1 auto;
      min-height: 0;
    }
    aside {
      flex: 0 0 var(--fc-nav-width);
      min-width: 0;
      border-right: 1px solid var(--fc-border);
    }
    section {
      display: flex;
      flex-direction: column;
      flex: 1 1 auto;
      min-width: 0;
    }
    .content {
      flex: 1 1 auto;
      min-height: 0;
    }
  `,
})
export class ShellComponent implements OnInit, OnDestroy {
  protected readonly api = inject(ApiService);
  protected readonly auth = inject(AuthService);
  protected readonly dialogs = inject(DialogService);
  protected readonly inventory = inject(InventoryService);
  protected readonly tasks = inject(TaskService);
  private readonly router = inject(Router);

  protected query = '';

  ngOnInit(): void {
    this.inventory.start();
    this.tasks.start();
  }

  ngOnDestroy(): void {
    this.inventory.stop();
    this.tasks.stop();
  }

  protected search(): void {
    void this.router.navigate(['/root', 'vms'], { queryParams: { q: this.query.trim() || null } });
  }
}
