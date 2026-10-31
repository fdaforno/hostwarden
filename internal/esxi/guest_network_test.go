package esxi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25"

	"hostwarden/internal/model"
)

func staticGuestRequest() *model.CreateVMRequest {
	return &model.CreateVMRequest{
		Name: "auto-network", CloudImage: "cloud-images/ubuntu.vmdk", Network: "VM Network",
		CloudInit: &model.CloudInitConfig{
			UserData: "#cloud-config\npackages: [vim]\n",
			Network:  &model.CloudInitNetwork{Address: "192.168.1.100/24", Gateway: "192.168.1.1", DNSServers: []string{"1.1.1.1"}},
		},
	}
}

func TestGuestNetworkValidation(t *testing.T) {
	for _, address := range []string{"192.168.1.100/24", "10.0.0.1/31", "10.0.0.1/32"} {
		req := staticGuestRequest()
		req.CloudInit.Network.Address, req.CloudInit.Network.Gateway = address, ""
		if err := validateGuestNetwork(req); err != nil {
			t.Fatalf("%s: %v", address, err)
		}
	}
	cases := map[string]func(*model.CreateVMRequest){
		"no image":               func(r *model.CreateVMRequest) { r.CloudImage = "" },
		"no NIC":                 func(r *model.CreateVMRequest) { r.Network = "" },
		"conflicting raw YAML":   func(r *model.CreateVMRequest) { r.CloudInit.NetworkConfig = "version: 2" },
		"invalid CIDR":           func(r *model.CreateVMRequest) { r.CloudInit.Network.Address = "192.168.1.100/33" },
		"IPv6":                   func(r *model.CreateVMRequest) { r.CloudInit.Network.Address = "2001:db8::1/64" },
		"network":                func(r *model.CreateVMRequest) { r.CloudInit.Network.Address = "192.168.1.0/24" },
		"broadcast":              func(r *model.CreateVMRequest) { r.CloudInit.Network.Address = "192.168.1.255/24" },
		"gateway outside subnet": func(r *model.CreateVMRequest) { r.CloudInit.Network.Gateway = "192.168.2.1" },
		"same gateway":           func(r *model.CreateVMRequest) { r.CloudInit.Network.Gateway = "192.168.1.100" },
		"broadcast gateway":      func(r *model.CreateVMRequest) { r.CloudInit.Network.Gateway = "192.168.1.255" },
		"invalid DNS":            func(r *model.CreateVMRequest) { r.CloudInit.Network.DNSServers = []string{"1.1.1.1", "bad"} },
		"multicast DNS":          func(r *model.CreateVMRequest) { r.CloudInit.Network.DNSServers = []string{"224.0.0.1"} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			req := staticGuestRequest()
			change(req)
			if err := validateGuestNetwork(req); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestAllocateGuestMAC(t *testing.T) {
	// The first candidate collides. The next must stay in VMware's manual range.
	used := map[string]bool{"00:50:56:3f:aa:bb": true}
	mac, err := allocateGuestMAC(used, bytes.NewReader([]byte{255, 170, 187, 255, 170, 188}))
	if err != nil || mac != "00:50:56:3f:aa:bc" {
		t.Fatalf("mac=%q error=%v", mac, err)
	}
	if _, err := allocateGuestMAC(used, bytes.NewReader(nil)); err == nil {
		t.Fatal("random source failure must abort allocation")
	}
	if _, err := allocateGuestMAC(used, bytes.NewReader(bytes.Repeat([]byte{255, 170, 187}, 100))); err == nil {
		t.Fatal("repeated collisions must abort allocation")
	}
}

func TestAutomaticGuestNetworkSeedAndNIC(t *testing.T) {
	err := simulator.ESX().Run(func(ctx context.Context, vc *vim25.Client) error {
		c := &Client{vc: vc, finder: find.NewFinder(vc), pc: property.DefaultCollector(vc)}
		req := staticGuestRequest()
		original := req.CloudInit
		release, err := c.prepareGuestNetwork(ctx, req)
		if err != nil {
			return err
		}
		defer release()
		if original.NetworkConfig != "" || req.NetworkMAC == "" {
			t.Fatal("must resolve a MAC without mutating the original config")
		}
		doc := parseVMX(buildVMX(req))
		wantVMX(t, doc, "ethernet0.addressType", "static")
		wantVMX(t, doc, "ethernet0.address", req.NetworkMAC)
		if !strings.Contains(req.CloudInit.NetworkConfig, "macaddress: \""+req.NetworkMAC+"\"") {
			t.Fatal("seed config must match the MAC assigned in VMX")
		}
		guestMACs.Lock()
		reserved := guestMACs.pending[req.NetworkMAC]
		guestMACs.Unlock()
		if !reserved {
			t.Fatal("MAC must remain reserved during VM creation")
		}
		t.Run("seed contains network and custom user data", func(t *testing.T) {
			for _, program := range []string{"genisoimage", "isoinfo"} {
				if _, err := exec.LookPath(program); err != nil {
					t.Skipf("%s is not installed", program)
				}
			}
			iso, err := BuildSeedISO(req.CloudInit, req.Name)
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(iso)
			for file, want := range map[string]string{"network-config": req.CloudInit.NetworkConfig, "user-data": original.UserData} {
				got, err := exec.Command("isoinfo", "-R", "-i", iso, "-x", "/"+file).Output()
				if err != nil || string(got) != want {
					t.Fatalf("ISO %s differs: err=%v, content=%q", file, err, got)
				}
			}
		})
		release()
		guestMACs.Lock()
		reserved = guestMACs.pending[req.NetworkMAC]
		guestMACs.Unlock()
		if reserved {
			t.Fatal("MAC reservation leaked")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGuestNetworkDHCPAndRawConfigUnchanged(t *testing.T) {
	c := &Client{} // These cases must not need a MAC or an inventory query.
	for _, cfg := range []*model.CloudInitConfig{nil, {}, {NetworkConfig: "version: 2\nethernets: {}\n"}} {
		req := &model.CreateVMRequest{Network: "VM Network", CloudInit: cfg}
		release, err := c.prepareGuestNetwork(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		release()
		if req.CloudInit != cfg || req.NetworkMAC != "" {
			t.Fatal("DHCP/raw network config changed")
		}
		wantVMX(t, parseVMX(buildVMX(req)), "ethernet0.addressType", "generated")
	}
	var req model.CreateVMRequest
	if err := json.Unmarshal([]byte(`{"NetworkMAC":"injected","network_mac":"injected"}`), &req); err != nil || req.NetworkMAC != "" {
		t.Fatal("internal MAC must not be accepted from JSON")
	}
}
