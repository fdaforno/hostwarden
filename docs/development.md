# Development and internals

## How it works

A standalone ESXi host may answer management API calls that change something with `RestrictedVersion`, while queries work normally. Hostwarden therefore uses two channels to each host, both with the administrator credentials you registered:

- the host's **management API** (port 443, through [govmomi](https://github.com/vmware/govmomi)) for everything it reads: inventory, host status, datastores, networks, performance, events, console tickets, file transfers;
- **SSH** (port 22) for everything it changes, using the tools that ship with ESXi (`vim-cmd`, `vmkfstools`, `esxcli`) and by editing virtual machine configuration files.

Nothing is installed on the hosts. For migrations and remote backups Hostwarden creates a dedicated SSH key on the source host and authorizes it on the target for the source address only, so that the two hosts exchange data directly instead of through the Hostwarden server.

The console of a virtual machine is the host's WebMKS stream (RFB over WebSocket), relayed by the Hostwarden server to [noVNC](https://novnc.com) in the browser: the browser never talks to the host and does not need to trust its certificate.

The server keeps its own state (users, registered hosts, schedules) in a single JSON file. Host passwords in it are encrypted with AES-256-GCM. Recent Tasks and background jobs are kept in memory and start empty after a restart.

How migration works in detail is described in [migration.md](migration.md).

## Repository layout

| Path | Content |
|---|---|
| `cmd/hostwarden` | server entry point |
| `internal/esxi` | everything that talks to a host |
| `internal/api` | REST API, inventory cache, task log, background jobs, scheduler |
| `internal/store` | persistent state and its encryption |
| `web/ui` | web console (Angular + Clarity); its build in `web/dist` is embedded in the server binary |

The Terraform provider is a separate project: [terraform-provider-hostwarden](https://github.com/fdaforno/terraform-provider-hostwarden).

## Build and run

Requirements: Go 1.26+, Node.js 22+ (for the web console), Docker for the image.

```bash
make run          # build the web console and start the server on http://localhost:8080 (data in ./data)
make ui-dev       # optional: web console with hot reload on :4200, /api forwarded to :8080
make build        # server binary with the web console embedded
make vet test     # go vet and tests
make ui-lint      # lint of the web console
docker build -t hostwarden .
```

Creating virtual machines with cloud-init needs `genisoimage` on the machine running the server. The Docker image includes it.

VM creation queries the target host's configuration descriptors and requires both
creation and power-on support. If `vmx-21` is supported, Hostwarden preserves the
upstream hardware version and `pciRootBridge` layout, even when the host's default
is newer. Otherwise it prefers the host default when it is one of `vmx-13`,
`vmx-14`, `vmx-15`, `vmx-17`, `vmx-18`, `vmx-19`, and `vmx-20`, paired with the
legacy PCI bridge and PCIe root ports. If no known default is available, it selects
the highest supported profile in that set. Direct ESXi connections query the selected
key in the single-host environment browser; vCenter queries also name the target host.
The returned version must match; guest OS choices use that same configuration. Unknown profiles,
failed queries, and mismatched responses stop creation before any datastore
commands. Existing VMs are not converted.

Compatibility tests exercise the real creation path against SOAP and SSH
simulators, compare the modern VMX to a captured upstream baseline, and verify
older bridge layouts and failure handling. These tests do not certify guest
bootability on every ESXi release; actual power-on and guest device checks still
require the corresponding ESXi host.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `HOSTWARDEN_ADDR` | `:8080` | listen address |
| `HOSTWARDEN_DATA` | `./data` (`/data` in the image) | state directory |
| `HOSTWARDEN_ADMIN_USER` | `admin` | initial administrator, created on first start |
| `HOSTWARDEN_ADMIN_PASSWORD` | `admin` | its password; with the default the console insists on a change |
| `HOSTWARDEN_SECRET_KEY` | (file `secret.key` in the state directory) | key that encrypts host passwords |

**About the key.** By default the key is generated on first start and stored next to the state file: a copy of the state file alone is useless, but whoever can read the whole directory has both. To keep them apart, provide the key through `HOSTWARDEN_SECRET_KEY`. Without the right key the server refuses to start.

## Lost administrator password

`hostwarden reset-password [username]` asks for a new password on the terminal and writes it to the state file (the user defaults to `admin`). The server keeps its state in memory, so restart it afterwards:

```bash
docker exec -it hostwarden hostwarden reset-password
docker restart hostwarden
```

## Notes on behavior

- Hardware changes and deletion require the virtual machine to be powered off.
- Disks can only grow, and not while snapshots exist.
- The inventory is cached by the server and refreshed in the background; every change invalidates the cache of the host involved.
- The `esxcli` console only accepts read-only commands from an allow list.
- **Authentication** supports two modes, available side by side:
  - *Local*: username and password verified with bcrypt. A 32-byte random token is issued and stored as a SHA-256 hash; it expires after 7 days of inactivity.
  - *OIDC*: when an administrator configures an external identity provider (e.g. Keycloak), the login page shows a "Sign in with OIDC" button. The flow is authorization code with PKCE: the user is redirected to the provider, comes back with a code, Hostwarden exchanges it, fetches userinfo, maps the OIDC groups to a local role, and issues a normal session token. Users are auto-provisioned on first login.
- **Authorization** is role-based with five permissions: `manage_vms`, `manage_hosts`, `manage_storage`, `manage_users`, `console_access`. The two built-in roles (`admin` with all permissions, `readonly` with none) cannot be deleted; administrators can create custom roles and assign them to users or map them to OIDC groups.
