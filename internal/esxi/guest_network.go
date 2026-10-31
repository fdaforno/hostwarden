package esxi

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"sync"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"hostwarden/internal/model"
)

// Reserve addresses during concurrent creates, until the registered VM is visible
// in inventory. ESXi may not generate a NIC's MAC until the first power-on, so
// the seed and VMX must share a static MAC before registration.
var guestMACs = struct {
	sync.Mutex
	pending map[string]bool
}{pending: make(map[string]bool)}

func validGuestIPv4(ip netip.Addr) bool {
	if !ip.Is4() {
		return false
	}
	first := ip.As4()[0]
	return first != 0 && first != 127 && first < 224
}

func subnetEndpoint(ip netip.Addr, prefix netip.Prefix) bool {
	if prefix.Bits() >= 31 {
		return false
	}
	if ip == prefix.Masked().Addr() {
		return true
	}
	return !prefix.Contains(ip.Next())
}

func validateGuestNetwork(req *model.CreateVMRequest) error {
	if req.CloudInit == nil || req.CloudInit.Network == nil {
		return nil
	}
	if req.CloudImage == "" || req.Network == "" {
		return fmt.Errorf("static guest networking requires a cloud image and a network adapter")
	}
	if req.CloudInit.NetworkConfig != "" {
		return fmt.Errorf("cloud_init.network and network_config are mutually exclusive")
	}
	n := req.CloudInit.Network
	prefix, err := netip.ParsePrefix(n.Address)
	if err != nil || !validGuestIPv4(prefix.Addr()) || subnetEndpoint(prefix.Addr(), prefix) {
		return fmt.Errorf("guest network address must be a usable IPv4 CIDR address")
	}
	if n.Gateway != "" {
		gw, err := netip.ParseAddr(n.Gateway)
		if err != nil || !validGuestIPv4(gw) || gw == prefix.Addr() || !prefix.Contains(gw) || subnetEndpoint(gw, prefix) {
			return fmt.Errorf("guest gateway must be a different usable IPv4 address in the same subnet")
		}
	}
	for _, server := range n.DNSServers {
		ip, err := netip.ParseAddr(server)
		if err != nil || !validGuestIPv4(ip) {
			return fmt.Errorf("guest DNS servers must be IPv4 unicast addresses")
		}
	}
	return nil
}

// allocateGuestMAC uses VMware's manual address range, 00:50:56:00:00:00 to
// 00:50:56:3f:ff:ff, which does not overlap its automatically generated ranges.
func allocateGuestMAC(used map[string]bool, random io.Reader) (string, error) {
	for range 100 {
		var suffix [3]byte
		if _, err := io.ReadFull(random, suffix[:]); err != nil {
			return "", fmt.Errorf("generate guest MAC: %w", err)
		}
		mac := fmt.Sprintf("00:50:56:%02x:%02x:%02x", suffix[0]&0x3f, suffix[1], suffix[2])
		if !used[mac] {
			return mac, nil
		}
	}
	return "", fmt.Errorf("could not allocate an unused guest MAC address")
}

func (c *Client) prepareGuestNetwork(ctx context.Context, req *model.CreateVMRequest) (func(), error) {
	req.NetworkMAC = ""
	noop := func() {}
	if err := validateGuestNetwork(req); err != nil {
		return noop, err
	}
	if req.CloudInit == nil || req.CloudInit.Network == nil {
		return noop, nil
	}
	guestMACs.Lock()
	defer guestMACs.Unlock()
	hsRef, err := c.hostSystemRef(ctx)
	if err != nil {
		return noop, err
	}
	var host mo.HostSystem
	if err := c.pc.RetrieveOne(ctx, hsRef, []string{"vm"}, &host); err != nil {
		return noop, fmt.Errorf("read VM inventory for guest MAC allocation: %w", err)
	}
	var vms []mo.VirtualMachine
	if len(host.Vm) > 0 {
		if err := c.pc.Retrieve(ctx, host.Vm, []string{"config.hardware.device"}, &vms); err != nil {
			return noop, fmt.Errorf("read existing guest MAC addresses: %w", err)
		}
	}
	used := make(map[string]bool)
	for mac := range guestMACs.pending {
		used[mac] = true
	}
	for _, vm := range vms {
		if vm.Config == nil {
			return noop, fmt.Errorf("cannot check MAC addresses for VM %s", vm.Self.Value)
		}
		for _, device := range vm.Config.Hardware.Device {
			if nic, ok := device.(types.BaseVirtualEthernetCard); ok {
				used[strings.ToLower(nic.GetVirtualEthernetCard().MacAddress)] = true
			}
		}
	}
	mac, err := allocateGuestMAC(used, rand.Reader)
	if err != nil {
		return noop, err
	}
	guestMACs.pending[mac] = true
	req.NetworkMAC = mac
	// Copy the config so resolving the network does not alter the caller's input.
	cfg := *req.CloudInit
	cfg.NetworkConfig = buildGuestNetworkConfig(cfg.Network, mac)
	req.CloudInit = &cfg
	return func() {
		guestMACs.Lock()
		delete(guestMACs.pending, mac)
		guestMACs.Unlock()
	}, nil
}

func buildGuestNetworkConfig(n *model.CloudInitNetwork, mac string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "version: 2\nethernets:\n  primary:\n    match:\n      macaddress: %q\n    dhcp4: false\n    addresses:\n      - %q\n", mac, n.Address)
	if n.Gateway != "" {
		fmt.Fprintf(&b, "    routes:\n      - to: \"0.0.0.0/0\"\n        via: %q\n", n.Gateway)
	}
	if len(n.DNSServers) > 0 {
		b.WriteString("    nameservers:\n      addresses:\n")
		for _, dns := range n.DNSServers {
			fmt.Fprintf(&b, "        - %q\n", dns)
		}
	}
	return b.String()
}
