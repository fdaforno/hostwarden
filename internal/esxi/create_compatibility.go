package esxi

import (
	"context"
	"fmt"
	"slices"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// A creation profile couples the hardware version with its VMX bridge layout.
// Only selectCreationProfile constructs profiles from host capabilities.
type vmCreationProfile int

const upstreamVMProfile vmCreationProfile = 21

func (p vmCreationProfile) key() string { return fmt.Sprintf("vmx-%d", p) }

func selectCreationProfile(descriptors []types.VirtualMachineConfigOptionDescriptor, host types.ManagedObjectReference) (vmCreationProfile, error) {
	available := make(map[string]bool)
	defaultKey := ""
	for _, d := range descriptors {
		if !d.CreateSupported || !d.RunSupported {
			continue
		}
		if len(d.Host) > 0 && !slices.Contains(d.Host, host) {
			continue
		}
		available[d.Key] = true
		if d.DefaultConfigOption {
			defaultKey = d.Key
		}
	}
	// Preserve the upstream vmx-21 + pciRootBridge layout whenever supported,
	// even if the host defaults to a newer hardware version.
	if available[upstreamVMProfile.key()] {
		return upstreamVMProfile, nil
	}
	// Keep the host's default when it has a known legacy layout. Otherwise use
	// the newest supported entry in the explicit compatibility set.
	legacyProfiles := []vmCreationProfile{20, 19, 18, 17, 15, 14, 13}
	for _, p := range legacyProfiles {
		if p.key() == defaultKey {
			return p, nil
		}
	}
	// Known ESXi hardware versions with the legacy PCI + PCIe root-port layout.
	// Do not infer a bridge layout for unknown versions (e.g. Workstation-only
	// vmx-16). Select the newest supported entry from this explicit set.
	for _, p := range legacyProfiles {
		if available[p.key()] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("host reports no supported VM creation profile: need vmx-21 or a compatible ESXi vmx-13/14/15/17/18/19/20 configuration with create and power-on support")
}

// creationConfig validates the selected profile against this specific host
// before CreateVM executes any SSH commands or creates datastore files.
func (c *Client) creationConfig(ctx context.Context) (vmCreationProfile, *types.VirtualMachineConfigOption, error) {
	browser, host, err := c.hostEnvironmentBrowser(ctx)
	if err != nil {
		return 0, nil, err
	}
	descriptors, err := browser.QueryConfigOptionDescriptor(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("query VM creation capabilities: %w", err)
	}
	profile, err := selectCreationProfile(descriptors, host)
	if err != nil {
		return 0, nil, err
	}
	spec := &types.EnvironmentBrowserConfigOptionQuerySpec{Key: profile.key()}
	// Direct ESXi 6.7 connections reject Key + Host together. Their environment
	// browser already belongs to the single connected host. vCenter still needs
	// an explicit target to avoid using another host's configuration.
	if c.vc.ServiceContent.About.ApiType != "HostAgent" {
		spec.Host = &host
	}
	opt, err := browser.QueryConfigOption(ctx, spec)
	if err != nil {
		return 0, nil, fmt.Errorf("query host configuration %s: %w", profile.key(), err)
	}
	if opt == nil || opt.Version != profile.key() {
		return 0, nil, fmt.Errorf("host did not confirm the selected VM configuration %s", profile.key())
	}
	return profile, opt, nil
}

// The caller must first Ensure the API session.
func (c *Client) hostEnvironmentBrowser(ctx context.Context) (*object.EnvironmentBrowser, types.ManagedObjectReference, error) {
	hsRef, err := c.hostSystemRef(ctx)
	if err != nil {
		return nil, hsRef, err
	}
	var hs mo.HostSystem
	if err := c.pc.RetrieveOne(ctx, hsRef, []string{"parent"}, &hs); err != nil {
		return nil, hsRef, err
	}
	if hs.Parent == nil {
		return nil, hsRef, fmt.Errorf("host has no parent compute resource")
	}
	var cr mo.ComputeResource
	if err := c.pc.RetrieveOne(ctx, *hs.Parent, []string{"environmentBrowser"}, &cr); err != nil {
		return nil, hsRef, err
	}
	if cr.EnvironmentBrowser == nil {
		return nil, hsRef, fmt.Errorf("environment browser not available")
	}
	return object.NewEnvironmentBrowser(c.vc, *cr.EnvironmentBrowser), hsRef, nil
}
