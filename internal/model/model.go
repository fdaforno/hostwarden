package model

import (
	"fmt"
	"strings"
	"time"
)

// HostRecord is a registered ESXi host (persisted).
type HostRecord struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Address    string    `json:"address"` // host or host:port (https 443)
	Username   string    `json:"username"`
	Password   string    `json:"password,omitempty"`
	SSHPort    int       `json:"ssh_port"`
	Insecure   *bool     `json:"insecure,omitempty"` // skip TLS verify (default true, ESXi self-signed)
	SSHEnabled bool      `json:"ssh_enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

// VM is the API view of a virtual machine.
type VM struct {
	HostID          string     `json:"host_id"`
	ID              string     `json:"id"` // ESXi MoRef value (vm-123)
	Name            string     `json:"name"`
	PowerState      string     `json:"power_state"` // poweredOn | poweredOff | suspended
	NumCPUs         int        `json:"num_cpus"`
	CoresPerSocket  int        `json:"cores_per_socket"`
	MemoryMB        int64      `json:"memory_mb"`
	GuestID         string     `json:"guest_id"`
	GuestFullName   string     `json:"guest_full_name"`
	HardwareVersion string     `json:"hardware_version"`
	UUID            string     `json:"uuid"`
	Annotation      string     `json:"annotation"`
	VMXPath         string     `json:"vmx_path"`
	IPAddress       string     `json:"ip_address,omitempty"`
	GuestHostname   string     `json:"guest_hostname,omitempty"`
	ToolsStatus     string     `json:"tools_status,omitempty"`
	UptimeSeconds   int32      `json:"uptime_seconds,omitempty"`
	CPUUsageMHz     int32      `json:"cpu_usage_mhz,omitempty"`
	GuestMemoryMB   int32      `json:"guest_memory_mb,omitempty"`
	HostMemoryMB    int32      `json:"host_memory_mb,omitempty"`
	Datastores      []string   `json:"datastores"`
	Networks        []string   `json:"networks"`
	Disks           []VMDisk   `json:"disks"`
	NICs            []VMNIC    `json:"nics"`
	CdromISO        string     `json:"cdrom_iso,omitempty"`
	SnapshotCount   int        `json:"snapshot_count"`
	Snapshots       []Snapshot `json:"snapshots,omitempty"`
	BootTime        *time.Time `json:"boot_time,omitempty"`
	Firmware        string     `json:"firmware,omitempty"` // bios | efi
	BootDelayMs     int64      `json:"boot_delay_ms"`
	CPUHotAdd       bool       `json:"cpu_hot_add"`
	MemoryHotAdd    bool       `json:"memory_hot_add"`
	NestedHV        bool       `json:"nested_hv"`
	USBController   string     `json:"usb_controller,omitempty"` // usb2 | usb3
	Cdroms          []VMCdrom  `json:"cdroms"`
	Template        bool       `json:"template"` // marked as template: cannot be powered on, meant to be cloned
}

type VMDisk struct {
	Key        int32  `json:"key"`
	Label      string `json:"label"`
	CapacityKB int64  `json:"capacity_kb"`
	CapacityGB int64  `json:"capacity_gb"`
	Thin       bool   `json:"thin"`
	Mode       string `json:"mode"`
	Path       string `json:"path"`
	Datastore  string `json:"datastore"`
}

type VMNIC struct {
	Key            int32  `json:"key"`
	Label          string `json:"label"`
	Network        string `json:"network"`
	AdapterType    string `json:"adapter_type"`
	MACAddress     string `json:"mac_address"`
	Connected      bool   `json:"connected"`
	StartConnected bool   `json:"start_connected"`
}

// VMCdrom is a virtual CD/DVD drive. ISO is empty for a client device (no media).
type VMCdrom struct {
	Key            int32  `json:"key"`
	Label          string `json:"label"`
	ISO            string `json:"iso"`
	StartConnected bool   `json:"start_connected"`
}

type Snapshot struct {
	ID          string `json:"id"` // vim-cmd snapshot id
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description"`
	CreateTime  string `json:"create_time,omitempty"`
	HasChildren bool   `json:"has_children"`
}

// HostStatus is live info about an ESXi host.
type HostStatus struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Address         string     `json:"address"`
	Connected       bool       `json:"connected"`
	SSHOk           bool       `json:"ssh_ok"`
	Product         string     `json:"product,omitempty"`
	Version         string     `json:"version,omitempty"`
	Build           string     `json:"build,omitempty"`
	OSType          string     `json:"os_type,omitempty"`
	OSID            string     `json:"os_id,omitempty"`
	CPUModel        string     `json:"cpu_model,omitempty"`
	CPUCores        int32      `json:"cpu_cores,omitempty"`
	CPUs            int32      `json:"cpus,omitempty"`
	CPUMHz          int32      `json:"cpu_mhz,omitempty"`
	MemoryTotalMB   int64      `json:"memory_total_mb,omitempty"`
	MemoryUsedMB    int64      `json:"memory_used_mb,omitempty"`
	VMsRunning      int32      `json:"vms_running,omitempty"`
	VMsTotal        int        `json:"vms_total,omitempty"`
	UptimeSeconds   int64      `json:"uptime_seconds,omitempty"`
	BootTime        *time.Time `json:"boot_time,omitempty"`
	MaintenanceMode bool       `json:"maintenance_mode"`
	ConnectionState string     `json:"connection_state,omitempty"`
	LicenseName     string     `json:"license_name,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

type Datastore struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	CapacityB  int64  `json:"capacity_bytes"`
	FreeB      int64  `json:"free_bytes"`
	URL        string `json:"url,omitempty"`
	Accessible bool   `json:"accessible"`
}

// ---- disk health (SMART / NVMe), read over SSH ----

// DiskHealth is the health of a single backing device of a datastore.
//
// On a standalone host, SMART/NVMe health is only reachable over SSH
// (esxcli storage core device smart / esxcli storage core device list).
// Not every controller exposes every field: missing values stay zero
// and the corresponding *Ok flag is false.
type DiskHealth struct {
	// Device ID (naa.xxx, eui.xxx or t10.xxx) and display name.
	DeviceID   string `json:"device_id"`
	DisplayName string `json:"display_name,omitempty"`
	// SAS/SATA: "HDD", "SSD"; NVMe: "NVMe". Empty when unknown.
	FormFactor string `json:"form_factor,omitempty"`

	// SMART overall assessment as reported by the controller
	// ("OK", "FAIL", "Suspected", "Warning", "Critical", ...). Empty when
	// the controller exposes no overall status.
	SMARTStatus string `json:"smart_status,omitempty"`

	// Counters that, when non-zero, indicate a degraded drive.
	// Always present in JSON (0 when not applicable) so the UI can
	// distinguish "0" from "not reported".
	ReallocatedSectors int `json:"reallocated_sectors"`
	PendingSectors     int `json:"pending_sectors"`
	Uncorrectable      int `json:"uncorrectable"`

	// NVMe-specific fields (populated when the device is NVMe and the
	// controller exposes health via esxcli). Always present in JSON
	// (0 for SATA/SAS) so the UI can distinguish "0" from "not reported".
	MediaErrors    int `json:"media_errors"`
	PercentageUsed int `json:"percentage_used"`

	// Temperature in degrees Celsius, when reported.
	Temperature int `json:"temperature,omitempty"`
	TempOk      bool `json:"temp_ok,omitempty"`

	// True when the device refused SMART / health (typical of some NVMe
	// controllers under ESXi): the UI shows what is available, never errors.
	Unsupported bool `json:"unsupported,omitempty"`

	// Last error from SSH, when the whole probe failed.
	Error string `json:"error,omitempty"`
}

// DatastoreHealth is the aggregate health of every device backing a datastore.
type DatastoreHealth struct {
	Datastore string       `json:"datastore"`
	HostID    string       `json:"host_id"`
	Devices   []DiskHealth `json:"devices"`
}

// Alarm is a single active condition worth surfacing at the root summary.
//
// Right now alarms come from disk health (reallocated sectors, SMART not OK,
// over-temperature); the structure is generic so other sources can be added
// later without changing the UI.
type Alarm struct {
	// Kind identifies the source: "disk" so far.
	Kind string `json:"kind"`
	// "danger" (red, compromising) or "warning".
	Severity string `json:"severity"`
	HostID   string `json:"host_id"`
	HostName string `json:"host_name"`
	// Object the alarm is about: a datastore for "disk" alarms.
	Target string `json:"target"`
	// Short, human-readable reason, e.g. "2 reallocated sectors on naa.5000..."
	Message string `json:"message"`
}

// DiskHealth thresholds, in degrees Celsius. A temperature at or above
// tempDanger is shown in red (alongside reallocated sectors and a
// non-OK SMART status); between tempWarn and tempDanger it is yellow.
const (
	DiskTempWarn   = 50
	DiskTempDanger = 65
)

// IsCompromising reports whether the disk health indicates a drive that
// may already be losing data: reallocated/pending/uncorrectable sectors,
// media errors (NVMe), or an explicit non-OK SMART status.
func (h DiskHealth) IsCompromising() bool {
	if h.SMARTStatus != "" && !strings.EqualFold(h.SMARTStatus, "OK") {
		return true
	}
	return h.ReallocatedSectors > 0 || h.PendingSectors > 0 || h.Uncorrectable > 0 || h.MediaErrors > 0
}

// IsOverTemperature reports a temperature at or above the danger threshold.
func (h DiskHealth) IsOverTemperature() bool {
	return h.Temperature > 0 && h.Temperature >= DiskTempDanger
}

// IsWarning reports a non-critical but notable condition (e.g. a high
// temperature below the danger threshold).
func (h DiskHealth) IsWarning() bool {
	if h.IsCompromising() {
		return false
	}
	return h.Temperature > 0 && h.Temperature >= DiskTempWarn && h.Temperature < DiskTempDanger
}

// Severity returns "danger" for compromising/over-temperature conditions,
// "warning" for the warning band, "" otherwise.
func (h DiskHealth) Severity() string {
	if h.IsCompromising() || h.IsOverTemperature() {
		return "danger"
	}
	if h.IsWarning() {
		return "warning"
	}
	return ""
}

// ShortDescription is a one-line, human-readable summary used in alarms.
func (h DiskHealth) ShortDescription() string {
	name := h.DisplayName
	if name == "" {
		name = h.DeviceID
	}
	var parts []string
	if h.SMARTStatus != "" && !strings.EqualFold(h.SMARTStatus, "OK") {
		parts = append(parts, "SMART "+h.SMARTStatus)
	}
	if h.ReallocatedSectors > 0 {
		parts = append(parts, fmt.Sprintf("%d reallocated sectors", h.ReallocatedSectors))
	}
	if h.PendingSectors > 0 {
		parts = append(parts, fmt.Sprintf("%d pending sectors", h.PendingSectors))
	}
	if h.Uncorrectable > 0 {
		parts = append(parts, fmt.Sprintf("%d uncorrectable sectors", h.Uncorrectable))
	}
	if h.MediaErrors > 0 {
		parts = append(parts, fmt.Sprintf("%d media errors", h.MediaErrors))
	}
	if h.PercentageUsed > 0 {
		parts = append(parts, fmt.Sprintf("%d%% endurance used", h.PercentageUsed))
	}
	if h.IsOverTemperature() {
		parts = append(parts, fmt.Sprintf("temperature %dC", h.Temperature))
	} else if h.IsWarning() && h.Temperature > 0 {
		parts = append(parts, fmt.Sprintf("temperature %dC", h.Temperature))
	}
	if len(parts) == 0 {
		return name
	}
	return name + ": " + strings.Join(parts, ", ")
}

type Network struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type GuestOS struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Requests

type DiskSpec struct {
	SizeGB    int64  `json:"size_gb"`
	Thin      bool   `json:"thin"`
	Datastore string `json:"datastore,omitempty"` // add-disk only
}

type NICSpec struct {
	Network     string `json:"network"`
	AdapterType string `json:"adapter_type,omitempty"` // e1000 | e1000e | vmxnet3
}

type DiskResize struct {
	Key    int32 `json:"key"`
	SizeGB int64 `json:"size_gb"`
}

// CloudInitNetwork configures the new VM's primary NIC without an OS interface name.
type CloudInitNetwork struct {
	Address    string   `json:"address"` // IPv4 CIDR
	Gateway    string   `json:"gateway,omitempty"`
	DNSServers []string `json:"dns_servers,omitempty"`
}

type CloudInitConfig struct {
	Network           *CloudInitNetwork `json:"network,omitempty"`
	Hostname          string            `json:"hostname,omitempty"`
	RootPassword      string            `json:"root_password,omitempty"`
	SSHAuthorizedKeys []string          `json:"ssh_authorized_keys,omitempty"`
	Users             []CloudInitUser   `json:"users,omitempty"`
	Packages          []string          `json:"packages,omitempty"`
	RunCmd            []string          `json:"runcmd,omitempty"`
	WriteFiles        []CloudInitFile   `json:"write_files,omitempty"`
	NetworkConfig     string            `json:"network_config,omitempty"`
	UserData          string            `json:"user_data,omitempty"` // overrides generated user-data, not networking or metadata
}

type CloudInitUser struct {
	Name              string   `json:"name"`
	Sudo              string   `json:"sudo,omitempty"`
	SSHAuthorizedKeys []string `json:"ssh_authorized_keys,omitempty"`
	Shell             string   `json:"shell,omitempty"`
	PlainPassword     string   `json:"password,omitempty"`
}

type CloudInitFile struct {
	Path        string `json:"path"`
	Content     string `json:"content"`
	Permissions string `json:"permissions,omitempty"`
	Owner       string `json:"owner,omitempty"`
}

type CreateVMRequest struct {
	NetworkMAC     string           `json:"-"` // internally allocated for cloud-init NIC matching
	Name           string           `json:"name"`
	GuestID        string           `json:"guest_id"`
	NumCPUs        int              `json:"num_cpus"`
	CoresPerSocket int              `json:"cores_per_socket,omitempty"`
	MemoryMB       int64            `json:"memory_mb"`
	Datastore      string           `json:"datastore"`
	Disks          []DiskSpec       `json:"disks"`
	Network        string           `json:"network,omitempty"`
	AdapterType    string           `json:"adapter_type,omitempty"`
	SCSIController string           `json:"scsi_controller,omitempty"`
	Annotation     string           `json:"annotation,omitempty"`
	ISOPath        string           `json:"iso_path,omitempty"`
	BootFromCDROM  bool             `json:"boot_from_cdrom,omitempty"`
	VNCEnabled     bool             `json:"vnc_enabled,omitempty"`
	VNCPort        int              `json:"vnc_port,omitempty"`
	VNCPassword    string           `json:"vnc_password,omitempty"`
	NestedHV       bool             `json:"nested_hv,omitempty"`
	CloudImage     string           `json:"cloud_image,omitempty"` // cloud image vmdk path on datastore (e.g., "cloud-images/rocky-9.vmdk")
	CloudInit      *CloudInitConfig `json:"cloud_init,omitempty"`
	PowerOn        bool             `json:"power_on"`
}

type UpdateVMRequest struct {
	NumCPUs         *int         `json:"num_cpus,omitempty"`
	CoresPerSocket  *int         `json:"cores_per_socket,omitempty"`
	MemoryMB        *int64       `json:"memory_mb,omitempty"`
	Annotation      *string      `json:"annotation,omitempty"`
	ResizeDisks     []DiskResize `json:"resize_disks,omitempty"`
	AddDisks        []DiskSpec   `json:"add_disks,omitempty"`
	RemoveDiskKeys  []int32      `json:"remove_disk_keys,omitempty"`
	DeleteDiskFiles bool         `json:"delete_disk_files"`
	AddNICs         []NICSpec    `json:"add_nics,omitempty"`
	RemoveNICKeys   []int32      `json:"remove_nic_keys,omitempty"`
	ISOPath         *string      `json:"iso_path,omitempty"` // "" = detach

	// VM options
	Name           *string `json:"name,omitempty"` // display name (files are not renamed)
	GuestID        *string `json:"guest_id,omitempty"`
	Firmware       *string `json:"firmware,omitempty"` // bios | efi
	BootDelayMs    *int64  `json:"boot_delay_ms,omitempty"`
	EnterBIOSSetup *bool   `json:"enter_bios_setup,omitempty"` // force firmware setup on next boot
	CPUHotAdd      *bool   `json:"cpu_hot_add,omitempty"`
	MemoryHotAdd   *bool   `json:"memory_hot_add,omitempty"`
	NestedHV       *bool   `json:"nested_hv,omitempty"`
	USBController  *string `json:"usb_controller,omitempty"` // "" (none) | usb2 | usb3

	// devices
	Template *bool `json:"template,omitempty"` // convert to / from template

	UpdateNICs      []NICUpdate   `json:"update_nics,omitempty"`
	AddCdroms       []CdromSpec   `json:"add_cdroms,omitempty"`
	UpdateCdroms    []CdromUpdate `json:"update_cdroms,omitempty"`
	RemoveCdromKeys []int32       `json:"remove_cdrom_keys,omitempty"`
}

// NICUpdate changes an existing network adapter; nil fields are left alone.
type NICUpdate struct {
	Key            int32   `json:"key"`
	Network        *string `json:"network,omitempty"`
	StartConnected *bool   `json:"start_connected,omitempty"`
}

// CdromSpec is a new CD/DVD drive. An empty ISOPath means client device (no media).
type CdromSpec struct {
	ISOPath string `json:"iso_path"`
}

// CdromUpdate sets the media of an existing CD/DVD drive ("" = no media).
type CdromUpdate struct {
	Key     int32  `json:"key"`
	ISOPath string `json:"iso_path"`
}

type CreateSnapshotRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Memory      bool   `json:"memory"`
	Quiesce     bool   `json:"quiesce"`
}

type PowerRequest struct {
	Action string `json:"action"` // on | off | reset | suspend | shutdown_guest | reboot_guest
}

// ---- VM migration between hosts ----

// Migration modes.
const (
	MigrateCold      = "cold"       // VM is powered off: copy and register
	MigrateSuspend   = "suspend"    // suspend, copy, resume on the target (same CPU only)
	MigrateStopStart = "stop_start" // shut down, copy, power on on the target
)

type MigrateRequest struct {
	TargetHostID    string `json:"target_host_id"`
	TargetDatastore string `json:"target_datastore"`
	Mode            string `json:"mode"`          // required by /migrate, ignored by /migrate/check
	DeleteSource    bool   `json:"delete_source"` // delete the source files once the target runs
	ReclaimThin     bool   `json:"reclaim_thin"`  // re-thin the copied disks (the copy fully allocates them)
	Precopy         bool   `json:"precopy"`       // running VM: snapshot it and copy the disks before stopping it (shorter downtime)
}

// RelocateRequest moves a VM between two datastores of the same host.
type RelocateRequest struct {
	TargetDatastore string `json:"target_datastore"`
	Mode            string `json:"mode"`          // cold | suspend | stop_start
	DeleteSource    bool   `json:"delete_source"` // delete the source files once the target runs
	ReclaimThin     bool   `json:"reclaim_thin"`  // re-thin thin disks after the copy
	Precopy         bool   `json:"precopy"`       // not implemented yet, kept for API symmetry
}

// MigrationCheck is the outcome of the compatibility checks for a migration.
type MigrationCheck struct {
	PowerState      string   `json:"power_state"`
	SourceCPU       string   `json:"source_cpu"`
	TargetCPU       string   `json:"target_cpu"`
	CPUCompatible   bool     `json:"cpu_compatible"`
	Shared          bool     `json:"shared"` // target datastore is the very volume the VM is on: nothing to copy
	RecommendedMode string   `json:"recommended_mode"`
	SizeBytes       int64    `json:"size_bytes"`     // bytes to copy
	ProvisionedGB   int64    `json:"provisioned_gb"` // space the disks take until re-thinned
	TargetFreeBytes int64    `json:"target_free_bytes"`
	Blockers        []string `json:"blockers"`
	Warnings        []string `json:"warnings"`
}

// JobStatus is the progress of a long operation running in the background
// (migration, clone, backup), polled by the UI.
type JobStatus struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"` // migrate | clone | backup
	VMName       string     `json:"vm_name"`
	SourceHostID string     `json:"source_host_id"`
	TargetHostID string     `json:"target_host_id"`
	Status       string     `json:"status"` // running | ok | error
	Phase        string     `json:"phase"`
	BytesTotal   int64      `json:"bytes_total"`
	BytesDone    int64      `json:"bytes_done"`
	NewVMID      string     `json:"new_vm_id,omitempty"`
	Error        string     `json:"error,omitempty"`
	Warnings     []string   `json:"warnings"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

// CloneRequest creates a copy of a powered-off VM on the same host.
type CloneRequest struct {
	Name      string `json:"name"`
	Datastore string `json:"datastore"` // empty = datastore of the source
	PowerOn   bool   `json:"power_on"`
	Template  bool   `json:"template"` // mark the copy as a template
}

// Task is an entry of Recent Tasks: one modifying operation, its initiator and outcome.
type Task struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Target     string     `json:"target"`
	HostID     string     `json:"host_id,omitempty"`
	Initiator  string     `json:"initiator"`
	Status     string     `json:"status"` // running | ok | error
	Details    string     `json:"details"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// ---- monitoring ----

// PerfSeries is one metric over time; Values[i] belongs to PerfData.Times[i].
type PerfSeries struct {
	Key    string    `json:"key"` // cpu | memory | disk | network
	Label  string    `json:"label"`
	Unit   string    `json:"unit"` // % | KBps
	Values []float64 `json:"values"`
}

// PerfData is the real-time performance of a host or VM (last hour, 20 s samples).
type PerfData struct {
	IntervalSeconds int          `json:"interval_seconds"`
	Times           []time.Time  `json:"times"`
	Series          []PerfSeries `json:"series"`
}

// Event is an entry of the host's own event log.
type Event struct {
	Time     time.Time `json:"time"`
	Category string    `json:"category"` // info | warning | error | user
	Message  string    `json:"message"`
	User     string    `json:"user,omitempty"`
	Target   string    `json:"target,omitempty"` // VM the event is about, if any
}

// ---- host configuration ----

type HostService struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Running bool   `json:"running"`
	Policy  string `json:"policy"` // on (start with host) | off | automatic
}

type HostServiceRequest struct {
	Action string `json:"action,omitempty"` // start | stop | restart
	Policy string `json:"policy,omitempty"` // on | off
}

type HostNic struct {
	Device  string `json:"device"`
	MAC     string `json:"mac"`
	SpeedMb int32  `json:"speed_mb"` // 0 = link down
	Driver  string `json:"driver,omitempty"`
}

type VSwitch struct {
	Name       string   `json:"name"`
	MTU        int32    `json:"mtu"`
	Uplinks    []string `json:"uplinks"`
	PortGroups []string `json:"port_groups"`
}

type PortGroup struct {
	Name    string `json:"name"`
	VSwitch string `json:"vswitch"`
	VLAN    int32  `json:"vlan"`
	// Management is true when a VMkernel interface (host management, storage)
	// uses the port group: changing it could cut the host off.
	Management bool `json:"management"`
	VMs        int  `json:"vms"`
}

type HostNetwork struct {
	VSwitches  []VSwitch   `json:"vswitches"`
	PortGroups []PortGroup `json:"port_groups"`
	Nics       []HostNic   `json:"nics"`
}

type PortGroupRequest struct {
	Name    string `json:"name"`
	VSwitch string `json:"vswitch"`
	VLAN    int32  `json:"vlan"`
}

type VSwitchRequest struct {
	Name    string   `json:"name"`
	Uplinks []string `json:"uplinks"`
}

type HostTime struct {
	Now        string   `json:"now"` // host clock, RFC 3339 UTC
	NTPEnabled bool     `json:"ntp_enabled"`
	NTPServers []string `json:"ntp_servers"`
}

type HostTimeRequest struct {
	NTPEnabled bool     `json:"ntp_enabled"`
	NTPServers []string `json:"ntp_servers"`
}

// ---- backups and schedules ----

// BackupRequest copies a VM (disks consolidated, configuration) into the
// backup folder of a datastore. With TargetHostID the copy is then moved to a
// datastore of another host.
type BackupRequest struct {
	Datastore       string `json:"datastore"`                  // on the VM's host
	TargetHostID    string `json:"target_host_id,omitempty"`   // optional: ship the backup to this host
	TargetDatastore string `json:"target_datastore,omitempty"` // on the target host
	Keep            int    `json:"keep"`                       // backups of this VM to keep at the destination (0 = all)
}

// Backup is one backup of a VM found on a datastore.
type Backup struct {
	HostID    string    `json:"host_id"`
	Datastore string    `json:"datastore"`
	VMName    string    `json:"vm_name"`
	Stamp     string    `json:"stamp"` // folder name: YYYYMMDD-HHMMSS (UTC)
	Created   time.Time `json:"created"`
	SizeBytes int64     `json:"size_bytes"`
}

// RestoreRequest registers a backup as a new VM (new identity) on the host that holds it.
type RestoreRequest struct {
	Name      string `json:"name"`
	Datastore string `json:"datastore"`
	PowerOn   bool   `json:"power_on"`
}

// Schedule kinds.
const (
	ScheduleSnapshot = "snapshot"
	ScheduleBackup   = "backup"
)

// Schedule runs a snapshot or a backup of one VM at fixed times.
type Schedule struct {
	ID     string `json:"id"`
	HostID string `json:"host_id"`
	VMID   string `json:"vm_id"`
	VMName string `json:"vm_name"`
	Kind   string `json:"kind"` // snapshot | backup

	Frequency string `json:"frequency"` // hourly | daily | weekly
	Hour      int    `json:"hour"`      // 0-23 (daily, weekly)
	Minute    int    `json:"minute"`    // 0-59
	Weekday   int    `json:"weekday"`   // 0 = Sunday (weekly)
	Timezone  string `json:"timezone"`  // IANA name the times are meant in

	Keep   int            `json:"keep"`             // snapshots / backups to keep (0 = all)
	Backup *BackupRequest `json:"backup,omitempty"` // destination, for kind backup

	Enabled    bool       `json:"enabled"`
	NextRun    time.Time  `json:"next_run"`
	LastRun    *time.Time `json:"last_run,omitempty"`
	LastStatus string     `json:"last_status,omitempty"` // ok | error
	LastError  string     `json:"last_error,omitempty"`
}
