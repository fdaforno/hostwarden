package esxi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/crypto/ssh"

	"hostwarden/internal/model"
)

// Exercise capability selection through CreateVM and capture the VMX sent over
// SSH. Registration deliberately fails; these tests never create a real VM.
func TestCreateVMCompatibilityProfiles(t *testing.T) {
	for _, tc := range []struct {
		name       string
		versions   []string
		want       string
		legacy     bool
		defaultKey string
		viaVCenter bool
	}{
		{"upstream", []string{"vmx-20", "vmx-21"}, "21", false, "vmx-20", false},
		{"newer-default-keeps-upstream", []string{"vmx-21", "vmx-22"}, "21", false, "vmx-22", false},
		{"esxi67-keeps-host-default", []string{"vmx-13", "vmx-14", "vmx-15"}, "14", true, "vmx-14", false},
		{"esxi67-newer-default", []string{"vmx-13", "vmx-14", "vmx-15"}, "15", true, "vmx-15", false},
		{"esxi7", []string{"vmx-17", "vmx-18", "vmx-19"}, "19", true, "", false},
		{"esxi8-before-vmx21", []string{"vmx-19", "vmx-20"}, "20", true, "vmx-20", false},
		{"vcenter-target-host", []string{"vmx-21"}, "21", false, "vmx-21", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			simulator.Test(func(ctx context.Context, vc *vim25.Client) {
				host := simulator.Map(ctx).Any("HostSystem").Reference()
				fixture := &creationCapabilities{RoundTripper: vc.RoundTripper, host: host, withHost: tc.viaVCenter}
				if tc.viaVCenter {
					vc.ServiceContent.About.ApiType = "VirtualCenter"
				}
				for _, key := range tc.versions {
					d := types.VirtualMachineConfigOptionDescriptor{Key: key, Host: []types.ManagedObjectReference{host}, CreateSupported: true, RunSupported: true, DefaultConfigOption: key == tc.defaultKey}
					if strings.HasPrefix(tc.name, "esxi67") {
						d.Host = nil // Standalone 6.7 omits the optional host list.
					}
					fixture.descriptors = append(fixture.descriptors, d)
				}
				vc.RoundTripper = fixture
				var vmx string
				var captureMu sync.Mutex
				client := serveCreateSSH(t, func(cmd string) (string, uint32) {
					captureMu.Lock()
					defer captureMu.Unlock()
					switch {
					case strings.HasPrefix(cmd, "test -f "):
						return "", 1
					case strings.HasPrefix(cmd, "cat > "):
						parts := strings.SplitN(cmd, "\n", 2)
						if len(parts) == 2 {
							vmx = strings.TrimSuffix(parts[1], "FCVMXEOF\n")
						}
					case strings.HasPrefix(cmd, "vim-cmd solo/registervm "):
						return "stop after vmx capture", 1
					}
					return "", 0
				})
				c := &Client{vc: vc, finder: find.NewFinder(vc), pc: property.DefaultCollector(vc), ssh: client}
				_, err := c.CreateVM(ctx, &model.CreateVMRequest{Name: "compat-test", Datastore: "datastore", GuestID: "ubuntu64Guest", MemoryMB: 2048, NumCPUs: 2, CloudImage: "images/ubuntu.vmdk", Network: "VM Network", AdapterType: "vmxnet3"})
				if err == nil || !strings.Contains(err.Error(), "stop after vmx capture") {
					t.Fatalf("did not reach registration: %v", err)
				}
				if fixture.selectedKey != "vmx-"+tc.want {
					t.Errorf("queried %q, want vmx-%s", fixture.selectedKey, tc.want)
				}
				captureMu.Lock()
				defer captureMu.Unlock()
				doc := parseVMX(vmx)
				wantVMX(t, doc, "virtualHW.version", tc.want)
				if tc.legacy {
					if strings.Contains(vmx, "pciRootBridge") {
						t.Error("legacy VMX contains upstream root bridges")
					}
					wantVMX(t, doc, "pciBridge0.present", "TRUE")
					for i := 4; i <= 7; i++ {
						prefix := fmt.Sprintf("pciBridge%d.", i)
						wantVMX(t, doc, prefix+"virtualDev", "pcieRootPort")
						wantVMX(t, doc, prefix+"functions", "8")
					}
				} else {
					baseline, err := os.ReadFile("testdata/create-upstream.vmx")
					if err != nil {
						t.Fatal(err)
					}
					if vmx != string(baseline) {
						t.Errorf("newer-host VMX differs from the upstream baseline:\n%s", vmx)
					}
					for i := 0; i < 2; i++ {
						prefix := fmt.Sprintf("pciBridge%d.", i)
						wantVMX(t, doc, prefix+"present", "TRUE")
						wantVMX(t, doc, prefix+"virtualDev", "pciRootBridge")
						wantVMX(t, doc, prefix+"functions", "1")
					}
					if strings.Contains(vmx, "pcieRootPort") {
						t.Error("upstream VMX changed to legacy PCIe ports")
					}
				}
			}, simulator.ESX())
		})
	}
}

func TestCreateVMRejectsUnknownCapabilitiesBeforeSSH(t *testing.T) {
	for _, mode := range []string{"empty", "unknown", "cannot-create", "cannot-run", "other-host", "query-error", "nil-config", "wrong-config", "options-query-error"} {
		t.Run(mode, func(t *testing.T) {
			simulator.Test(func(ctx context.Context, vc *vim25.Client) {
				host := simulator.Map(ctx).Any("HostSystem").Reference()
				d := types.VirtualMachineConfigOptionDescriptor{Key: "vmx-21", Host: []types.ManagedObjectReference{host}, CreateSupported: true, RunSupported: true}
				fixture := &creationCapabilities{RoundTripper: vc.RoundTripper, host: host, mode: mode}
				switch mode {
				case "unknown":
					d.Key = "vmx-99"
				case "cannot-create":
					d.CreateSupported = false
				case "cannot-run":
					d.RunSupported = false
				case "other-host":
					d.Host = []types.ManagedObjectReference{{Type: "HostSystem", Value: "another-host"}}
				}
				if mode != "empty" {
					fixture.descriptors = []types.VirtualMachineConfigOptionDescriptor{d}
				}
				vc.RoundTripper = fixture
				var commands int
				var captureMu sync.Mutex
				sshClient := serveCreateSSH(t, func(string) (string, uint32) {
					captureMu.Lock()
					defer captureMu.Unlock()
					commands++
					return "unexpected SSH command", 2
				})
				c := &Client{vc: vc, finder: find.NewFinder(vc), pc: property.DefaultCollector(vc), ssh: sshClient}
				_, err := c.CreateVM(ctx, &model.CreateVMRequest{Name: "compat-test", Datastore: "datastore"})
				if err == nil || !strings.Contains(err.Error(), "compatibility") {
					t.Errorf("expected compatibility error, got %v", err)
				}
				captureMu.Lock()
				defer captureMu.Unlock()
				if commands != 0 {
					t.Errorf("%d SSH commands before compatibility validation", commands)
				}
			}, simulator.ESX())
		})
	}
}

func TestGuestOSListUsesSelectedCreationProfile(t *testing.T) {
	simulator.Test(func(ctx context.Context, vc *vim25.Client) {
		host := simulator.Map(ctx).Any("HostSystem").Reference()
		fixture := &creationCapabilities{RoundTripper: vc.RoundTripper, host: host, descriptors: []types.VirtualMachineConfigOptionDescriptor{
			{Key: "vmx-21", CreateSupported: true, RunSupported: true},
			{Key: "vmx-22", CreateSupported: true, RunSupported: true, DefaultConfigOption: true},
		}}
		vc.RoundTripper = fixture
		c := &Client{vc: vc, finder: find.NewFinder(vc), pc: property.DefaultCollector(vc)}
		guests, err := c.GuestOSList(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if fixture.selectedKey != "vmx-21" || len(guests) != 1 || guests[0].ID != "ubuntu64Guest" {
			t.Fatalf("guest list used a different profile: key=%s guests=%v", fixture.selectedKey, guests)
		}
	}, simulator.ESX())
}

type creationCapabilities struct {
	soap.RoundTripper
	host        types.ManagedObjectReference
	descriptors []types.VirtualMachineConfigOptionDescriptor
	selectedKey string
	withHost    bool
	mode        string
}

func (f *creationCapabilities) RoundTrip(ctx context.Context, req, res soap.HasFault) error {
	switch r := req.(type) {
	case *methods.QueryConfigOptionDescriptorBody:
		if f.mode == "query-error" {
			return errors.New("capability service unavailable")
		}
		res.(*methods.QueryConfigOptionDescriptorBody).Res = &types.QueryConfigOptionDescriptorResponse{Returnval: f.descriptors}
		return nil
	case *methods.QueryConfigOptionExBody:
		if f.mode == "options-query-error" {
			return errors.New("configuration query unavailable")
		}
		if r.Req.Spec == nil || r.Req.Spec.Key == "" {
			return errors.New("query must select a hardware version")
		}
		if f.withHost {
			if r.Req.Spec.Host == nil || *r.Req.Spec.Host != f.host {
				return errors.New("vCenter query must name the target host")
			}
		} else if r.Req.Spec.Host != nil {
			return errors.New("standalone ESXi 6.7 rejects Key and Host together")
		}

		f.selectedKey = r.Req.Spec.Key
		response := &types.QueryConfigOptionExResponse{}
		if f.mode != "nil-config" {
			response.Returnval = &types.VirtualMachineConfigOption{Version: f.selectedKey, GuestOSDescriptor: []types.GuestOsDescriptor{{Id: "ubuntu64Guest", FullName: "Ubuntu Linux (64-bit)"}}}
		}
		if f.mode == "wrong-config" {
			response.Returnval.Version = "vmx-22"
		}
		res.(*methods.QueryConfigOptionExBody).Res = response
		return nil
	}
	return f.RoundTripper.RoundTrip(ctx, req, res)
}

// A loopback SSH server keeps the production SSHClient and shell-command path
// in the regression test without adding a test-only hook to Client.
func serveCreateSSH(t *testing.T, run func(string) (string, uint32)) *SSHClient {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var handlers sync.WaitGroup
	var runMu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer conn.Close()
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					if incoming.ChannelType() != "session" {
						_ = incoming.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					channel, requests, err := incoming.Accept()
					if err != nil {
						continue
					}
					for request := range requests {
						if request.Type != "exec" {
							_ = request.Reply(false, nil)
							continue
						}
						var payload struct{ Command string }
						if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
							_ = request.Reply(false, nil)
							break
						}
						_ = request.Reply(true, nil)
						runMu.Lock()
						output, status := run(payload.Command)
						runMu.Unlock()
						_, _ = io.WriteString(channel, output)
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
						break
					}
					_ = channel.Close()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		handlers.Wait()
	})
	return NewSSHClient("127.0.0.1", listener.Addr().(*net.TCPAddr).Port, "test", "", "")
}
