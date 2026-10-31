package esxi

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"hostwarden/internal/model"
)

// All mutating operations go through SSH (vim-cmd / vmkfstools / VMX editing).

var ErrSSHDisabled = fmt.Errorf("ssh is not enabled for this host; Hostwarden makes every change over SSH, so writes require ssh_enabled=true")

func (c *Client) sshRun(ctx context.Context, cmd string) (string, error) {
	if c.ssh == nil {
		return "", ErrSSHDisabled
	}
	return c.ssh.Run(ctx, cmd)
}

func (c *Client) sshPut(ctx context.Context, cmd string, r io.Reader) error {
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	_, err := c.ssh.RunWithStdin(ctx, cmd, r)
	return err
}

// shq single-quotes a string for safe shell usage.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ensureVNCFirewallSSH adds a custom firewall ruleset for VNC (ports 5900-5999)
// on the ESXi host if it doesn't already exist. Best-effort: errors are ignored.
func ensureVNCFirewallSSH(ctx context.Context, c *Client) {
	out, _ := c.sshRun(ctx, "esxcli network firewall ruleset list 2>/dev/null | grep -q remoteDisplayVNC && echo exists || echo missing")
	if strings.Contains(out, "exists") {
		return
	}
	// create custom ruleset in a separate file (service.xml is read-only on ESXi)
	script := "cat > /etc/vmware/firewall/vnc.xml <<'FWEOF'\n" +
		"<ConfigRoot>\n" +
		"  <service>\n" +
		"    <id>remoteDisplayVNC</id>\n" +
		"    <rule id=\"0000\">\n" +
		"      <direction>inbound</direction>\n" +
		"      <protocol>tcp</protocol>\n" +
		"      <porttype>dst</porttype>\n" +
		"      <port>5900-5999</port>\n" +
		"    </rule>\n" +
		"    <enabled>true</enabled>\n" +
		"    <required>false</required>\n" +
		"  </service>\n" +
		"</ConfigRoot>\n" +
		"FWEOF\n" +
		"esxcli network firewall refresh 2>/dev/null; true"
	c.sshRun(ctx, script)
}

func parseFirstInt(s string) (int, error) {
	re := regexp.MustCompile(`\d+`)
	m := re.FindString(strings.TrimSpace(s))
	if m == "" {
		return 0, fmt.Errorf("no numeric id in output: %q", strings.TrimSpace(s))
	}
	return strconv.Atoi(m)
}

// dsPathToFS converts "[datastore] dir/file" to "/vmfs/volumes/datastore/dir/file".
func dsPathToFS(p string) string {
	if !strings.HasPrefix(p, "[") {
		return p
	}
	end := strings.Index(p, "]")
	if end < 0 {
		return p
	}
	ds := p[1:end]
	rest := strings.TrimSpace(p[end+1:])
	return "/vmfs/volumes/" + ds + "/" + strings.TrimPrefix(rest, "/")
}

func isoToFSPath(p string) string { return dsPathToFS(p) }

func vmxEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

func scsiVirtualDev(kind string) string {
	switch kind {
	case "pvscsi":
		return "pvscsi"
	case "lsilogic":
		return "lsilogic"
	case "buslogic":
		return "buslogic"
	default:
		return "lsisas1068" // lsilogic-sas
	}
}

// guestOSOverrides maps vim GuestOsIdentifier names to legacy VMX guestOS
// names where the mechanical rule would be wrong.
var guestOSOverrides = map[string]string{
	"windows10_64Guest":          "windows9-64", // Win10 family
	"windows11_64Guest":          "windows9-64", // Win11 family
	"windows2019srvNext_64Guest": "windows2019srv-64",
}

// guestOSToVMX maps vim GuestOsIdentifier names ("ubuntu64Guest") to the
// legacy VMX guestOS names ("ubuntu-64") that hand-written VMX files require.
func guestOSToVMX(id string) string {
	if id == "" {
		return "other-64"
	}
	if v, ok := guestOSOverrides[id]; ok {
		return v
	}
	// already a legacy name (e.g. "ubuntu-64") or non-standard value
	if strings.HasSuffix(id, "-64") || !strings.HasSuffix(id, "Guest") {
		return id
	}
	base := strings.TrimSuffix(id, "Guest")
	suffix := ""
	if strings.HasSuffix(base, "_64") {
		base = strings.TrimSuffix(base, "_64")
		suffix = "-64"
	} else if strings.HasSuffix(base, "64") {
		base = strings.TrimSuffix(base, "64")
		suffix = "-64"
	}
	if base == "" {
		return id
	}
	return strings.ToLower(base) + suffix
}

// ---- CREATE ----

func (c *Client) CreateVM(ctx context.Context, req *model.CreateVMRequest) (*model.VM, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	if c.ssh == nil {
		return nil, ErrSSHDisabled
	}
	profile, _, err := c.creationConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("VM compatibility: %w", err)
	}
	if req.AdapterType == "" {
		req.AdapterType = "e1000"
	}
	if req.SCSIController == "" {
		req.SCSIController = "lsilogic-sas"
	}
	if len(req.Disks) == 0 {
		req.Disks = []model.DiskSpec{{SizeGB: 16, Thin: true}}
	}

	dir := fmt.Sprintf("/vmfs/volumes/%s/%s", req.Datastore, req.Name)
	vmxPath := dir + "/" + req.Name + ".vmx"
	// fail fast with a clear error if a VM configuration already exists at the target path
	if out, err := c.sshRun(ctx, "test -f "+shq(vmxPath)); err != nil {
		if !strings.Contains(err.Error(), "status 1") {
			return nil, fmt.Errorf("stat %s: %w (output: %s)", vmxPath, err, out)
		}
	} else {
		return nil, fmt.Errorf("a VM configuration already exists at %s (delete it or choose another name)", vmxPath)
	}
	if out, err := c.sshRun(ctx, "mkdir -p "+shq(dir)); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w (output: %s)", dir, err, out)
	}

	if req.CloudImage != "" {
		// Cloud image flow: clone the cloud image vmdk as the boot disk.
		srcVmdk := "/vmfs/volumes/" + req.Datastore + "/" + strings.TrimPrefix(req.CloudImage, "/")
		dstVmdk := dir + "/" + req.Name + ".vmdk"
		cmd := fmt.Sprintf("vmkfstools -i %s -d thin %s", shq(srcVmdk), shq(dstVmdk))
		if out, err := c.sshRun(ctx, cmd); err != nil {
			return nil, fmt.Errorf("vmkfstools clone cloud image: %w (output: %s)", err, out)
		}
		// The boot disk is the clone: the VMX needs exactly one disk entry for it.
		if len(req.Disks) == 0 {
			req.Disks = []model.DiskSpec{{Thin: true}}
		}
		req.Disks = req.Disks[:1]
		// Grow the clone to the requested size. A size not larger than the image
		// makes vmkfstools fail, which is fine: the disk keeps the image size
		// (cloud-init grows the root filesystem to whatever the disk offers).
		if req.Disks[0].SizeGB > 0 {
			_, _ = c.sshRun(ctx, fmt.Sprintf("vmkfstools -X %dG %s", req.Disks[0].SizeGB, shq(dstVmdk)))
		}
		// Build + upload cloud-init seed ISO
		if req.CloudInit != nil {
			seedPath, err := BuildSeedISO(req.CloudInit, req.Name)
			if err != nil {
				return nil, fmt.Errorf("seed iso: %w", err)
			}
			defer os.Remove(seedPath)
			if err := c.sshPut(ctx, "cat > "+shq(dir+"/seed.iso"), mustOpen(seedPath)); err != nil {
				return nil, fmt.Errorf("upload seed iso: %w", err)
			}
			// Attach seed ISO as CDROM (buildVMX picks up ISOPath)
			req.ISOPath = fmt.Sprintf("[%s] %s/seed.iso", req.Datastore, req.Name)
			req.BootFromCDROM = false
		}
	} else {
		for i, d := range req.Disks {
			fname := fmt.Sprintf("%s.vmdk", req.Name)
			if i > 0 {
				fname = fmt.Sprintf("%s_%d.vmdk", req.Name, i)
			}
			dtype := "zeroedthick"
			if d.Thin {
				dtype = "thin"
			}
			cmd := fmt.Sprintf("vmkfstools -c %dG -d %s %s", d.SizeGB, dtype, shq(dir+"/"+fname))
			if out, err := c.sshRun(ctx, cmd); err != nil {
				return nil, fmt.Errorf("vmkfstools create disk: %w (output: %s)", err, out)
			}
		}
	}

	vmx := buildVMX(req, profile)
	cmd := fmt.Sprintf("cat > %s <<'FCVMXEOF'\n%sFCVMXEOF\n", shq(vmxPath), vmx)
	if out, err := c.sshRun(ctx, cmd); err != nil {
		return nil, fmt.Errorf("write vmx: %w (output: %s)", err, out)
	}

	out, err := c.sshRun(ctx, "vim-cmd solo/registervm "+shq(vmxPath))
	if err != nil {
		_, _ = c.sshRun(ctx, "rm -rf "+shq(dir))
		return nil, fmt.Errorf("registervm: %w (output: %s)", err, out)
	}
	id, err := parseFirstInt(out)
	if err != nil {
		return nil, fmt.Errorf("registervm: %w (output: %s)", err, out)
	}
	vmid := strconv.Itoa(id)

	if req.VNCEnabled && req.VNCPort > 0 {
		// open firewall for VNC ports 5900-5999 (custom ruleset, best-effort)
		ensureVNCFirewallSSH(ctx, c)
	}

	if req.PowerOn {
		if out, err := c.sshRun(ctx, "vim-cmd vmsvc/power.on "+shq(vmid)); err != nil {
			return nil, fmt.Errorf("vm created but power on failed: %w (output: %s)", err, out)
		}
	}
	// the freshly registered VM may not be immediately readable; retry briefly
	var vm *model.VM
	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-after(500 * time.Millisecond):
		}
		if v, err := c.GetVM(ctx, vmid); err == nil {
			vm = v
			break
		}
	}
	if vm == nil {
		return nil, fmt.Errorf("vm %s registered but not readable via API", vmid)
	}
	return vm, nil
}

func buildVMX(req *model.CreateVMRequest, profile vmCreationProfile) string {
	var b strings.Builder
	w := func(k, v string) { fmt.Fprintf(&b, "%s = \"%s\"\n", k, v) }
	b.WriteString(".encoding = \"UTF-8\"\n")
	w("config.version", "8")
	w("virtualHW.version", strconv.Itoa(int(profile)))
	w("displayName", req.Name)
	w("guestOS", guestOSToVMX(req.GuestID))
	w("memSize", strconv.FormatInt(req.MemoryMB, 10))
	w("numvcpus", strconv.Itoa(req.NumCPUs))
	if req.CoresPerSocket > 0 {
		w("cpuid.coresPerSocket", strconv.Itoa(req.CoresPerSocket))
	}
	if req.NestedHV {
		w("vhv.enable", "TRUE")
	}
	if profile == upstreamVMProfile {
		// Keep the upstream bridge layout unchanged for vmx-21.
		for i := 0; i < 2; i++ {
			w(fmt.Sprintf("pciBridge%d.present", i), "TRUE")
			w(fmt.Sprintf("pciBridge%d.virtualDev", i), "pciRootBridge")
			w(fmt.Sprintf("pciBridge%d.functions", i), "1")
		}
	} else {
		// Older ESXi profiles require the legacy PCI bridge and PCIe root ports.
		w("pciBridge0.present", "TRUE")
		for i := 4; i <= 7; i++ {
			w(fmt.Sprintf("pciBridge%d.present", i), "TRUE")
			w(fmt.Sprintf("pciBridge%d.virtualDev", i), "pcieRootPort")
			w(fmt.Sprintf("pciBridge%d.functions", i), "8")
		}
	}
	if req.CloudImage != "" {
		w("firmware", "efi")
	}
	w("svga.guestBackedPrimaryAware", "TRUE")
	w("scsi0.present", "TRUE")
	w("scsi0.virtualDev", scsiVirtualDev(req.SCSIController))
	for i := range req.Disks {
		fname := fmt.Sprintf("%s.vmdk", req.Name)
		if i > 0 {
			fname = fmt.Sprintf("%s_%d.vmdk", req.Name, i)
		}
		w(fmt.Sprintf("scsi0:%d.present", i), "TRUE")
		w(fmt.Sprintf("scsi0:%d.fileName", i), fname)
	}
	if req.Network != "" {
		w("ethernet0.present", "TRUE")
		w("ethernet0.virtualDev", req.AdapterType)
		w("ethernet0.networkName", req.Network)
		w("ethernet0.addressType", "generated")
	}
	if req.ISOPath != "" {
		w("ide1:0.present", "TRUE")
		w("ide1:0.deviceType", "cdrom-image")
		w("ide1:0.fileName", isoToFSPath(req.ISOPath))
		w("ide1:0.startConnected", "TRUE")
	}
	if req.BootFromCDROM {
		w("bios.bootorder", "cdrom,hdd")
		w("bios.bootRetryDelay", "10000")
	}
	if req.VNCEnabled {
		w("RemoteDisplay.vnc.enabled", "TRUE")
		if req.VNCPort > 0 {
			w("RemoteDisplay.vnc.port", strconv.Itoa(req.VNCPort))
		}
		if req.VNCPassword != "" {
			w("RemoteDisplay.vnc.password", vmxEscape(req.VNCPassword))
		}
	}
	if req.Annotation != "" {
		w("annotation", vmxEscape(req.Annotation))
	}
	w("floppy0.present", "FALSE")
	return b.String()
}

// after allows stubbing in tests.
var after = func(d time.Duration) <-chan time.Time { return time.After(d) }

// ---- VMX helpers ----

type vmxDoc struct {
	lines []string // raw lines without trailing newline
}

func parseVMX(content string) *vmxDoc {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return &vmxDoc{lines: lines}
}

func (d *vmxDoc) String() string { return strings.Join(d.lines, "\n") + "\n" }

func vmxKeyOf(line string) string {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return ""
	}
	if i := strings.Index(t, "="); i > 0 {
		return strings.TrimSpace(t[:i])
	}
	return ""
}

func (d *vmxDoc) set(key, value string) {
	for i, l := range d.lines {
		if vmxKeyOf(l) == key {
			d.lines[i] = fmt.Sprintf("%s = \"%s\"", key, value)
			return
		}
	}
	d.lines = append(d.lines, fmt.Sprintf("%s = \"%s\"", key, value))
}

func (d *vmxDoc) get(key string) (string, bool) {
	for _, l := range d.lines {
		if vmxKeyOf(l) == key {
			t := strings.TrimSpace(l)
			i := strings.Index(t, "=")
			v := strings.TrimSpace(t[i+1:])
			return strings.Trim(v, `"`), true
		}
	}
	return "", false
}

// removePrefix deletes every line whose key equals key or starts with prefix + ".".
func (d *vmxDoc) removePrefix(prefix string) {
	out := d.lines[:0]
	for _, l := range d.lines {
		k := vmxKeyOf(l)
		if k == prefix || strings.HasPrefix(k, prefix+".") {
			continue
		}
		out = append(out, l)
	}
	d.lines = out
}

func (d *vmxDoc) keysWithPrefix(prefix string) []string {
	var keys []string
	for _, l := range d.lines {
		k := vmxKeyOf(l)
		if k == "" || !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		if rest == "" || rest[0] == '.' || rest[0] == ':' || (rest[0] >= '0' && rest[0] <= '9') {
			keys = append(keys, k)
		}
	}
	return keys
}

// ---- UPDATE ----

type deviceAddr struct {
	key        int32
	ctrl       string // VMX controller prefix: scsi | ide | sata | nvme ("" = not on a disk controller)
	bus        int32
	unit       int32
	mac        string
	file       string // "[ds] dir/file.vmdk"
	capacityKB int64
}

// vmxPrefix is the VMX key prefix of a disk or CD/DVD drive ("scsi0:1", "ide1:0").
func (a deviceAddr) vmxPrefix() string {
	return fmt.Sprintf("%s%d:%d", a.ctrl, a.bus, a.unit)
}

// deviceAddrs maps ESXi device keys to VMX addresses (controller bus:unit / mac)
// so update requests addressed by device key can be translated to VMX edits.
func deviceAddrs(m *mo.VirtualMachine) map[int32]deviceAddr {
	type ctrlInfo struct {
		kind string
		bus  int32
	}
	out := map[int32]deviceAddr{}
	ctrls := map[int32]ctrlInfo{} // controller key -> VMX prefix and bus number
	for _, d := range m.Config.Hardware.Device {
		key := d.GetVirtualDevice().Key
		switch ct := d.(type) {
		case types.BaseVirtualSCSIController:
			ctrls[key] = ctrlInfo{"scsi", ct.GetVirtualSCSIController().BusNumber}
		case *types.VirtualIDEController:
			ctrls[key] = ctrlInfo{"ide", ct.BusNumber}
		case types.BaseVirtualSATAController:
			ctrls[key] = ctrlInfo{"sata", ct.GetVirtualSATAController().BusNumber}
		case *types.VirtualNVMEController:
			ctrls[key] = ctrlInfo{"nvme", ct.BusNumber}
		}
	}
	for _, d := range m.Config.Hardware.Device {
		vd := d.GetVirtualDevice()
		addr := deviceAddr{key: vd.Key}
		if vd.ControllerKey != 0 {
			if ci, ok := ctrls[vd.ControllerKey]; ok {
				addr.ctrl = ci.kind
				addr.bus = ci.bus
				addr.unit = -1
				if vd.UnitNumber != nil {
					addr.unit = *vd.UnitNumber
				}
			}
		}
		switch dev := d.(type) {
		case *types.VirtualDisk:
			addr.capacityKB = dev.CapacityInKB
			if b, ok := dev.Backing.(*types.VirtualDiskFlatVer2BackingInfo); ok {
				addr.file = b.FileName
			}
		case types.BaseVirtualEthernetCard:
			addr.mac = dev.GetVirtualEthernetCard().MacAddress
		}
		out[vd.Key] = addr
	}
	return out
}

func (c *Client) UpdateVM(ctx context.Context, id string, req *model.UpdateVMRequest) (*model.VM, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	if c.ssh == nil {
		return nil, ErrSSHDisabled
	}
	m, err := c.retrieveVM(ctx, id, vmPropsDetail)
	if err != nil {
		return nil, err
	}

	vmxFS := dsPathToFS(m.Config.Files.VmPathName)
	vmxDir := vmxFS[:strings.LastIndex(vmxFS, "/")]
	poweredOn := m.Summary.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn

	content, err := c.sshRun(ctx, "cat "+shq(vmxFS))
	if err != nil {
		return nil, fmt.Errorf("read vmx: %w", err)
	}
	doc := parseVMX(content)
	addrs := deviceAddrs(m)

	needsReload := false
	vmxChanged := false
	sshCmds := []string{} // deferred commands (disk ops)

	// --- simple fields (require power off) ---
	if req.NumCPUs != nil || req.MemoryMB != nil || req.Annotation != nil || req.CoresPerSocket != nil {
		if poweredOn {
			return nil, fmt.Errorf("%w: cpu/memory/annotation changes require the VM to be powered off", ErrInvalidState)
		}
		if req.NumCPUs != nil {
			doc.set("numvcpus", strconv.Itoa(*req.NumCPUs))
		}
		if req.CoresPerSocket != nil {
			if *req.CoresPerSocket > 0 {
				doc.set("cpuid.coresPerSocket", strconv.Itoa(*req.CoresPerSocket))
			} else {
				doc.removePrefix("cpuid.coresPerSocket")
			}
		}
		if req.MemoryMB != nil {
			doc.set("memSize", strconv.FormatInt(*req.MemoryMB, 10))
		}
		if req.Annotation != nil {
			if *req.Annotation == "" {
				doc.removePrefix("annotation")
			} else {
				doc.set("annotation", vmxEscape(*req.Annotation))
			}
		}
		vmxChanged = true
	}

	// --- resize disks (grow only, power off, no snapshots) ---
	if len(req.ResizeDisks) > 0 {
		if poweredOn {
			return nil, fmt.Errorf("%w: disk resize requires the VM to be powered off", ErrInvalidState)
		}
		if m.Snapshot != nil {
			return nil, fmt.Errorf("%w: disk resize requires all snapshots to be removed (vmkfstools cannot extend snapshot delta chains)", ErrInvalidState)
		}
	}
	for _, rd := range req.ResizeDisks {
		addr, ok := addrs[rd.Key]
		if !ok || addr.file == "" {
			return nil, fmt.Errorf("%w: disk with key %d", ErrNotFound, rd.Key)
		}
		path := dsPathToFS(addr.file)
		curGB := addr.capacityKB / (1024 * 1024)
		if curGB > 0 && rd.SizeGB < curGB {
			return nil, fmt.Errorf("cannot shrink disk %d (%dGB -> %dGB): only growing is supported", rd.Key, curGB, rd.SizeGB)
		}
		sshCmds = append(sshCmds, fmt.Sprintf("vmkfstools -X %dG %s", rd.SizeGB, shq(path)))
	}

	// --- add disks ---
	for _, ad := range req.AddDisks {
		if poweredOn {
			return nil, fmt.Errorf("%w: adding disks requires the VM to be powered off", ErrInvalidState)
		}
		bus, unit := freeSCSISlot(doc)
		fname := uniqueDiskName(doc, m.Name)
		dtype := "zeroedthick"
		if ad.Thin {
			dtype = "thin"
		}
		// Default: next to the VMX (relative fileName). On another datastore the
		// disk goes in a folder named like the VM's and is referenced by full path.
		diskPath, vmxRef := vmxDir+"/"+fname, fname
		if ad.Datastore != "" && ad.Datastore != dsFromPath(m.Config.Files.VmPathName) {
			dir := "/vmfs/volumes/" + ad.Datastore + "/" + path.Base(vmxDir)
			sshCmds = append(sshCmds, "mkdir -p "+shq(dir))
			diskPath = dir + "/" + fname
			vmxRef = diskPath
		}
		sshCmds = append(sshCmds, fmt.Sprintf("vmkfstools -c %dG -d %s %s", ad.SizeGB, dtype, shq(diskPath)))
		doc.set(fmt.Sprintf("scsi%d:%d.present", bus, unit), "TRUE")
		doc.set(fmt.Sprintf("scsi%d:%d.fileName", bus, unit), vmxRef)
		vmxChanged = true
	}

	// --- remove disks ---
	if len(req.RemoveDiskKeys) > 0 {
		if poweredOn {
			return nil, fmt.Errorf("%w: removing disks requires the VM to be powered off", ErrInvalidState)
		}
		if m.Snapshot != nil && req.DeleteDiskFiles {
			return nil, fmt.Errorf("%w: deleting disk files requires all snapshots to be removed", ErrInvalidState)
		}
	}
	for _, key := range req.RemoveDiskKeys {
		addr, ok := addrs[key]
		if !ok || addr.file == "" || addr.ctrl == "" {
			return nil, fmt.Errorf("%w: disk with key %d", ErrNotFound, key)
		}
		doc.removePrefix(addr.vmxPrefix())
		if req.DeleteDiskFiles {
			diskFS := dsPathToFS(addr.file)
			sshCmds = append(sshCmds, fmt.Sprintf("vmkfstools -U %s", shq(diskFS)))
			// A disk on another datastore lives in its own folder: drop it once empty.
			if dir := path.Dir(diskFS); dir != vmxDir {
				sshCmds = append(sshCmds, fmt.Sprintf("rmdir %s 2>/dev/null || true", shq(dir)))
			}
		}
		vmxChanged = true
	}

	// --- add nics ---
	for _, an := range req.AddNICs {
		if poweredOn {
			return nil, fmt.Errorf("%w: adding nics requires the VM to be powered off", ErrInvalidState)
		}
		idx := freeEthernetIndex(doc)
		alias := an.AdapterType
		if alias == "" {
			alias = "e1000"
		}
		doc.set(fmt.Sprintf("ethernet%d.present", idx), "TRUE")
		doc.set(fmt.Sprintf("ethernet%d.virtualDev", idx), alias)
		doc.set(fmt.Sprintf("ethernet%d.networkName", idx), an.Network)
		doc.set(fmt.Sprintf("ethernet%d.addressType", idx), "generated")
		vmxChanged = true
	}

	// --- remove nics ---
	for _, key := range req.RemoveNICKeys {
		if poweredOn {
			return nil, fmt.Errorf("%w: removing nics requires the VM to be powered off", ErrInvalidState)
		}
		idx, err := ethernetIndex(doc, addrs, key)
		if err != nil {
			return nil, err
		}
		doc.removePrefix(fmt.Sprintf("ethernet%d", idx))
		vmxChanged = true
	}

	// --- options, nic changes, cd/dvd drives, usb ---
	changed, err := applyVMSettings(doc, req, addrs, poweredOn)
	if err != nil {
		return nil, err
	}
	vmxChanged = vmxChanged || changed

	// --- legacy single cdrom (ide1:0), kept for API clients such as the Terraform provider ---
	if req.ISOPath != nil {
		if poweredOn {
			return nil, fmt.Errorf("%w: iso changes require the VM to be powered off", ErrInvalidState)
		}
		doc.removePrefix("ide1:0")
		if *req.ISOPath != "" {
			doc.set("ide1:0.present", "TRUE")
			doc.set("ide1:0.deviceType", "cdrom-image")
			doc.set("ide1:0.fileName", isoToFSPath(*req.ISOPath))
			doc.set("ide1:0.startConnected", "TRUE")
		}
		vmxChanged = true
	}

	// execute: disk operations first (so a failure leaves the VMX untouched),
	// then write the new VMX, then reload the VM config.
	if len(sshCmds) > 0 || vmxChanged {
		for _, cmd := range sshCmds {
			if out, err := c.sshRun(ctx, cmd); err != nil {
				return nil, fmt.Errorf("%s: %w (output: %s)", cmd, err, out)
			}
		}
		if vmxChanged {
			cmd := fmt.Sprintf("cat > %s <<'FCVMXEOF'\n%sFCVMXEOF\n", shq(vmxFS), doc.String())
			if out, err := c.sshRun(ctx, cmd); err != nil {
				return nil, fmt.Errorf("write vmx: %w (output: %s)", err, out)
			}
		}
		needsReload = true
	}

	if needsReload {
		if out, err := c.sshRun(ctx, "vim-cmd vmsvc/reload "+shq(id)); err != nil {
			return nil, fmt.Errorf("reload vm: %w (output: %s)", err, out)
		}
		// give hostd a moment to apply the new config before reading back
		select {
		case <-ctx.Done():
		case <-after(700 * time.Millisecond):
		}
	}
	return c.GetVM(ctx, id)
}

func freeSCSISlot(doc *vmxDoc) (int32, int32) {
	used := map[int32]map[int32]bool{}
	for _, k := range doc.keysWithPrefix("scsi") {
		parts := strings.SplitN(k, ":", 2)
		if len(parts) != 2 {
			continue
		}
		bus, err := strconv.Atoi(parts[0][4:])
		if err != nil {
			continue
		}
		unitStr := strings.SplitN(parts[1], ".", 2)[0]
		unit, err := strconv.Atoi(unitStr)
		if err != nil {
			continue
		}
		if used[int32(bus)] == nil {
			used[int32(bus)] = map[int32]bool{}
		}
		used[int32(bus)][int32(unit)] = true
	}
	for bus := int32(0); bus < 4; bus++ {
		for unit := int32(0); unit < 16; unit++ {
			if unit == 7 {
				continue
			}
			if !used[bus][unit] {
				return bus, unit
			}
		}
	}
	return 0, 0
}

func freeEthernetIndex(doc *vmxDoc) int {
	max := -1
	for _, k := range doc.keysWithPrefix("ethernet") {
		rest := k[len("ethernet"):]
		n := 0
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if n == 0 {
			continue
		}
		if i, err := strconv.Atoi(rest[:n]); err == nil && i > max {
			max = i
		}
	}
	return max + 1
}

// uniqueDiskName returns "<vmname>_diskK.vmdk" with K not colliding with any
// fileName already referenced in the VMX.
func uniqueDiskName(doc *vmxDoc, vmName string) string {
	existing := map[string]bool{}
	for _, l := range doc.lines {
		if k := vmxKeyOf(l); strings.HasSuffix(k, ".fileName") {
			if v, ok := doc.get(k); ok {
				existing[path.Base(v)] = true // disks on other datastores are referenced by full path
			}
		}
	}
	for i := 1; ; i++ {
		name := fmt.Sprintf("%s_disk%d.vmdk", vmName, i)
		if !existing[name] {
			return name
		}
	}
}

// firstEthernetKey is the device key ESXi assigns to ethernet0; ethernetN gets firstEthernetKey+N.
const firstEthernetKey = 4000

// ethernetIndex resolves a network adapter device key to its VMX index
// (ethernetN): by MAC address, or by key when the adapter has no MAC yet
// (a VM that was never powered on).
func ethernetIndex(doc *vmxDoc, addrs map[int32]deviceAddr, key int32) (int, error) {
	addr, ok := addrs[key]
	if !ok {
		return 0, fmt.Errorf("%w: nic with key %d", ErrNotFound, key)
	}
	if addr.mac != "" {
		if idx := ethernetIndexByMAC(doc, addr.mac); idx >= 0 {
			return idx, nil
		}
	}
	idx := int(key) - firstEthernetKey
	if idx >= 0 {
		if _, present := doc.get(fmt.Sprintf("ethernet%d.present", idx)); present {
			return idx, nil
		}
	}
	return 0, fmt.Errorf("cannot find VMX ethernet entry for nic key %d (mac %q)", key, addr.mac)
}

func ethernetIndexByMAC(doc *vmxDoc, mac string) int {
	mac = strings.ToLower(mac)
	for i := 0; i < 16; i++ {
		for _, key := range []string{fmt.Sprintf("ethernet%d.generatedAddress", i), fmt.Sprintf("ethernet%d.address", i)} {
			if v, ok := doc.get(key); ok && strings.ToLower(v) == mac {
				return i
			}
		}
	}
	return -1
}

// ---- DELETE ----

func (c *Client) DeleteVM(ctx context.Context, id string, deleteFiles bool) error {
	if err := c.Ensure(ctx); err != nil {
		return err
	}
	if c.ssh == nil {
		return nil
	}
	m, err := c.retrieveVM(ctx, id, []string{"summary.runtime.powerState", "summary.config.vmPathName"})
	if err != nil {
		return err
	}
	state := m.Summary.Runtime.PowerState
	vmDir := path.Dir(dsPathToFS(m.Summary.Config.VmPathName))
	if deleteFiles {
		if state == types.VirtualMachinePowerStatePoweredOn {
			if out, err := c.sshRun(ctx, "vim-cmd vmsvc/power.off "+shq(id)); err != nil {
				return fmt.Errorf("power off: %w (output: %s)", err, out)
			}
		}
		// wait until the power state actually settles (hard-off releases disk locks late)
		for i := 0; i < 30; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-after(1 * time.Second):
			}
			st, err := readPowerState(c, ctx, id)
			if err != nil {
				return nil // already gone
			}
			if st == "poweredOff" {
				break
			}
		}
		var lastErr error
		for i := 0; i < 3; i++ {
			out, err := c.sshRun(ctx, "vim-cmd vmsvc/destroy "+shq(id))
			if err == nil {
				// destroy leaves files it does not know (the cloud-init seed image): remove
				// that one and the folder, which rmdir only does once it is empty.
				if strings.HasPrefix(vmDir, "/vmfs/volumes/") && path.Base(vmDir) != "" {
					_, _ = c.sshRun(ctx, fmt.Sprintf("rm -f %s; rmdir %s 2>/dev/null; true", shq(vmDir+"/seed.iso"), shq(vmDir)))
				}
				return nil
			}
			lastErr = fmt.Errorf("destroy: %w (output: %s)", err, out)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-after(2 * time.Second):
			}
		}
		return lastErr
	}
	if state == types.VirtualMachinePowerStatePoweredOn {
		return fmt.Errorf("%w: cannot unregister a powered on VM; power it off first or use delete_files=true", ErrInvalidState)
	}
	if out, err := c.sshRun(ctx, "vim-cmd vmsvc/unregister "+shq(id)); err != nil {
		return fmt.Errorf("unregister: %w (output: %s)", err, out)
	}
	return nil
}

// ---- POWER ----

func (c *Client) SetPower(ctx context.Context, id, action string) (string, error) {
	if err := c.Ensure(ctx); err != nil {
		return "", err
	}
	m, err := c.retrieveVM(ctx, id, []string{"name", "config.extraConfig"})
	if err != nil {
		return "", err
	}
	if c.ssh == nil {
		return "", ErrSSHDisabled
	}
	if action == "on" && m.Config != nil && isTemplate(m.Config.ExtraConfig) {
		return "", fmt.Errorf("%w: %s is a template; convert it to a virtual machine or clone it", ErrInvalidState, m.Name)
	}

	var vimAction string
	switch action {
	case "on":
		vimAction = "power.on"
	case "off":
		vimAction = "power.off"
	case "reset":
		vimAction = "power.reset"
	case "suspend":
		vimAction = "power.suspend"
	case "shutdown_guest":
		vimAction = "power.shutdown"
	case "reboot_guest":
		vimAction = "power.reboot"
	default:
		return "", fmt.Errorf("unknown power action %q", action)
	}

	if out, err := c.sshRun(ctx, "vim-cmd vmsvc/"+vimAction+" "+shq(id)); err != nil {
		return "", fmt.Errorf("power %s: %w (output: %s)", action, err, out)
	}

	// guest-initiated actions change state asynchronously: short wait then report.
	if action == "shutdown_guest" || action == "reboot_guest" {
		select {
		case <-ctx.Done():
		case <-after(3 * time.Second):
		}
		return readPowerState(c, ctx, id)
	}

	// task-based actions: wait until the desired state settles (max ~30s).
	want := "poweredOn"
	if action == "off" {
		want = "poweredOff"
	} else if action == "suspend" {
		want = "suspended"
	}
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-after(1 * time.Second):
		}
		if st, err := readPowerState(c, ctx, id); err == nil {
			if st == want {
				return st, nil
			}
		}
	}
	return readPowerState(c, ctx, id)
}

func readPowerState(c *Client, ctx context.Context, id string) (string, error) {
	m, err := c.retrieveVM(ctx, id, []string{"summary.runtime.powerState"})
	if err != nil {
		return "", err
	}
	return string(m.Summary.Runtime.PowerState), nil
}
