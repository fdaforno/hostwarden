package esxi

import (
	"strings"
	"testing"

	"hostwarden/internal/model"
)

const sampleVMX = `.encoding = "UTF-8"
config.version = "8"
virtualHW.version = "21"
displayName = "vm1"
guestOS = "ubuntu-64"
memSize = "2048"
numvcpus = "2"
scsi0.present = "TRUE"
scsi0.virtualDev = "lsisas1068"
scsi0:0.present = "TRUE"
scsi0:0.fileName = "vm1.vmdk"
scsi0.pciSlotNumber = "32"
ethernet0.present = "TRUE"
ethernet0.virtualDev = "vmxnet3"
ethernet0.networkName = "VM Network"
ethernet0.generatedAddress = "00:0c:29:aa:bb:cc"
ide1:0.present = "TRUE"
ide1:0.deviceType = "cdrom-image"
ide1:0.fileName = "/vmfs/volumes/DS/isos/x.iso"
`

func TestParseAndEditVMX(t *testing.T) {
	doc := parseVMX(sampleVMX)
	if v, ok := doc.get("guestOS"); !ok || v != "ubuntu-64" {
		t.Fatalf("guestOS = %q, %v", v, ok)
	}
	doc.set("memSize", "4096")
	if v, _ := doc.get("memSize"); v != "4096" {
		t.Fatalf("memSize after set = %q", v)
	}
	doc.set("annotation", "new note")
	if v, _ := doc.get("annotation"); v != "new note" {
		t.Fatalf("annotation = %q", v)
	}
}

func TestKeysWithPrefix(t *testing.T) {
	doc := parseVMX(sampleVMX)
	keys := doc.keysWithPrefix("scsi")
	want := map[string]bool{
		"scsi0.present": true, "scsi0.virtualDev": true,
		"scsi0:0.present": true, "scsi0:0.fileName": true, "scsi0.pciSlotNumber": true,
	}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("unexpected key %q", k)
		}
		delete(want, k)
	}
	if len(want) > 0 {
		t.Errorf("missing keys: %v", want)
	}
	eth := doc.keysWithPrefix("ethernet")
	if len(eth) != 4 {
		t.Errorf("ethernet keys = %v", eth)
	}
}

func TestFreeSCSISlot(t *testing.T) {
	doc := parseVMX(sampleVMX)
	bus, unit := freeSCSISlot(doc)
	if bus != 0 || unit != 1 {
		t.Fatalf("freeSCSISlot = (%d,%d), want (0,1)", bus, unit)
	}
}

func TestFreeEthernetIndex(t *testing.T) {
	doc := parseVMX(sampleVMX)
	if i := freeEthernetIndex(doc); i != 1 {
		t.Fatalf("freeEthernetIndex = %d, want 1", i)
	}
}

func TestRemovePrefix(t *testing.T) {
	doc := parseVMX(sampleVMX)
	doc.removePrefix("scsi0:0")
	if _, ok := doc.get("scsi0:0.fileName"); ok {
		t.Fatal("scsi0:0.fileName should be removed")
	}
	if _, ok := doc.get("scsi0.present"); !ok {
		t.Fatal("scsi0.present should survive")
	}
}

func TestEthernetIndexByMAC(t *testing.T) {
	doc := parseVMX(sampleVMX)
	if i := ethernetIndexByMAC(doc, "00:0C:29:AA:BB:CC"); i != 0 {
		t.Fatalf("ethernetIndexByMAC = %d, want 0", i)
	}
	if i := ethernetIndexByMAC(doc, "00:11:22:33:44:55"); i != -1 {
		t.Fatalf("ethernetIndexByMAC unknown = %d, want -1", i)
	}
}

func TestUniqueDiskName(t *testing.T) {
	doc := parseVMX(sampleVMX)
	if n := uniqueDiskName(doc, "vm1"); n != "vm1_disk1.vmdk" {
		t.Fatalf("uniqueDiskName = %q", n)
	}
}

func TestGuestOSToVMX(t *testing.T) {
	cases := map[string]string{
		"ubuntu64Guest":          "ubuntu-64",
		"rockylinux_64Guest":     "rockylinux-64",
		"other6xLinux64Guest":    "other6xlinux-64",
		"windows2019srv_64Guest": "windows2019srv-64",
		"windows10_64Guest":      "windows9-64",
		"windows11_64Guest":      "windows9-64",
		"debian11_64Guest":       "debian11-64",
		"ubuntu-64":              "ubuntu-64",
		"dosGuest":               "dos",
	}
	for in, want := range cases {
		if got := guestOSToVMX(in); got != want {
			t.Errorf("guestOSToVMX(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSnapshotGet(t *testing.T) {
	out := `Get Snapshot:
|-ROOT
--Snapshot Name        : base
--Snapshot Id        : 1
--Snapshot Desciption  : first
--Snapshot Created On  : 10/6/2026 10:12:15
--Snapshot State       : powered off
---Snapshot Name        : child
---Snapshot Id        : 2
---Snapshot Desciption  :
---Snapshot Created On  : 10/6/2026 10:13:00
---Snapshot State       : powered off
--Snapshot Name        : second
--Snapshot Id        : 3
--Snapshot Desciption  :
--Snapshot Created On  : 10/6/2026 10:14:00
--Snapshot State       : powered off
`
	snaps := parseSnapshotGet(out)
	if len(snaps) != 3 {
		t.Fatalf("snaps = %d, want 3", len(snaps))
	}
	if snaps[0].Name != "base" || snaps[0].ID != "1" || snaps[0].Description != "first" {
		t.Errorf("snap0 = %+v", snaps[0])
	}
	if !snaps[0].HasChildren {
		t.Error("base should have children")
	}
	if snaps[1].Path != "base/child" || snaps[1].ID != "2" {
		t.Errorf("snap1 = %+v", snaps[1])
	}
	if snaps[2].Name != "second" || snaps[2].ID != "3" || snaps[2].HasChildren {
		t.Errorf("snap2 = %+v", snaps[2])
	}
}

func TestDsPathToFS(t *testing.T) {
	if got := dsPathToFS("[CM6-02] fc/vm.vmx"); got != "/vmfs/volumes/CM6-02/fc/vm.vmx" {
		t.Errorf("dsPathToFS = %q", got)
	}
}

func TestBuildVMXNestedHV(t *testing.T) {
	req := &model.CreateVMRequest{
		Name:    "nested",
		GuestID: "ubuntu64Guest",
	}
	if got := buildVMX(req, upstreamVMProfile); strings.Contains(got, "vhv.enable") {
		t.Errorf("vhv.enable written without the option:\n%s", got)
	}
	req.NestedHV = true
	if got := buildVMX(req, upstreamVMProfile); !strings.Contains(got, `vhv.enable = "TRUE"`) {
		t.Errorf("vhv.enable missing:\n%s", got)
	}
}
