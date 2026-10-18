# Migrating virtual machines between hosts

Hostwarden does not do live migration: the memory of a running machine is never transferred while the machine runs. It migrates in the ways a standalone host allows, and tells you up front which one applies and how long the machine will be stopped.

## The four cases

| # | Situation | What happens | Stop of the machine |
|---|---|---|---|
| 1 | Powered-off machine | its files are copied to the target host (nothing is copied when the datastore is shared), it is registered there and removed from the source | none: it was already off |
| 2 | Running machine, hosts with the same CPU | it is suspended, copied with its memory image, and resumed on the target | the time to write the memory to disk, copy and read it back |
| 3 | Running machine, with pre-copy | a temporary snapshot freezes the disks, which are copied while the machine keeps running; then the machine is stopped and only what changed is copied | the time to copy the changes, not the whole disks |
| 4 | Running machine, hosts with different CPUs | the guest is shut down, the machine is copied and powered on on the target | copy and boot |

A suspended machine can only resume on a compatible CPU: across different CPUs Hostwarden proposes case 4. Case 3 combines with 2 and 4.

In every case the source copy is removed only once the machine is registered on the target and, if it was running, started. If anything fails, the copy on the target is deleted and the machine is brought back on the source.

## How it is done

- **Direct transfer.** The source host copies to the target host with `scp`: the data does not go through the Hostwarden server.
- **Keys.** On the first migration Hostwarden creates a key pair on the source host (ECDSA: the SSH server of ESXi runs in FIPS mode and rejects ed25519). The private key never leaves the host. The public key is authorized on the target for the address of the source only, the `sshClient` firewall rule is enabled on the source, and the host key of the target is pinned to the one Hostwarden itself sees.
- **Pre-copy.** The disks of a running machine are locked and cannot be read directly. A temporary snapshot freezes them, and `vmkfstools -i` clones them into a staging folder next to the machine (which needs free space on the source datastore). The staging copy travels to the target while the machine works. Then the machine is stopped, only the snapshot deltas and the configuration are copied, the deltas are linked to the new base disks, and the snapshot is consolidated on the target with the machine already running again. Not available when the machine already has snapshots.
- **Shared datastore.** When the target datastore is the very same volume on both hosts, nothing is copied: the machine is stopped, unregistered on the source, registered and started on the target. On failure it is registered on the source again. Files are never deleted.
- **Thin disks.** The copy allocates them in full. The option "Keep thin disks thin" makes them thin again with `vmkfstools -K` (skipped when the machine has snapshots).

## Relocation between datastores of the same host

The **Relocate** dialog moves a virtual machine from one datastore to another on the same host. It works like a migration to the same host:

- a powered-off machine is copied to the target datastore and registered there;
- a running machine is suspended (default) or shut down, copied, and resumed or powered on;
- the source copy is deleted or kept, and the VM gets a new identity on the host.

The copy is done with `cp -r` on the host, so thin disks stay thin. The "Keep thin disks thin" option runs `vmkfstools -K` afterwards to reclaim unused space.

## Requirements and limits

- All disks of the machine must be in its own folder.
- The target host needs a port group with the same name for each network adapter, enough free space, and no folder with the machine's name on the target datastore.
- The source host must reach the target host on the SSH port.
- It is not transparent: while the machine is stopped, client connections can time out.

## What has been tried

Tried on real hosts: a powered-off machine (case 1), pre-copy with shut down and power on (cases 3 and 4) between hosts with different CPUs, and the rollback, by interrupting a copy midway. Implemented but not yet tried on real hardware: suspend and resume (case 2, which needs two hosts with the same CPU) and the shared datastore path.
