# REST API

Everything the web console does goes through this API. All paths are under `/api/v1` and, except for the login, need the header `Authorization: Bearer <token>`.

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"..."}' | jq -r .token)

curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/hosts
```

A token expires after 7 days without use. Errors are returned as `{"error": "..."}` with a matching HTTP status. Users with the read-only role can call every `GET` (and the queries listed as such below), nothing else.

Long operations (migration, clone, backup, restore) answer `202` with a **job**: poll `GET /jobs/{id}` until its `status` is `ok` or `error`.

Every modifying request is recorded in Recent Tasks. A client can label its requests with the headers `X-Task-Name` and `X-Task-Target` (URL-encoded).

## Session and users

| Method | Path | Description |
|---|---|---|
| POST | `/auth/login` | `{username,password}` → token, role |
| GET | `/auth/oidc/login` | redirect to the OIDC provider's authorization endpoint (no token needed) |
| GET | `/auth/oidc/callback` | OIDC redirect target: exchanges the code, creates/links a local user, issues a session token |
| POST | `/auth/logout` | ends the session |
| GET | `/auth/me` | current user |
| POST | `/auth/password` | `{current,new}`: change one's own password |
| GET, POST | `/users` | list / create users (`{username,password,role}`); administrators only |
| PUT, DELETE | `/users/{id}` | change role or password / delete |

### OIDC configuration and roles (administrators only)

| Method | Path | Description |
|---|---|---|
| GET | `/oidc/config` | current OIDC configuration (client secret never returned) |
| PUT | `/oidc/config` | `{enabled,issuer,client_id,client_secret,scopes[],claim_name,group_roles{},default_role}` — save; empty secret keeps the existing one |
| GET | `/roles` | all role definitions with their permissions |
| POST | `/roles` | create a custom role `{name,description,manage_vms,manage_hosts,manage_storage,manage_users,console_access}` |
| PUT | `/roles/{id}` | replace a role (built-in roles can be edited but not deleted) |
| DELETE | `/roles/{id}` | delete a custom role (built-in roles are protected) |

Permissions are checked on every mutating request: a user needs `manage_vms` for VM lifecycle, `manage_hosts` for host configuration, `manage_storage` for file uploads and ISO downloads, `manage_users` for user and role management, and `console_access` for the VM console. The built-in `admin` role has all permissions; `readonly` has none.

## Hosts

| Method | Path | Description |
|---|---|---|
| GET, POST | `/hosts` | list with live status / register (`{name,address,username,password,ssh_enabled,ssh_port,insecure}`) |
| GET, PUT, DELETE | `/hosts/{hid}` | live status / update connection settings / unregister |
| GET | `/hosts/{hid}/test` | connectivity test (API and SSH) |
| POST | `/hosts/{hid}/power` | `{action:"reboot"\|"shutdown", reason}` |
| POST | `/hosts/{hid}/maintenance` | `{enabled}` |
| GET | `/hosts/{hid}/datastores` | datastores with capacity and free space |
| GET | `/hosts/{hid}/datastores/{ds}/health` | SMART/NVMe health of the devices backing a datastore (SSH-only, best effort) |
| GET | `/hosts/{hid}/networks` | networks available to virtual machines |
| GET | `/hosts/{hid}/guest-os` | supported guest operating systems |
| POST | `/hosts/{hid}/esxcli` | `{command:"esxcli ..."}`: esxcli `list`/`get` queries in a fixed set of namespaces; anything else is refused (a query: allowed to read-only users) |
| GET | `/hosts/{hid}/performance`, `/hosts/{hid}/events` | real-time statistics of the last hour / event log |

### Host configuration

| Method | Path | Description |
|---|---|---|
| GET | `/hosts/{hid}/services` | services with state and startup policy |
| POST | `/hosts/{hid}/services/{key}` | `{action:start\|stop\|restart, policy:on\|off}` |
| GET | `/hosts/{hid}/network` | virtual switches, port groups, physical adapters |
| POST | `/hosts/{hid}/network/portgroups` | `{name,vswitch,vlan}` |
| PUT, DELETE | `/hosts/{hid}/network/portgroups/{name}` | change the VLAN / remove |
| POST | `/hosts/{hid}/network/vswitches` | `{name,uplinks[]}` |
| DELETE | `/hosts/{hid}/network/vswitches/{name}` | remove (must have no port groups) |
| GET, PUT | `/hosts/{hid}/time` | host clock and NTP (`{ntp_enabled,ntp_servers[]}`) |

### Datastore files

| Method | Path | Description |
|---|---|---|
| GET | `/hosts/{hid}/datastores/{ds}/files?path=/` | list a folder |
| POST | `/hosts/{hid}/datastores/{ds}/files/upload?path=/dir/file.iso` | upload (raw body) |
| GET | `/hosts/{hid}/datastores/{ds}/files/download?path=/dir/file` | download a file (administrators only) |
| DELETE | `/hosts/{hid}/datastores/{ds}/files?path=/dir/file` | delete a file or an empty folder |
| POST | `/hosts/{hid}/datastores/{ds}/iso/download` | download an ISO from a URL to the datastore |
| GET | `/cloud-images` | catalog of downloadable cloud images |
| POST | `/hosts/{hid}/datastores/{ds}/cloud-image/download` | download and convert a cloud image |

## Virtual machines

| Method | Path | Description |
|---|---|---|
| GET, POST | `/hosts/{hid}/vms` | list / create |
| POST | `/hosts/{hid}/vms/register` | `{datastore,path}`: add to the inventory a machine that already exists on a datastore, from its `.vmx` file |
| GET, PUT, DELETE | `/hosts/{hid}/vms/{vmid}` | details / change settings / delete (`?delete_files=false` to only unregister) |
| POST | `/hosts/{hid}/vms/{vmid}/power` | `{action:"on"\|"off"\|"reset"\|"suspend"\|"shutdown_guest"\|"reboot_guest"}` |
| GET (WebSocket) | `/hosts/{hid}/vms/{vmid}/console?token=<token>` | console of a running machine (RFB, subprotocol `binary`) |
| GET | `/hosts/{hid}/vms/{vmid}/performance`, `.../events` | real-time statistics / event log |
| POST | `/hosts/{hid}/vms/{vmid}/clone` | `{name,datastore?,power_on,template}` → job |
| POST | `/hosts/{hid}/vms/{vmid}/migrate/check` | `{target_host_id,target_datastore}` → compatibility report (a query) |
| POST | `/hosts/{hid}/vms/{vmid}/migrate` | as above plus `mode` (`cold`\|`suspend`\|`stop_start`), `delete_source`, `reclaim_thin`, `precopy` → job |
| POST | `/hosts/{hid}/vms/{vmid}/relocate/check` | `{target_datastore}` → compatibility report (a query) |
| POST | `/hosts/{hid}/vms/{vmid}/relocate` | as above plus `mode` (`cold`\|`suspend`\|`stop_start`), `delete_source`, `reclaim_thin` → job |
| GET | `/jobs/{id}` | progress of a long operation |
| GET | `/tasks` | Recent Tasks |

Creating a virtual machine:

```bash
curl -X POST http://localhost:8080/api/v1/hosts/$HID/vms \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "name": "web-01", "guest_id": "ubuntu64Guest",
    "num_cpus": 2, "memory_mb": 4096,
    "datastore": "datastore1", "disks": [{"size_gb": 16, "thin": true}],
    "network": "VM Network", "adapter_type": "vmxnet3",
    "power_on": true
  }'
```

With `"cloud_image": "cloud-images/rocky-9.vmdk"` the boot disk is cloned from that image, and an optional `cloud_init` object (`hostname`, `users`, `ssh_authorized_keys`, `packages`, `runcmd`, `write_files`, `network_config`, or a raw `user_data`) configures the guest on first boot.

Fields of `PUT /hosts/{hid}/vms/{vmid}`. All are optional and only what is present is applied; the machine must be powered off.

| Field | Meaning |
|---|---|
| `num_cpus`, `cores_per_socket`, `memory_mb`, `annotation` | CPU, memory, notes |
| `name`, `guest_id` | display name (files are not renamed), guest OS |
| `firmware` (`bios`\|`efi`), `boot_delay_ms`, `enter_bios_setup` | boot options |
| `cpu_hot_add`, `memory_hot_add`, `nested_hv` | hot add, nested virtualization |
| `usb_controller` (`""`\|`usb2`\|`usb3`) | USB controller |
| `template` | convert to / from template |
| `add_disks[{size_gb,thin,datastore?}]`, `resize_disks[{key,size_gb}]`, `remove_disk_keys[]`, `delete_disk_files` | disks |
| `add_nics[{network,adapter_type}]`, `update_nics[{key,network?,start_connected?}]`, `remove_nic_keys[]` | network adapters |
| `add_cdroms[{iso_path}]`, `update_cdroms[{key,iso_path}]`, `remove_cdrom_keys[]` | CD/DVD drives (empty `iso_path` = no media) |
| `iso_path` | shortcut for the first CD/DVD drive (empty = remove it) |

### Snapshots

| Method | Path | Description |
|---|---|---|
| GET, POST | `/hosts/{hid}/vms/{vmid}/snapshots` | list / take (`{name,description,memory,quiesce}`) |
| POST | `/hosts/{hid}/vms/{vmid}/snapshots/{snapid}/revert` | revert (`?suppress_power_on=true`) |
| DELETE | `/hosts/{hid}/vms/{vmid}/snapshots/{snapid}` | delete (`?children=true`) |
| POST | `/hosts/{hid}/vms/{vmid}/snapshots/remove-all` | delete all |

## Backups and schedules

| Method | Path | Description |
|---|---|---|
| POST | `/hosts/{hid}/vms/{vmid}/backup` | `{datastore,keep,target_host_id?,target_datastore?}` → job |
| GET | `/backups` | backups found on all connected hosts |
| DELETE | `/hosts/{hid}/backups/{ds}/{vm}/{stamp}` | delete a backup |
| POST | `/hosts/{hid}/backups/{ds}/{vm}/{stamp}/restore` | restore as a new machine (`{name,datastore,power_on}`) → job |
| GET, POST | `/schedules` | list / create a scheduled snapshot or backup |
| PUT, DELETE | `/schedules/{id}` | replace / delete |
| POST | `/schedules/{id}/run` | run now |
| GET | `/alarms` | active conditions across all connected hosts (disk health so far), cached server-side and refreshed every ~60 s |
