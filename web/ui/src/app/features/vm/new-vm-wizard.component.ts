import { Component, OnInit, ViewChild, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { ClarityModule, ClrWizard } from '@clr/angular';

import {
  ApiService,
  BytesPipe,
  CloudImageCatalogEntry,
  CloudInitConfig,
  Datastore,
  DialogService,
  FileInfo,
  GuestOs,
  InventoryService,
  IsoService,
  MbPipe,
  Network,
  TaskService,
  errorMessage,
} from '../../core';

type CreationType = 'new' | 'cloud';

/** Folder (on each datastore) holding the cloud image disks VMs are cloned from. */
const CLOUD_IMAGE_FOLDER = 'cloud-images';
const DOWNLOAD_POLL_MS = 5000;

/** Guest OS suggested by the name of a cloud image. */
const GUEST_BY_IMAGE: [RegExp, string][] = [
  [/rocky/i, 'rockylinux_64Guest'],
  [/ubuntu/i, 'ubuntu64Guest'],
  [/debian/i, 'debian12_64Guest'],
  [/fedora/i, 'fedora64Guest'],
  [/alma/i, 'almalinux_64Guest'],
];

/** New Virtual Machine wizard: an empty VM, or one deployed from a cloud image with cloud-init. */
@Component({
  selector: 'app-new-vm-wizard',
  imports: [ClarityModule, FormsModule, BytesPipe, MbPipe],
  template: `
    <clr-wizard #wizard [clrWizardOpen]="true" (clrWizardOpenChange)="close()" clrWizardSize="xl" [clrWizardClosable]="!busy()">
      <clr-wizard-title>New Virtual Machine</clr-wizard-title>

      <clr-wizard-button type="cancel">Cancel</clr-wizard-button>
      <clr-wizard-button type="previous">Back</clr-wizard-button>
      <clr-wizard-button type="next">Next</clr-wizard-button>
      <clr-wizard-button type="finish">Finish</clr-wizard-button>

      <clr-wizard-page>
        <ng-template clrPageTitle>Creation type</ng-template>
        <p>How would you like to create a virtual machine?</p>
        <clr-radio-container>
          <clr-radio-wrapper>
            <input type="radio" clrRadio name="creationType" value="new" [(ngModel)]="creationType" />
            <label>Create a new virtual machine</label>
          </clr-radio-wrapper>
          <clr-radio-wrapper>
            <input type="radio" clrRadio name="creationType" value="cloud" [(ngModel)]="creationType" />
            <label>Deploy from a cloud image</label>
          </clr-radio-wrapper>
        </clr-radio-container>
        <p class="fc-muted">
          @if (creationType === 'new') {
            An empty virtual machine: you choose the hardware and install the guest operating system yourself, for example from an ISO image.
          } @else {
            The boot disk is cloned from a ready-made cloud image and the guest is configured on first boot with cloud-init (host name, user, SSH keys, packages).
          }
        </p>
      </clr-wizard-page>

      <clr-wizard-page [clrWizardPageNextDisabled]="!name.trim() || !host()">
        <ng-template clrPageTitle>Name and host</ng-template>
        <form clrForm clrLayout="horizontal">
          <clr-input-container>
            <label>Virtual machine name</label>
            <input clrInput name="name" [(ngModel)]="name" size="40" placeholder="web-01" />
          </clr-input-container>
        </form>
        <p>Choose the host that will run the virtual machine</p>
        <table class="table table-compact">
          <thead>
            <tr>
              <th></th>
              <th class="left">Name</th>
              <th class="left">State</th>
              <th class="left">Version</th>
            </tr>
          </thead>
          <tbody>
            @for (h of hosts(); track h.id) {
              <tr (click)="selectHost(h.id)">
                <td><input type="radio" name="host" [checked]="host() === h.id" /></td>
                <td class="left">{{ h.name }}</td>
                <td class="left">Connected</td>
                <td class="left">{{ h.status?.product }} {{ h.status?.version }}</td>
              </tr>
            }
          </tbody>
        </table>
        @if (loadError()) {
          <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
            <clr-alert-item><span class="alert-text">{{ loadError() }}</span></clr-alert-item>
          </clr-alert>
        }
      </clr-wizard-page>

      <clr-wizard-page [clrWizardPageNextDisabled]="!datastore()">
        <ng-template clrPageTitle>Storage</ng-template>
        <p>Choose the datastore that will hold the virtual machine</p>
        <table class="table table-compact">
          <thead>
            <tr>
              <th></th>
              <th class="left">Name</th>
              <th class="left">Capacity</th>
              <th class="left">Free</th>
              <th class="left">Type</th>
            </tr>
          </thead>
          <tbody>
            @for (d of stores(); track d.name) {
              <tr (click)="selectDatastore(d.name)">
                <td><input type="radio" name="datastore" [checked]="datastore() === d.name" /></td>
                <td class="left">{{ d.name }}</td>
                <td class="left">{{ d.capacity_bytes | bytes }}</td>
                <td class="left">{{ d.free_bytes | bytes }}</td>
                <td class="left">{{ d.type }}</td>
              </tr>
            }
          </tbody>
        </table>
      </clr-wizard-page>

      @if (creationType === 'cloud') {
        <clr-wizard-page [clrWizardPageNextDisabled]="!cloudImage">
          <ng-template clrPageTitle>Cloud image</ng-template>
          <p>Images available in the {{ imageFolder }} folder of {{ datastore() }}</p>
          <table class="table table-compact">
            <thead>
              <tr>
                <th></th>
                <th class="left">Image</th>
                <th class="left">Size</th>
              </tr>
            </thead>
            <tbody>
              @for (img of images(); track img.name) {
                <tr (click)="selectImage(img.name)">
                  <td><input type="radio" name="cloudImage" [checked]="cloudImage === imageFolder + '/' + img.name" /></td>
                  <td class="left">{{ img.name }}</td>
                  <td class="left">{{ img.size | bytes }}</td>
                </tr>
              } @empty {
                <tr>
                  <td colspan="3" class="fc-muted">No cloud image on this datastore yet: download one below.</td>
                </tr>
              }
            </tbody>
          </table>

          <h5>Download a cloud image to {{ datastore() }}</h5>
          <table class="table table-compact">
            <tbody>
              @for (c of catalog(); track c.url) {
                <tr>
                  <td class="left">{{ c.distro }} {{ c.ver }}</td>
                  <td class="left">{{ c.variant }}</td>
                  <td class="left">{{ c.size }}</td>
                  <td>
                    <button type="button" class="btn btn-link btn-sm" [disabled]="downloading().has(c.url)" (click)="downloadImage(c)">
                      {{ downloading().has(c.url) ? 'Downloading...' : 'Download' }}
                    </button>
                  </td>
                </tr>
              }
            </tbody>
          </table>
          <p class="fc-muted">The image is downloaded and converted by the Hostwarden server, then uploaded to the datastore; progress is in Recent Tasks and the list above refreshes when it is done.</p>
        </clr-wizard-page>
      }

      <clr-wizard-page [clrWizardPageNextDisabled]="!guestId">
        <ng-template clrPageTitle>Guest OS</ng-template>
        <p>
          {{ creationType === 'cloud' ? 'Confirm the guest OS of the selected image' : 'Choose the guest OS that will be installed on the virtual machine' }}
        </p>
        <form clrForm clrLayout="horizontal">
          <clr-select-container>
            <label>Guest OS Version</label>
            <select clrSelect name="guestId" [(ngModel)]="guestId">
              @for (g of guests(); track g.id) {
                <option [value]="g.id">{{ g.name }}</option>
              }
            </select>
          </clr-select-container>
        </form>
      </clr-wizard-page>

      @if (creationType === 'cloud') {
        <clr-wizard-page [clrWizardPageNextDisabled]="!cloudInitValid()">
          <ng-template clrPageTitle>Guest setup</ng-template>
          <p>Applied by cloud-init on the first boot</p>
          <form clrForm clrLayout="horizontal">
            <clr-input-container>
              <label>Host name</label>
              <input clrInput name="ciHostname" [(ngModel)]="ciHostname" [placeholder]="name" />
            </clr-input-container>
            <clr-input-container>
              <label>User name</label>
              <input clrInput name="ciUser" [(ngModel)]="ciUser" autocomplete="off" />
              <clr-control-helper>Created with passwordless sudo</clr-control-helper>
            </clr-input-container>
            <clr-password-container>
              <label>Password</label>
              <input clrPassword name="ciPassword" [(ngModel)]="ciPassword" autocomplete="new-password" />
              <clr-control-helper>Optional when an SSH key is given</clr-control-helper>
            </clr-password-container>
            <clr-textarea-container>
              <label>SSH public keys</label>
              <textarea clrTextarea name="ciKeys" rows="3" cols="60" placeholder="ssh-ed25519 AAAA... one key per line" [(ngModel)]="ciKeys"></textarea>
            </clr-textarea-container>
            <clr-input-container>
              <label>Packages</label>
              <input clrInput name="ciPackages" [(ngModel)]="ciPackages" size="50" placeholder="qemu-guest-agent, vim" />
              <clr-control-helper>Comma separated, installed on first boot</clr-control-helper>
            </clr-input-container>
            <clr-textarea-container>
              <label>Custom user-data</label>
              <textarea clrTextarea name="ciUserData" rows="4" cols="60" placeholder="#cloud-config ..." [(ngModel)]="ciUserData"></textarea>
              <clr-control-helper>Optional: when filled in, it replaces all the fields above</clr-control-helper>
            </clr-textarea-container>
          </form>
        </clr-wizard-page>
      }

      <clr-wizard-page [clrWizardPageNextDisabled]="!(cpu > 0 && memoryMb > 0 && diskGb > 0)">
        <ng-template clrPageTitle>Hardware</ng-template>
        <p>Set the CPU, memory, disks and devices of the virtual machine</p>
        <clr-stack-view>
          <clr-stack-block>
            <clr-stack-label>CPU</clr-stack-label>
            <clr-stack-content><input class="clr-input num" type="number" name="cpu" min="1" max="128" [(ngModel)]="cpu" /></clr-stack-content>
          </clr-stack-block>
          <clr-stack-block>
            <clr-stack-label>Memory</clr-stack-label>
            <clr-stack-content>
              <input class="clr-input num" type="number" name="memoryMb" min="64" step="64" [(ngModel)]="memoryMb" /> MB
              <span class="fc-muted">({{ memoryMb | mb }})</span>
            </clr-stack-content>
          </clr-stack-block>
          <clr-stack-block>
            <clr-stack-label>Hardware virtualization</clr-stack-label>
            <clr-stack-content>
              <label><input type="checkbox" name="nestedHv" [(ngModel)]="nestedHv" /> Expose hardware assisted virtualization to the guest OS</label>
            </clr-stack-content>
          </clr-stack-block>
          @if (creationType === 'cloud') {
            <clr-stack-block>
              <clr-stack-label>Hard disk 1</clr-stack-label>
              <clr-stack-content>
                <input class="clr-input num" type="number" name="diskGb" min="1" max="65536" [(ngModel)]="diskGb" /> GB
                <span class="fc-muted">Cloned thin from the image and grown to this size (never shrunk)</span>
              </clr-stack-content>
            </clr-stack-block>
          } @else {
            <clr-stack-block [clrSbExpandable]="true" [clrSbExpanded]="true">
              <clr-stack-label>New Hard disk</clr-stack-label>
              <clr-stack-content><input class="clr-input num" type="number" name="diskGb" min="1" max="65536" [(ngModel)]="diskGb" /> GB</clr-stack-content>
              <clr-stack-block>
                <clr-stack-label>Disk Provisioning</clr-stack-label>
                <clr-stack-content>
                  <div class="clr-select-wrapper">
                    <select class="clr-select" name="thin" [(ngModel)]="thin">
                      <option [ngValue]="true">Thin Provision</option>
                      <option [ngValue]="false">Thick Provision</option>
                    </select>
                  </div>
                </clr-stack-content>
              </clr-stack-block>
            </clr-stack-block>
          }
          <clr-stack-block>
            <clr-stack-label>New SCSI controller</clr-stack-label>
            <clr-stack-content>
              <div class="clr-select-wrapper">
                <select class="clr-select" name="scsi" [(ngModel)]="scsi">
                  <option value="lsilogic-sas">LSI Logic SAS</option>
                  <option value="pvscsi">VMware Paravirtual</option>
                  <option value="lsilogic">LSI Logic Parallel</option>
                </select>
              </div>
            </clr-stack-content>
          </clr-stack-block>
          <clr-stack-block [clrSbExpandable]="true" [clrSbExpanded]="true">
            <clr-stack-label>New Network</clr-stack-label>
            <clr-stack-content>
              <div class="clr-select-wrapper">
                <select class="clr-select" name="network" [(ngModel)]="network">
                  <option value="">-- None --</option>
                  @for (n of networks(); track n.name) {
                    <option [value]="n.name">{{ n.name }}</option>
                  }
                </select>
              </div>
            </clr-stack-content>
            <clr-stack-block>
              <clr-stack-label>Adapter Type</clr-stack-label>
              <clr-stack-content>
                <div class="clr-select-wrapper">
                  <select class="clr-select" name="adapter" [(ngModel)]="adapter">
                    <option value="vmxnet3">VMXNET 3</option>
                    <option value="e1000e">E1000E</option>
                    <option value="e1000">E1000</option>
                  </select>
                </div>
              </clr-stack-content>
            </clr-stack-block>
          </clr-stack-block>
          @if (creationType === 'new') {
            <clr-stack-block>
              <clr-stack-label>New CD/DVD Drive</clr-stack-label>
              <clr-stack-content>
                <div class="clr-select-wrapper">
                  <select class="clr-select" name="iso" [(ngModel)]="iso">
                    <option value="">Client Device (no media)</option>
                    @for (i of isos(); track i) {
                      <option [value]="i">{{ i }}</option>
                    }
                  </select>
                </div>
                <button type="button" class="btn btn-link btn-sm" (click)="openIsoDownload()">Download ISO...</button>
              </clr-stack-content>
            </clr-stack-block>
          }
        </clr-stack-view>
      </clr-wizard-page>

      <clr-wizard-page [clrWizardPagePreventDefaultNext]="true" (clrWizardPageOnCommit)="create()" [clrWizardPageNextDisabled]="busy()">
        <ng-template clrPageTitle>Review and create</ng-template>
        <p>Check the summary, then choose Finish to create the virtual machine.</p>
        @if (createError()) {
          <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
            <clr-alert-item><span class="alert-text">{{ createError() }}</span></clr-alert-item>
          </clr-alert>
        }
        <dl class="fc-props">
          <dt>Virtual machine name</dt>
          <dd>{{ name }}</dd>
          <dt>Host</dt>
          <dd>{{ inventory.hostName(host()) }}</dd>
          <dt>Datastore</dt>
          <dd>{{ datastore() }}</dd>
          @if (creationType === 'cloud') {
            <dt>Cloud image</dt>
            <dd>{{ cloudImage }}</dd>
            <dt>Guest customization</dt>
            <dd>{{ ciUserData.trim() ? 'Custom user-data' : 'Host ' + (ciHostname.trim() || name) + ', user ' + ciUser }}</dd>
          }
          <dt>Guest OS name</dt>
          <dd>{{ guestName() }}</dd>
          <dt>CPUs</dt>
          <dd>{{ cpu }}</dd>
          <dt>Memory</dt>
          <dd>{{ memoryMb | mb }}</dd>
          <dt>Hardware virtualization</dt>
          <dd>{{ nestedHv ? 'Exposed to the guest OS' : 'Not exposed' }}</dd>
          <dt>Hard disk 1</dt>
          <dd>{{ diskGb }} GB, {{ creationType === 'cloud' || thin ? 'Thin Provision' : 'Thick Provision' }}</dd>
          <dt>SCSI controller</dt>
          <dd>{{ scsi }}</dd>
          <dt>Network</dt>
          <dd>{{ network ? network + ' (' + adapter + ')' : 'None' }}</dd>
          @if (creationType === 'new') {
            <dt>CD/DVD drive 1</dt>
            <dd>{{ iso || 'No media' }}</dd>
          }
        </dl>
        <clr-checkbox-wrapper>
          <input type="checkbox" clrCheckbox name="powerOn" [(ngModel)]="powerOn" />
          <label>Power on the virtual machine after creation</label>
        </clr-checkbox-wrapper>
        @if (busy()) {
          <p><clr-spinner clrInline></clr-spinner> Creating virtual machine...</p>
        }
      </clr-wizard-page>
    </clr-wizard>
  `,
  styles: `
    .num {
      width: 5rem;
    }
    tbody tr {
      cursor: pointer;
    }
    .fc-props {
      margin-bottom: 0.8rem;
    }
    h5 {
      margin-top: 0.9rem;
      font-weight: 600;
    }
  `,
})
export class NewVmWizardComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly dialogs = inject(DialogService);
  protected readonly inventory = inject(InventoryService);
  private readonly isoService = inject(IsoService);
  private readonly tasks = inject(TaskService);
  private readonly router = inject(Router);

  @ViewChild('wizard') private wizard!: ClrWizard;

  /** Preselected host when the wizard is opened from a host. */
  readonly hostId = input<string>();

  protected readonly imageFolder = CLOUD_IMAGE_FOLDER;
  protected readonly hosts = computed(() => this.inventory.hosts().filter((h) => h.status?.connected));
  protected readonly host = signal('');
  protected readonly datastore = signal('');
  protected readonly stores = signal<Datastore[]>([]);
  protected readonly networks = signal<Network[]>([]);
  protected readonly guests = signal<GuestOs[]>([]);
  protected readonly isos = signal<string[]>([]);
  protected readonly images = signal<FileInfo[]>([]);
  protected readonly catalog = signal<CloudImageCatalogEntry[]>([]);
  protected readonly downloading = signal(new Set<string>());
  protected readonly loadError = signal('');
  protected readonly createError = signal('');
  protected readonly busy = signal(false);

  protected creationType: CreationType = 'new';
  protected name = '';
  protected guestId = '';
  protected cpu = 2;
  protected memoryMb = 4096;
  protected diskGb = 16;
  protected thin = true;
  protected scsi = 'lsilogic-sas';
  protected network = '';
  protected adapter = 'vmxnet3';
  protected iso = '';
  protected powerOn = false;
  protected nestedHv = false;
  // Cloud image deployment
  protected cloudImage = '';
  protected ciHostname = '';
  protected ciUser = 'admin';
  protected ciPassword = '';
  protected ciKeys = '';
  protected ciPackages = '';
  protected ciUserData = '';

  ngOnInit(): void {
    const preset = this.hostId() ?? (this.hosts().length === 1 ? this.hosts()[0].id : '');
    if (preset) void this.selectHost(preset);
    // The catalog only feeds the optional download list.
    void this.api.cloudImageCatalog().then(
      (c) => this.catalog.set(c),
      () => undefined,
    );
  }

  protected guestName(): string {
    return this.guests().find((g) => g.id === this.guestId)?.name ?? this.guestId;
  }

  protected close(): void {
    this.dialogs.newVm.set(null);
  }

  protected openIsoDownload(): void {
    this.dialogs.isoDownload.set({ hostId: this.host(), datastore: this.datastore() });
  }

  /** Loads everything the later pages offer for the chosen host. */
  protected async selectHost(hid: string): Promise<void> {
    this.host.set(hid);
    this.loadError.set('');
    try {
      const [guests, stores, networks] = await Promise.all([this.api.guestOs(hid), this.api.datastores(hid), this.api.networks(hid)]);
      if (this.host() !== hid) return;
      const usable = stores.filter((d) => d.accessible);
      this.guests.set(guests.filter((g) => /64/.test(g.id)).sort((a, b) => a.name.localeCompare(b.name)));
      this.stores.set(usable);
      this.networks.set(networks);
      this.guestId = this.hasGuest('ubuntu64Guest') ? 'ubuntu64Guest' : (this.guests()[0]?.id ?? '');
      this.network = networks.some((n) => n.name === 'VM Network') ? 'VM Network' : '';
      this.iso = '';
      this.selectDatastore(usable[0]?.name ?? '');
      this.isos.set(await this.isoService.list(hid, usable));
    } catch (e) {
      this.loadError.set(errorMessage(e));
    }
  }

  protected selectDatastore(name: string): void {
    this.datastore.set(name);
    this.cloudImage = '';
    void this.loadImages();
  }

  private hasGuest(id: string): boolean {
    return this.guests().some((g) => g.id === id);
  }

  /** Cloud images must be on the datastore the VM is created on (the boot disk is cloned locally). */
  private async loadImages(): Promise<void> {
    const hid = this.host();
    const ds = this.datastore();
    this.images.set([]);
    if (!hid || !ds) return;
    // A datastore without the folder simply has no images.
    const files = await this.api.files(hid, ds, `/${CLOUD_IMAGE_FOLDER}`).catch(() => []);
    if (this.host() !== hid || this.datastore() !== ds) return;
    this.images.set(files.filter((f) => !f.is_dir && /\.vmdk$/i.test(f.name) && !/-flat\.vmdk$/i.test(f.name)));
  }

  protected selectImage(fileName: string): void {
    this.cloudImage = `${CLOUD_IMAGE_FOLDER}/${fileName}`;
    const guess = GUEST_BY_IMAGE.find(([re, id]) => re.test(fileName) && this.hasGuest(id));
    if (guess) this.guestId = guess[1];
  }

  protected downloadImage(entry: CloudImageCatalogEntry): void {
    const hid = this.host();
    const ds = this.datastore();
    const file = (entry.url.split('/').pop() ?? 'image').replace(/\.(qcow2|img)$/i, '') + '.vmdk';
    const target = `${CLOUD_IMAGE_FOLDER}/${file}`;
    this.downloading.update((s) => new Set(s).add(entry.url));
    // The server downloads in the background and completes the task itself; here we only wait to refresh the list.
    this.tasks
      .run(`Download cloud image to ${ds}`, file, () => this.api.downloadCloudImage(hid, ds, entry.url, target))
      .then(async () => {
        for (;;) {
          await new Promise((r) => setTimeout(r, DOWNLOAD_POLL_MS));
          const st = await this.api.downloadIsoStatus(hid, ds, CLOUD_IMAGE_FOLDER, target);
          if (st.status !== 'running') return;
        }
      })
      .catch(() => undefined)
      .finally(() => {
        this.downloading.update((s) => {
          const next = new Set(s);
          next.delete(entry.url);
          return next;
        });
        void this.loadImages();
      });
  }

  protected cloudInitValid(): boolean {
    if (this.ciUserData.trim()) return true;
    return !!this.ciUser.trim() && (!!this.ciPassword || !!this.ciKeys.trim());
  }

  private cloudInit(): CloudInitConfig {
    const hostname = this.ciHostname.trim() || this.name.trim();
    if (this.ciUserData.trim()) return { hostname, user_data: this.ciUserData };
    const keys = this.ciKeys
      .split('\n')
      .map((k) => k.trim())
      .filter(Boolean);
    const packages = this.ciPackages
      .split(',')
      .map((p) => p.trim())
      .filter(Boolean);
    return {
      hostname,
      users: [
        {
          name: this.ciUser.trim(),
          sudo: 'ALL=(ALL) NOPASSWD:ALL',
          shell: '/bin/bash',
          ...(this.ciPassword ? { password: this.ciPassword } : {}),
          ...(keys.length ? { ssh_authorized_keys: keys } : {}),
        },
      ],
      ...(packages.length ? { packages } : {}),
    };
  }

  protected async create(): Promise<void> {
    if (this.busy()) return;
    const hid = this.host();
    const name = this.name.trim();
    const cloud = this.creationType === 'cloud';
    this.createError.set('');
    this.busy.set(true);
    try {
      const vm = await this.tasks.run(cloud ? 'Deploy virtual machine from cloud image' : 'Create virtual machine', name, () =>
        this.api.createVm(hid, {
          name,
          guest_id: this.guestId,
          num_cpus: Number(this.cpu),
          memory_mb: Number(this.memoryMb),
          datastore: this.datastore(),
          disks: [{ size_gb: Number(this.diskGb), thin: cloud || this.thin }],
          network: this.network,
          adapter_type: this.adapter,
          scsi_controller: this.scsi,
          iso_path: cloud ? '' : this.iso,
          nested_hv: this.nestedHv,
          power_on: this.powerOn,
          ...(cloud ? { cloud_image: this.cloudImage, cloud_init: this.cloudInit() } : {}),
        }),
      );
      this.wizard.forceFinish();
      this.close();
      void this.router.navigate(['/vm', hid, vm.id]);
    } catch (e) {
      this.createError.set(errorMessage(e));
    } finally {
      this.busy.set(false);
      void this.inventory.refresh();
    }
  }
}
