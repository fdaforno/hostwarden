import { Component, OnInit, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import {
  ActionsService,
  ApiService,
  BytesPipe,
  DialogService,
  InventoryService,
  MigrationCheck,
  MigrationMode,
  TaskService,
  Vm,
  errorMessage,
} from '../../core';

/**
 * Relocate a virtual machine to another datastore of the same host. The VM is
 * stopped (or suspended), copied to the target datastore and registered there.
 */
@Component({
  selector: 'app-relocate-vm-dialog',
  imports: [ClarityModule, FormsModule, BytesPipe],
  template: `
    <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="close()" [clrModalStaticBackdrop]="true" clrModalSize="lg">
      <h3 class="modal-title">Relocate <span class="fc-muted">| {{ vm().name }}</span></h3>
      <div class="modal-body">
        @if (datastores().length === 0) {
          <clr-alert clrAlertType="info" [clrAlertClosable]="false">
            <clr-alert-item
              ><span class="alert-text">There is no other accessible datastore on this host.</span></clr-alert-item
            >
          </clr-alert>
        } @else {
          <form clrForm clrLayout="horizontal">
            <clr-select-container>
              <label>Source datastore</label>
              <select clrSelect name="source" [ngModel]="sourceDatastore()" disabled>
                <option [value]="sourceDatastore()">{{ sourceDatastore() }}</option>
              </select>
            </clr-select-container>
            <clr-select-container>
              <label>Target datastore</label>
              <select clrSelect name="datastore" [ngModel]="datastore()" (ngModelChange)="selectDatastore($event)">
                @for (d of datastores(); track d.name) {
                  <option [value]="d.name">{{ d.name }} ({{ d.free_bytes | bytes }} free)</option>
                }
              </select>
            </clr-select-container>
          </form>

          <h5>Compatibility</h5>
          @if (checking()) {
            <p><clr-spinner clrInline></clr-spinner> Running compatibility checks...</p>
          } @else if (error()) {
            <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
              <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
            </clr-alert>
          } @else if (check(); as c) {
            @if (c.blockers.length > 0) {
              <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
                @for (b of c.blockers; track b) {
                  <clr-alert-item><span class="alert-text">{{ b }}</span></clr-alert-item>
                }
              </clr-alert>
            } @else {
              <clr-alert clrAlertType="success" [clrAlertClosable]="false">
                <clr-alert-item>
                  <span class="alert-text">Compatibility checks succeeded. {{ c.size_bytes | bytes }} to copy.</span>
                </clr-alert-item>
              </clr-alert>
            }
            @if (c.warnings.length > 0) {
              <clr-alert clrAlertType="warning" [clrAlertClosable]="false">
                @for (w of c.warnings; track w) {
                  <clr-alert-item><span class="alert-text">{{ w }}</span></clr-alert-item>
                }
              </clr-alert>
            }

            @if (c.blockers.length === 0) {
              @if (c.power_state === 'poweredOn') {
                <h5>Running virtual machine</h5>
                <clr-radio-container>
                  <clr-radio-wrapper>
                    <input type="radio" clrRadio name="mode" value="suspend" [(ngModel)]="runningMode" />
                    <label>Suspend, relocate and resume (the guest keeps running where it left off)</label>
                  </clr-radio-wrapper>
                  <clr-radio-wrapper>
                    <input type="radio" clrRadio name="mode" value="stop_start" [(ngModel)]="runningMode" />
                    <label>Shut down, relocate and power on (the guest operating system restarts)</label>
                  </clr-radio-wrapper>
                </clr-radio-container>
                <p class="fc-muted">
                  The virtual machine is unavailable while all its files are copied. The guest is shut down through
                  VMware Tools; if it does not respond within 3 minutes it is powered off.
                </p>
              } @else {
                <p class="fc-muted">The virtual machine is not running: its files are copied and it is registered on the target datastore.</p>
              }

              <h5>Options</h5>
              <clr-checkbox-container>
                <clr-checkbox-wrapper>
                  <input type="checkbox" clrCheckbox name="deleteSource" [(ngModel)]="deleteSource" />
                  <label>Delete the source files after a successful relocation</label>
                </clr-checkbox-wrapper>
                <clr-checkbox-wrapper>
                  <input type="checkbox" clrCheckbox name="reclaimThin" [(ngModel)]="reclaimThin" />
                  <label>Reclaim unused space of thin disks on the target (takes longer)</label>
                </clr-checkbox-wrapper>
              </clr-checkbox-container>
              <p class="fc-muted">
                The source virtual machine is removed only after the target is registered and, if it was running,
                powered on. On any failure the source is restored.
              </p>
            }
          }
        }
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" (click)="close()">Cancel</button>
        <button type="button" class="btn btn-primary" [disabled]="!canRelocate()" (click)="relocate()">Relocate</button>
      </div>
    </clr-modal>
  `,
  styles: `
    h5 {
      margin-top: 0.9rem;
      font-weight: 600;
    }
    clr-alert {
      display: block;
      margin-top: 0.3rem;
    }
  `,
})
export class RelocateVmDialogComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly actions = inject(ActionsService);
  private readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);
  private readonly tasks = inject(TaskService);

  readonly vm = input.required<Vm>();

  protected readonly sourceDatastore = computed(() => {
    const m = (this.vm().vmx_path ?? '').match(/^\[([^[\]]+)\]/);
    return m?.[1] ?? '';
  });
  protected readonly datastores = computed(() =>
    (this.inventory.datastoresByHost()[this.vm().host_id] ?? []).filter(
      (d) => d.accessible && d.name !== this.sourceDatastore(),
    ),
  );
  protected readonly datastore = signal('');
  protected readonly check = signal<MigrationCheck | null>(null);
  protected readonly checking = signal(false);
  protected readonly error = signal('');

  protected runningMode: MigrationMode = 'suspend';
  protected deleteSource = true;
  protected reclaimThin = true;

  /** Discards the result of a check that was superseded by a newer selection. */
  private checkSeq = 0;

  ngOnInit(): void {
    const best = [...this.datastores()].sort((a, b) => b.free_bytes - a.free_bytes)[0];
    this.selectDatastore(best?.name ?? '');
  }

  protected close(): void {
    this.dialogs.relocateVm.set(null);
  }

  protected selectDatastore(name: string): void {
    this.datastore.set(name);
    void this.runCheck();
  }

  private async runCheck(): Promise<void> {
    const seq = ++this.checkSeq;
    this.check.set(null);
    this.error.set('');
    if (!this.datastore()) return;
    this.checking.set(true);
    try {
      const res = await this.api.relocateCheck(this.vm().host_id, this.vm().id, {
        target_datastore: this.datastore(),
      });
      if (seq !== this.checkSeq) return;
      this.check.set(res);
      this.runningMode = res.recommended_mode === 'cold' ? 'suspend' : res.recommended_mode;
    } catch (e) {
      if (seq === this.checkSeq) this.error.set(errorMessage(e));
    } finally {
      if (seq === this.checkSeq) this.checking.set(false);
    }
  }

  private mode(c: MigrationCheck): MigrationMode {
    if (c.power_state !== 'poweredOn') return 'cold';
    return this.runningMode;
  }

  protected canRelocate(): boolean {
    const c = this.check();
    return !!c && !this.checking() && c.blockers.length === 0 && !!this.datastore();
  }

  protected relocate(): void {
    const c = this.check();
    if (!c || !this.canRelocate()) return;
    this.tasks.panelOpen.set(true);
    void this.actions.relocateVm(
      this.vm(),
      {
        target_datastore: this.datastore(),
        mode: this.mode(c),
        delete_source: this.deleteSource,
        reclaim_thin: this.reclaimThin,
        precopy: false,
      },
      this.datastore(),
    );
    this.close();
  }
}
