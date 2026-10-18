import { Injectable, signal } from '@angular/core';

import { Host, Schedule, Vm } from './models';

export interface ConfirmOptions {
  title: string;
  message: string;
  okLabel?: string;
  danger?: boolean;
  /** When set, the dialog shows a text input with this label. */
  promptLabel?: string;
  promptDefault?: string;
}

export interface ConfirmState extends ConfirmOptions {
  resolve: (value: string | null) => void;
}

/** Central registry of the modal dialogs rendered once by the shell. */
@Injectable({ providedIn: 'root' })
export class DialogService {
  readonly confirmState = signal<ConfirmState | null>(null);
  /** `host` set = edit connection settings, unset = add a new host. */
  readonly hostForm = signal<{ host?: Host } | null>(null);
  readonly newVm = signal<{ hostId?: string } | null>(null);
  readonly editVm = signal<Vm | null>(null);
  readonly takeSnapshot = signal<Vm | null>(null);
  readonly migrateVm = signal<Vm | null>(null);
  /** Move a VM to another datastore of the same host. */
  readonly relocateVm = signal<Vm | null>(null);
  /** `template` = the copy becomes a template. */
  readonly cloneVm = signal<{ vm: Vm; template: boolean } | null>(null);
  readonly changePassword = signal(false);
  /** Back up now (`schedule` false) or define a schedule; `edit` = change an existing schedule. */
  readonly protectVm = signal<{ vm?: Vm; schedule: boolean; edit?: Schedule } | null>(null);
  readonly isoDownload = signal<{ hostId: string; datastore?: string } | null>(null);
  /** The VM whose console is open in the floating window (one at a time). */
  readonly console = signal<{ hostId: string; vmId: string } | null>(null);

  async confirm(opts: ConfirmOptions): Promise<boolean> {
    return (await this.open(opts)) !== null;
  }

  /** Resolves to the typed text, or null when cancelled. */
  prompt(opts: ConfirmOptions & { promptLabel: string }): Promise<string | null> {
    return this.open(opts);
  }

  private open(opts: ConfirmOptions): Promise<string | null> {
    return new Promise((resolve) => {
      this.confirmState.set({
        ...opts,
        resolve: (value) => {
          this.confirmState.set(null);
          resolve(value);
        },
      });
    });
  }
}
