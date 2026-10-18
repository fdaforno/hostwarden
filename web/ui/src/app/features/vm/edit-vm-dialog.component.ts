import { Component, OnInit, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import {
  ActionsService,
  ApiError,
  ApiService,
  BytesPipe,
  Datastore,
  DialogService,
  GuestOs,
  InventoryService,
  IsoService,
  MbPipe,
  Network,
  TaskService,
  UpdateVmRequest,
  UsbController,
  Vm,
  errorMessage,
} from '../../core';

interface DiskEdit {
  key: number;
  label: string;
  currentGb: number;
  sizeGb: number;
  thin: boolean;
  mode: string;
  path: string;
  removed: boolean;
}

interface NicEdit {
  key: number;
  label: string;
  /** Values on the VM, to detect changes. */
  currentNetwork: string;
  currentStartConnected: boolean;
  network: string;
  startConnected: boolean;
  adapterType: string;
  mac: string;
  removed: boolean;
}

interface CdromEdit {
  key: number;
  label: string;
  currentIso: string;
  /** Empty = client device (no media). */
  iso: string;
  removed: boolean;
}

interface NewDisk {
  id: number;
  sizeGb: number;
  thin: boolean;
  /** Empty = same datastore as the virtual machine. */
  datastore: string;
}

interface NewNic {
  id: number;
  network: string;
  adapterType: string;
}

interface NewCdrom {
  id: number;
  iso: string;
}

const POWER_OFF_POLL_MS = 3000;

/** Edit Settings: Virtual Hardware and VM Options. */
@Component({
  selector: 'app-edit-vm-dialog',
  imports: [ClarityModule, FormsModule, MbPipe, BytesPipe],
  template: `
    <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="close()" [clrModalStaticBackdrop]="true" clrModalSize="lg">
      <h3 class="modal-title">Edit Settings <span class="fc-muted">| {{ vm().name }}</span></h3>
      <div class="modal-body">
        @if (error()) {
          <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
            <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
          </clr-alert>
        }
        @if (!current()) {
          <div class="fc-empty"><clr-spinner clrMedium></clr-spinner></div>
        } @else {
          <clr-tabs>
            <clr-tab>
              <button clrTabLink type="button">Virtual Hardware</button>
              <clr-tab-content *clrIfActive="true">
                <div class="add-device">
                  <clr-dropdown>
                    <button type="button" class="btn btn-link btn-sm" clrDropdownTrigger>
                      Add New Device <cds-icon shape="angle" direction="down" size="12"></cds-icon>
                    </button>
                    <clr-dropdown-menu *clrIfOpen clrPosition="bottom-right">
                      <h4 class="dropdown-header">Disks and drives</h4>
                      <button type="button" clrDropdownItem (click)="addDisk()">Hard Disk</button>
                      <button type="button" clrDropdownItem (click)="addCdrom()">CD/DVD Drive</button>
                      <h4 class="dropdown-header">Controllers</h4>
                      <button type="button" clrDropdownItem [clrDisabled]="usb !== ''" (click)="usb = 'usb3'">USB Controller</button>
                      <h4 class="dropdown-header">Network</h4>
                      <button type="button" clrDropdownItem [clrDisabled]="networks().length === 0" (click)="addNic()">Network Adapter</button>
                    </clr-dropdown-menu>
                  </clr-dropdown>
                </div>

                <clr-stack-view>
                  <clr-stack-block [clrSbExpandable]="true" [clrSbExpanded]="true">
                    <clr-stack-label>CPU</clr-stack-label>
                    <clr-stack-content>
                      <input class="clr-input num" type="number" name="cpu" min="1" max="128" [(ngModel)]="cpu" />
                    </clr-stack-content>
                    <clr-stack-block>
                      <clr-stack-label>Cores per Socket</clr-stack-label>
                      <clr-stack-content>
                        <input class="clr-input num" type="number" name="coresPerSocket" min="1" [max]="cpu" [(ngModel)]="coresPerSocket" />
                        <span class="fc-muted">Sockets: {{ sockets() }}</span>
                      </clr-stack-content>
                    </clr-stack-block>
                    <clr-stack-block>
                      <clr-stack-label>CPU Hot Plug</clr-stack-label>
                      <clr-stack-content>
                        <label><input type="checkbox" name="cpuHotAdd" [(ngModel)]="cpuHotAdd" /> Enable CPU Hot Add</label>
                      </clr-stack-content>
                    </clr-stack-block>
                    <clr-stack-block>
                      <clr-stack-label>Hardware virtualization</clr-stack-label>
                      <clr-stack-content>
                        <label><input type="checkbox" name="nestedHv" [(ngModel)]="nestedHv" /> Expose hardware assisted virtualization to the guest OS</label>
                      </clr-stack-content>
                    </clr-stack-block>
                  </clr-stack-block>

                  <clr-stack-block [clrSbExpandable]="true" [clrSbExpanded]="true">
                    <clr-stack-label>Memory</clr-stack-label>
                    <clr-stack-content>
                      <input class="clr-input num" type="number" name="memoryMb" min="64" step="64" [(ngModel)]="memoryMb" /> MB
                      <span class="fc-muted">({{ memoryMb | mb }})</span>
                    </clr-stack-content>
                    <clr-stack-block>
                      <clr-stack-label>Memory Hot Plug</clr-stack-label>
                      <clr-stack-content>
                        <label><input type="checkbox" name="memoryHotAdd" [(ngModel)]="memoryHotAdd" /> Enable</label>
                      </clr-stack-content>
                    </clr-stack-block>
                  </clr-stack-block>

                  @for (d of disks; track d.key) {
                    <clr-stack-block [clrSbExpandable]="!d.removed">
                      <clr-stack-label>{{ d.label }}</clr-stack-label>
                      <clr-stack-content>
                        @if (d.removed) {
                          <span class="removed">Device will be removed</span>
                          <label class="inline"><input type="checkbox" name="deleteDiskFiles" [(ngModel)]="deleteDiskFiles" /> Delete files from datastore</label>
                          <button type="button" class="btn btn-link btn-sm row-btn" (click)="d.removed = false">Restore</button>
                        } @else {
                          <input class="clr-input num" type="number" [name]="'disk' + d.key" [min]="d.currentGb" max="65536" [(ngModel)]="d.sizeGb" /> GB
                          <button type="button" class="btn btn-link btn-icon btn-sm row-btn" title="Remove device" aria-label="Remove device" (click)="d.removed = true">
                            <cds-icon shape="times-circle"></cds-icon>
                          </button>
                        }
                      </clr-stack-content>
                      <clr-stack-block>
                        <clr-stack-label>Disk Provisioning</clr-stack-label>
                        <clr-stack-content>{{ d.thin ? 'Thin Provision' : 'Thick Provision' }}</clr-stack-content>
                      </clr-stack-block>
                      <clr-stack-block>
                        <clr-stack-label>Disk Mode</clr-stack-label>
                        <clr-stack-content>{{ d.mode || 'persistent' }}</clr-stack-content>
                      </clr-stack-block>
                      <clr-stack-block>
                        <clr-stack-label>Disk File</clr-stack-label>
                        <clr-stack-content>{{ d.path }}</clr-stack-content>
                      </clr-stack-block>
                    </clr-stack-block>
                  }

                  @for (d of newDisks; track d.id) {
                    <clr-stack-block [clrSbExpandable]="true" [clrSbExpanded]="true">
                      <clr-stack-label>New Hard disk *</clr-stack-label>
                      <clr-stack-content>
                        <input class="clr-input num" type="number" [name]="'newDisk' + d.id" min="1" max="65536" [(ngModel)]="d.sizeGb" /> GB
                        <button type="button" class="btn btn-link btn-icon btn-sm row-btn" title="Remove device" aria-label="Remove device" (click)="dropNewDisk(d)">
                          <cds-icon shape="times-circle"></cds-icon>
                        </button>
                      </clr-stack-content>
                      <clr-stack-block>
                        <clr-stack-label>Location</clr-stack-label>
                        <clr-stack-content>
                          <div class="clr-select-wrapper">
                            <select class="clr-select" [name]="'newDiskDs' + d.id" [(ngModel)]="d.datastore">
                              <option value="">Store with the virtual machine</option>
                              @for (ds of datastores(); track ds.name) {
                                <option [value]="ds.name">{{ ds.name }} ({{ ds.free_bytes | bytes }} free)</option>
                              }
                            </select>
                          </div>
                        </clr-stack-content>
                      </clr-stack-block>
                      <clr-stack-block>
                        <clr-stack-label>Disk Provisioning</clr-stack-label>
                        <clr-stack-content>
                          <div class="clr-select-wrapper">
                            <select class="clr-select" [name]="'newDiskThin' + d.id" [(ngModel)]="d.thin">
                              <option [ngValue]="true">Thin Provision</option>
                              <option [ngValue]="false">Thick Provision</option>
                            </select>
                          </div>
                        </clr-stack-content>
                      </clr-stack-block>
                    </clr-stack-block>
                  }

                  @for (n of nics; track n.key) {
                    <clr-stack-block [clrSbExpandable]="!n.removed">
                      <clr-stack-label>{{ n.label }}</clr-stack-label>
                      <clr-stack-content>
                        @if (n.removed) {
                          <span class="removed">Device will be removed</span>
                          <button type="button" class="btn btn-link btn-sm row-btn" (click)="n.removed = false">Restore</button>
                        } @else {
                          <div class="clr-select-wrapper">
                            <select class="clr-select" [name]="'nicNet' + n.key" [(ngModel)]="n.network">
                              @for (net of networkChoices(n.currentNetwork); track net) {
                                <option [value]="net">{{ net }}</option>
                              }
                            </select>
                          </div>
                          <label class="inline"><input type="checkbox" [name]="'nicConn' + n.key" [(ngModel)]="n.startConnected" /> Connect At Power On</label>
                          <button type="button" class="btn btn-link btn-icon btn-sm row-btn" title="Remove device" aria-label="Remove device" (click)="n.removed = true">
                            <cds-icon shape="times-circle"></cds-icon>
                          </button>
                        }
                      </clr-stack-content>
                      <clr-stack-block>
                        <clr-stack-label>Adapter Type</clr-stack-label>
                        <clr-stack-content>{{ n.adapterType }}</clr-stack-content>
                      </clr-stack-block>
                      <clr-stack-block>
                        <clr-stack-label>MAC Address</clr-stack-label>
                        <clr-stack-content>{{ n.mac }}</clr-stack-content>
                      </clr-stack-block>
                    </clr-stack-block>
                  }

                  @for (n of newNics; track n.id) {
                    <clr-stack-block [clrSbExpandable]="true" [clrSbExpanded]="true">
                      <clr-stack-label>New Network *</clr-stack-label>
                      <clr-stack-content>
                        <div class="clr-select-wrapper">
                          <select class="clr-select" [name]="'newNicNet' + n.id" [(ngModel)]="n.network">
                            @for (net of networks(); track net.name) {
                              <option [value]="net.name">{{ net.name }}</option>
                            }
                          </select>
                        </div>
                        <button type="button" class="btn btn-link btn-icon btn-sm row-btn" title="Remove device" aria-label="Remove device" (click)="dropNewNic(n)">
                          <cds-icon shape="times-circle"></cds-icon>
                        </button>
                      </clr-stack-content>
                      <clr-stack-block>
                        <clr-stack-label>Adapter Type</clr-stack-label>
                        <clr-stack-content>
                          <div class="clr-select-wrapper">
                            <select class="clr-select" [name]="'newNicType' + n.id" [(ngModel)]="n.adapterType">
                              <option value="vmxnet3">VMXNET 3</option>
                              <option value="e1000e">E1000E</option>
                              <option value="e1000">E1000</option>
                            </select>
                          </div>
                        </clr-stack-content>
                      </clr-stack-block>
                    </clr-stack-block>
                  }

                  @for (c of cdroms; track c.key) {
                    <clr-stack-block>
                      <clr-stack-label>{{ c.label }}</clr-stack-label>
                      <clr-stack-content>
                        @if (c.removed) {
                          <span class="removed">Device will be removed</span>
                          <button type="button" class="btn btn-link btn-sm row-btn" (click)="c.removed = false">Restore</button>
                        } @else {
                          <div class="clr-select-wrapper">
                            <select class="clr-select iso" [name]="'cd' + c.key" [(ngModel)]="c.iso">
                              <option value="">Client Device (no media)</option>
                              @for (i of isoChoices(c.currentIso); track i) {
                                <option [value]="i">{{ i }}</option>
                              }
                            </select>
                          </div>
                          <button type="button" class="btn btn-link btn-icon btn-sm row-btn" title="Remove device" aria-label="Remove device" (click)="c.removed = true">
                            <cds-icon shape="times-circle"></cds-icon>
                          </button>
                        }
                      </clr-stack-content>
                    </clr-stack-block>
                  }

                  @for (c of newCdroms; track c.id) {
                    <clr-stack-block>
                      <clr-stack-label>New CD/DVD Drive *</clr-stack-label>
                      <clr-stack-content>
                        <div class="clr-select-wrapper">
                          <select class="clr-select iso" [name]="'newCd' + c.id" [(ngModel)]="c.iso">
                            <option value="">Client Device (no media)</option>
                            @for (i of isos(); track i) {
                              <option [value]="i">{{ i }}</option>
                            }
                          </select>
                        </div>
                        <button type="button" class="btn btn-link btn-icon btn-sm row-btn" title="Remove device" aria-label="Remove device" (click)="dropNewCdrom(c)">
                          <cds-icon shape="times-circle"></cds-icon>
                        </button>
                      </clr-stack-content>
                    </clr-stack-block>
                  }

                  @if (usb !== '' || currentUsb !== '') {
                    <clr-stack-block>
                      <clr-stack-label>{{ currentUsb === '' ? 'New USB Controller *' : 'USB controller' }}</clr-stack-label>
                      <clr-stack-content>
                        @if (usb === '') {
                          <span class="removed">Device will be removed</span>
                          <button type="button" class="btn btn-link btn-sm row-btn" (click)="usb = currentUsb">Restore</button>
                        } @else {
                          <div class="clr-select-wrapper">
                            <select class="clr-select" name="usb" [(ngModel)]="usb">
                              <option value="usb3">USB 3.1</option>
                              <option value="usb2">USB 2.0</option>
                            </select>
                          </div>
                          <button type="button" class="btn btn-link btn-icon btn-sm row-btn" title="Remove device" aria-label="Remove device" (click)="usb = ''">
                            <cds-icon shape="times-circle"></cds-icon>
                          </button>
                        }
                      </clr-stack-content>
                    </clr-stack-block>
                  }
                </clr-stack-view>
                <p class="fc-muted">Changes require the virtual machine to be powered off. Disks can only grow, and not while snapshots exist.</p>
              </clr-tab-content>
            </clr-tab>

            <clr-tab>
              <button clrTabLink type="button">VM Options</button>
              <clr-tab-content *clrIfActive>
                <clr-stack-view>
                  <clr-stack-block [clrSbExpandable]="true" [clrSbExpanded]="true">
                    <clr-stack-label>General Options</clr-stack-label>
                    <clr-stack-content>VM Name: {{ name }}</clr-stack-content>
                    <clr-stack-block>
                      <clr-stack-label>VM Name</clr-stack-label>
                      <clr-stack-content>
                        <input class="clr-input wide" type="text" name="name" maxlength="80" [(ngModel)]="name" />
                      </clr-stack-content>
                    </clr-stack-block>
                    <clr-stack-block>
                      <clr-stack-label>Guest OS Version</clr-stack-label>
                      <clr-stack-content>
                        <div class="clr-select-wrapper">
                          <select class="clr-select" name="guestId" [(ngModel)]="guestId">
                            @for (g of guests(); track g.id) {
                              <option [value]="g.id">{{ g.name }}</option>
                            }
                          </select>
                        </div>
                      </clr-stack-content>
                    </clr-stack-block>
                    <clr-stack-block>
                      <clr-stack-label>VM Config File</clr-stack-label>
                      <clr-stack-content>{{ current()!.vmx_path }}</clr-stack-content>
                    </clr-stack-block>
                    <clr-stack-block>
                      <clr-stack-label>Compatibility</clr-stack-label>
                      <clr-stack-content>{{ current()!.hardware_version }}</clr-stack-content>
                    </clr-stack-block>
                    <clr-stack-block>
                      <clr-stack-label>UUID</clr-stack-label>
                      <clr-stack-content>{{ current()!.uuid }}</clr-stack-content>
                    </clr-stack-block>
                  </clr-stack-block>

                  <clr-stack-block [clrSbExpandable]="true" [clrSbExpanded]="true">
                    <clr-stack-label>Boot Options</clr-stack-label>
                    <clr-stack-content>{{ firmware === 'efi' ? 'EFI' : 'BIOS' }}</clr-stack-content>
                    <clr-stack-block>
                      <clr-stack-label>Firmware</clr-stack-label>
                      <clr-stack-content>
                        <div class="clr-select-wrapper">
                          <select class="clr-select" name="firmware" [(ngModel)]="firmware">
                            <option value="bios">BIOS</option>
                            <option value="efi">EFI</option>
                          </select>
                        </div>
                        @if (firmware !== currentFirmware) {
                          <span class="removed">An operating system installed with the other firmware will probably not boot after this change.</span>
                        }
                      </clr-stack-content>
                    </clr-stack-block>
                    <clr-stack-block>
                      <clr-stack-label>Boot Delay</clr-stack-label>
                      <clr-stack-content>
                        <input class="clr-input num" type="number" name="bootDelayMs" min="0" max="10000" step="500" [(ngModel)]="bootDelayMs" /> milliseconds
                      </clr-stack-content>
                    </clr-stack-block>
                    <clr-stack-block>
                      <clr-stack-label>Force {{ firmware === 'efi' ? 'EFI' : 'BIOS' }} setup</clr-stack-label>
                      <clr-stack-content>
                        <label><input type="checkbox" name="enterBios" [(ngModel)]="enterBios" /> During the next boot, force entry into the setup screen</label>
                      </clr-stack-content>
                    </clr-stack-block>
                  </clr-stack-block>

                  <clr-stack-block>
                    <clr-stack-label>Notes</clr-stack-label>
                    <clr-stack-content>
                      <textarea class="clr-textarea wide" name="notes" rows="4" [(ngModel)]="notes"></textarea>
                    </clr-stack-content>
                  </clr-stack-block>
                </clr-stack-view>
              </clr-tab-content>
            </clr-tab>
          </clr-tabs>
        }
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" (click)="close()">Cancel</button>
        <button type="button" class="btn btn-primary" [clrLoading]="busy()" [disabled]="!current() || !name.trim()" (click)="save()">OK</button>
      </div>
    </clr-modal>
  `,
  styles: `
    .num {
      width: 5rem;
    }
    .wide {
      width: 100%;
    }
    .iso {
      max-width: 22rem;
    }
    .inline {
      margin-left: 0.6rem;
    }
    clr-stack-view {
      margin-top: 0.6rem;
    }
    .add-device {
      display: flex;
      justify-content: flex-end;
      margin-top: 0.3rem;
    }
    .add-device .btn {
      margin: 0;
    }
    .add-device + clr-stack-view {
      margin-top: 0;
    }
    .row-btn {
      margin: 0 0 0 0.4rem;
      min-width: 0;
      padding: 0 0.2rem;
      vertical-align: middle;
    }
    .removed {
      color: #c21d00;
    }
  `,
})
export class EditVmDialogComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly actions = inject(ActionsService);
  private readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);
  private readonly isoService = inject(IsoService);
  private readonly tasks = inject(TaskService);

  readonly vm = input.required<Vm>();

  /** Fresh copy of the VM; the list row passed in may be stale. */
  protected readonly current = signal<Vm | null>(null);
  /** Live from the inventory, which may still be loading when the dialog opens. */
  protected readonly networks = computed<Network[]>(() => this.inventory.networksByHost()[this.vm().host_id] ?? []);
  protected readonly datastores = computed<Datastore[]>(() => (this.inventory.datastoresByHost()[this.vm().host_id] ?? []).filter((d) => d.accessible));
  protected readonly guests = signal<GuestOs[]>([]);
  protected readonly isos = signal<string[]>([]);
  protected readonly error = signal('');
  protected readonly busy = signal(false);

  // Virtual hardware
  protected cpu = 1;
  protected coresPerSocket = 1;
  protected cpuHotAdd = false;
  protected nestedHv = false;
  protected memoryMb = 1024;
  protected memoryHotAdd = false;
  protected disks: DiskEdit[] = [];
  protected nics: NicEdit[] = [];
  protected cdroms: CdromEdit[] = [];
  protected newDisks: NewDisk[] = [];
  protected newNics: NewNic[] = [];
  protected newCdroms: NewCdrom[] = [];
  protected deleteDiskFiles = false;
  protected currentUsb: UsbController = '';
  protected usb: UsbController = '';
  // VM options
  protected name = '';
  protected guestId = '';
  protected currentFirmware = 'bios';
  protected firmware = 'bios';
  protected bootDelayMs = 0;
  protected enterBios = false;
  protected notes = '';

  private nextId = 1;

  async ngOnInit(): Promise<void> {
    const { host_id, id } = this.vm();
    try {
      const vm = { ...(await this.api.vm(host_id, id)), host_id };
      this.fill(vm);
      this.current.set(vm);
    } catch (e) {
      this.error.set(errorMessage(e));
      return;
    }
    // Choice lists are optional: without them the current values stay selectable.
    void this.api.guestOs(host_id).then(
      (list) => this.guests.set(this.withCurrentGuest(list)),
      () => undefined,
    );
    void this.isoService.list(host_id, this.datastores()).then(
      (list) => this.isos.set(list),
      () => undefined,
    );
  }

  private fill(vm: Vm): void {
    this.cpu = vm.num_cpus;
    this.coresPerSocket = vm.cores_per_socket || 1;
    this.cpuHotAdd = !!vm.cpu_hot_add;
    this.nestedHv = !!vm.nested_hv;
    this.memoryMb = vm.memory_mb;
    this.memoryHotAdd = !!vm.memory_hot_add;
    this.currentUsb = this.usb = vm.usb_controller ?? '';
    this.name = vm.name;
    this.guestId = vm.guest_id;
    this.currentFirmware = this.firmware = vm.firmware || 'bios';
    this.bootDelayMs = vm.boot_delay_ms ?? 0;
    this.notes = vm.annotation ?? '';
    this.guests.set(this.withCurrentGuest([]));
    this.disks = (vm.disks ?? []).map((d) => ({
      key: d.key,
      label: d.label || `Hard disk ${d.key}`,
      currentGb: d.capacity_gb,
      sizeGb: d.capacity_gb,
      thin: d.thin,
      mode: d.mode,
      path: d.path,
      removed: false,
    }));
    this.nics = (vm.nics ?? []).map((n) => ({
      key: n.key,
      label: n.label || `Network adapter ${n.key}`,
      currentNetwork: n.network,
      currentStartConnected: n.start_connected,
      network: n.network,
      startConnected: n.start_connected,
      adapterType: n.adapter_type,
      mac: n.mac_address,
      removed: false,
    }));
    this.cdroms = (vm.cdroms ?? []).map((c) => ({
      key: c.key,
      label: c.label || `CD/DVD drive ${c.key}`,
      currentIso: c.iso,
      iso: c.iso,
      removed: false,
    }));
  }

  /** 64-bit guests sorted by name, always including the VM's current one. */
  private withCurrentGuest(list: GuestOs[]): GuestOs[] {
    const out = list.filter((g) => /64/.test(g.id) || g.id === this.guestId).sort((a, b) => a.name.localeCompare(b.name));
    if (this.guestId && !out.some((g) => g.id === this.guestId)) {
      out.unshift({ id: this.guestId, name: this.current()?.guest_full_name || this.vm().guest_full_name || this.guestId });
    }
    return out;
  }

  protected sockets(): number {
    return Math.max(1, Math.floor(Number(this.cpu) / Math.max(1, Number(this.coresPerSocket))));
  }

  /** Known ISO images plus the one currently mounted, which may live outside the ISO folders. */
  protected isoChoices(currentIso: string): string[] {
    const list = this.isos();
    return currentIso && !list.includes(currentIso) ? [currentIso, ...list] : list;
  }

  /** Port groups of the host plus the adapter's current one (e.g. a network that no longer exists). */
  protected networkChoices(currentNetwork: string): string[] {
    const names = this.networks().map((n) => n.name);
    return currentNetwork && !names.includes(currentNetwork) ? [currentNetwork, ...names] : names;
  }

  protected close(): void {
    this.dialogs.editVm.set(null);
  }

  protected addDisk(): void {
    this.newDisks.push({ id: this.nextId++, sizeGb: 16, thin: true, datastore: '' });
  }

  protected dropNewDisk(d: NewDisk): void {
    this.newDisks = this.newDisks.filter((x) => x !== d);
  }

  protected addNic(): void {
    const nets = this.networks();
    if (nets.length === 0) return;
    const network = (nets.find((n) => n.name === 'VM Network') ?? nets[0]).name;
    this.newNics.push({ id: this.nextId++, network, adapterType: 'vmxnet3' });
  }

  protected dropNewNic(n: NewNic): void {
    this.newNics = this.newNics.filter((x) => x !== n);
  }

  protected addCdrom(): void {
    this.newCdroms.push({ id: this.nextId++, iso: '' });
  }

  protected dropNewCdrom(c: NewCdrom): void {
    this.newCdroms = this.newCdroms.filter((x) => x !== c);
  }

  protected async save(): Promise<void> {
    const vm = this.current();
    if (!vm || this.busy()) return;
    const req = this.buildRequest(vm);
    this.error.set('');
    this.busy.set(true);
    try {
      let poweredOffByUs = false;
      try {
        await this.reconfigure(vm, req);
      } catch (e) {
        // 409 also covers other invalid states (e.g. snapshots present), so only offer this for a running VM.
        const canPowerOff = e instanceof ApiError && e.status === 409 && vm.power_state !== 'poweredOff';
        if (!canPowerOff || !(await this.confirmPowerOff(vm, errorMessage(e)))) throw e;
        await this.powerOffAndWait(vm);
        poweredOffByUs = true;
        await this.reconfigure(vm, req);
      }
      this.close();
      if (
        poweredOffByUs &&
        (await this.dialogs.confirm({
          title: 'Power On',
          message: `${vm.name} was powered off to apply the changes. Power it on now?`,
          okLabel: 'Power On',
        }))
      ) {
        await this.actions.vmPower(vm, 'on');
      }
    } catch (e) {
      this.error.set(errorMessage(e));
    } finally {
      this.busy.set(false);
      void this.inventory.refresh();
    }
  }

  /** Only changed fields are sent, so untouched settings are never rewritten. */
  private buildRequest(vm: Vm): UpdateVmRequest {
    const req: UpdateVmRequest = {
      resize_disks: [],
      add_disks: [],
      remove_disk_keys: this.disks.filter((d) => d.removed).map((d) => d.key),
      delete_disk_files: this.deleteDiskFiles,
      add_nics: [],
      update_nics: [],
      remove_nic_keys: this.nics.filter((n) => n.removed).map((n) => n.key),
      add_cdroms: this.newCdroms.map((c) => ({ iso_path: c.iso })),
      update_cdroms: this.cdroms.filter((c) => !c.removed && c.iso !== c.currentIso).map((c) => ({ key: c.key, iso_path: c.iso })),
      remove_cdrom_keys: this.cdroms.filter((c) => c.removed).map((c) => c.key),
    };

    const cpu = Number(this.cpu);
    const cores = Number(this.coresPerSocket);
    const memory = Number(this.memoryMb);
    if (cpu > 0 && cpu !== vm.num_cpus) req.num_cpus = cpu;
    if (cores > 0 && cores !== (vm.cores_per_socket || 1)) req.cores_per_socket = cores;
    if (memory > 0 && memory !== vm.memory_mb) req.memory_mb = memory;
    if (this.cpuHotAdd !== !!vm.cpu_hot_add) req.cpu_hot_add = this.cpuHotAdd;
    if (this.memoryHotAdd !== !!vm.memory_hot_add) req.memory_hot_add = this.memoryHotAdd;
    if (this.nestedHv !== !!vm.nested_hv) req.nested_hv = this.nestedHv;
    if (this.usb !== this.currentUsb) req.usb_controller = this.usb;

    if (this.name.trim() !== vm.name) req.name = this.name.trim();
    if (this.guestId !== vm.guest_id) req.guest_id = this.guestId;
    if (this.firmware !== this.currentFirmware) req.firmware = this.firmware;
    if (Number(this.bootDelayMs) !== (vm.boot_delay_ms ?? 0)) req.boot_delay_ms = Number(this.bootDelayMs);
    if (this.enterBios) req.enter_bios_setup = true;
    if (this.notes !== (vm.annotation ?? '')) req.annotation = this.notes;

    for (const d of this.disks) {
      if (!d.removed && Number(d.sizeGb) > d.currentGb) req.resize_disks.push({ key: d.key, size_gb: Number(d.sizeGb) });
    }
    for (const d of this.newDisks) {
      if (Number(d.sizeGb) > 0) req.add_disks.push({ size_gb: Number(d.sizeGb), thin: d.thin, ...(d.datastore ? { datastore: d.datastore } : {}) });
    }
    for (const n of this.nics) {
      if (n.removed) continue;
      const networkChanged = n.network !== n.currentNetwork;
      const connectChanged = n.startConnected !== n.currentStartConnected;
      if (networkChanged || connectChanged) {
        req.update_nics.push({
          key: n.key,
          ...(networkChanged ? { network: n.network } : {}),
          ...(connectChanged ? { start_connected: n.startConnected } : {}),
        });
      }
    }
    for (const n of this.newNics) {
      if (n.network) req.add_nics.push({ network: n.network, adapter_type: n.adapterType });
    }
    return req;
  }

  private reconfigure(vm: Vm, req: UpdateVmRequest): Promise<Vm> {
    return this.tasks.run('Reconfigure virtual machine', vm.name, () => this.api.updateVm(vm.host_id, vm.id, req));
  }

  private confirmPowerOff(vm: Vm, reason: string): Promise<boolean> {
    return this.dialogs.confirm({
      title: 'Virtual machine is powered on',
      message: `${reason}. Shut down ${vm.name} (guest shutdown, hard power off as fallback), apply the changes and continue?`,
      okLabel: 'Shut Down and Apply',
      danger: true,
    });
  }

  private async powerOffAndWait(vm: Vm): Promise<void> {
    await this.tasks.run('Power Off virtual machine', vm.name, async () => {
      try {
        await this.api.powerVm(vm.host_id, vm.id, 'shutdown_guest');
        await this.waitPoweredOff(vm, 120);
      } catch {
        await this.api.powerVm(vm.host_id, vm.id, 'off');
        await this.waitPoweredOff(vm, 60);
      }
    });
  }

  private async waitPoweredOff(vm: Vm, maxSeconds: number): Promise<void> {
    for (let waited = 0; waited < maxSeconds * 1000; waited += POWER_OFF_POLL_MS) {
      await new Promise((r) => setTimeout(r, POWER_OFF_POLL_MS));
      const state = await this.api.vm(vm.host_id, vm.id).then(
        (v) => v.power_state,
        () => null,
      );
      if (state === 'poweredOff') return;
    }
    throw new Error('Timed out waiting for the virtual machine to power off');
  }
}
