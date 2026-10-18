# Changelog

All notable changes to Hostwarden are listed here. Dates are in UTC.

## [Unreleased]

### Changed

- **Renamed to Hostwarden.** The project was called FreeCenter. The
  binary, Docker image and environment variables now use the new name.
  Existing installations keep working: `FREECENTER_*` variables are
  still read, templates created before the rename are still recognised,
  and `freecenter-backups` folders are renamed to `hostwarden-backups`
  the first time backups are listed.

### Security

- **Diagnostics console.** The esxcli console matched commands by
  prefix and passed them to the host's shell as typed, so anything
  could be appended to an allowed command. It now accepts only esxcli
  `list`/`get` queries in a fixed set of namespaces and refuses
  everything a shell would interpret.

### Added

- **Password recovery.** `hostwarden reset-password [username]` sets a
  new password from the terminal when the administrator's one is lost.

- **OIDC single sign-on.** Hostwarden can now delegate authentication
  to an external identity provider (e.g. Keycloak). The login page
  shows a "Sign in with OIDC" button when an administrator configures
  it. The flow is authorization code: the user is redirected to the
  provider, comes back with a code, Hostwarden exchanges it, fetches
  userinfo, maps OIDC groups to a local role, and issues a normal
  session token. Users are auto-provisioned on first login. The OIDC
  client secret is encrypted at rest like host passwords.
- **Granular role-based authorization.** The two-role model
  (admin/readonly) is replaced by roles with five per-area
  permissions: manage virtual machines, manage hosts, manage storage,
  manage users, and console access. The built-in `admin` (all
  permissions) and `readonly` (none) roles cannot be deleted;
  administrators can create custom roles and assign them to users or
  map them to OIDC groups. Every mutating API request is checked
  against the user's permissions.
- **Authentication admin page.** A new page at *Admin > Authentication*
  lets administrators configure the OIDC provider (issuer, client ID
  and secret, scopes, groups claim name, group→role mapping, default
  role) and manage role definitions with a visual permission editor.
- **Disk health (SMART/NVMe).** The datastore Summary now shows a Disk
  Health card with the SMART/NVMe status of every device backing the
  datastore: overall health, reallocated sectors, pending sectors,
  uncorrectable sectors, media errors (NVMe), endurance used (NVMe) and
  temperature. Values that may compromise the drive — reallocated
  sectors > 0, SMART not OK, temperature at or above 65 °C — are shown
  in red; the warning band (50–65 °C, endurance ≥ 80 %) is yellow.
  Health is read over SSH with `esxcli storage core device smart get`
  (SATA/SAS) and `esxcli nvme device log smart get` (NVMe, with
  Kelvin→Celsius conversion and device-to-HBA mapping).
- **Alarms.** The root Summary gains an Alarms card that aggregates
  active disk-health conditions across all connected hosts. Alarms are
  probed every 10 minutes in the background; a manual Refresh button
  triggers an immediate probe.
- **New API endpoints.**
  - `GET /auth/oidc/login`, `GET /auth/oidc/callback` — OIDC browser
    redirect flow.
  - `GET /oidc/config`, `PUT /oidc/config` — read / save the OIDC
    configuration (client secret never returned).
  - `GET /roles`, `POST /roles`, `PUT /roles/{id}`,
    `DELETE /roles/{id}` — role CRUD (built-in roles are protected).
  - `GET /hosts/{hid}/datastores/{ds}/health` — cached disk health of
    one datastore.
  - `GET /alarms` — cached alarm list across all hosts.
  - `POST /alarms/refresh` — trigger an immediate probe and return the
    fresh alarms.
- **esxcli allow list.** `esxcli storage core device list`,
  `esxcli storage core device smart`, `esxcli nvme device list`,
  `esxcli nvme device log` and `esxcli nvme device namespace` are now
  permitted in the read-only esxcli console.
- **Relocation between datastores.** A virtual machine can be moved to
  another datastore of the same host: a powered-off machine is copied
  and registered on the target, a running one is suspended (default) or
  shut down first and resumed or powered on afterwards. The source copy
  is deleted or kept, and thin disks stay thin.

## 2026-10-07

### Added

- **Datastore browser**: register a virtual machine that already exists
  on a datastore from its `.vmx` file, and download files from a
  datastore.

## 2026-10-06

### Added

- Initial public release of Hostwarden: one web console for all your
  standalone ESXi hosts, with virtual machine lifecycle, templates and
  clones, migration between hosts, backups, scheduled tasks, monitoring,
  users and roles, a REST API and a Terraform provider.
