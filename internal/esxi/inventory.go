package esxi

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"hostwarden/internal/model"
)

// Datastores lists the host datastores.
func (c *Client) Datastores(ctx context.Context) ([]model.Datastore, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	dss, err := c.finder.DatastoreList(ctx, "*")
	if err != nil {
		return nil, err
	}
	out := make([]model.Datastore, 0, len(dss))
	for _, ds := range dss {
		var mds mo.Datastore
		if err := c.pc.RetrieveOne(ctx, ds.Reference(), []string{"summary"}, &mds); err != nil {
			return nil, err
		}
		s := mds.Summary
		out = append(out, model.Datastore{
			Name:       s.Name,
			Type:       s.Type,
			CapacityB:  s.Capacity,
			FreeB:      s.FreeSpace,
			URL:        s.Url,
			Accessible: s.Accessible,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Networks lists portgroups.
func (c *Client) Networks(ctx context.Context) ([]model.Network, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	v, err := view.NewManager(c.vc).CreateContainerView(ctx, c.vc.ServiceContent.RootFolder, []string{"Network"}, true)
	if err != nil {
		return nil, err
	}
	defer v.Destroy(ctx)
	var nets []mo.Network
	if err := v.Retrieve(ctx, []string{"Network"}, []string{"name"}, &nets); err != nil {
		return nil, err
	}
	out := make([]model.Network, 0, len(nets))
	for i := range nets {
		out = append(out, model.Network{Name: nets[i].Name, Type: "Network"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GuestOSList returns supported guest OS identifiers via the environment browser.
func (c *Client) GuestOSList(ctx context.Context) ([]model.GuestOS, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	_, opt, err := c.creationConfig(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.GuestOS, 0)
	if opt != nil {
		for _, d := range opt.GuestOSDescriptor {
			out = append(out, model.GuestOS{ID: d.Id, Name: d.FullName})
		}
	}
	if len(out) == 0 {
		// curated fallback
		for id, name := range fallbackGuests {
			out = append(out, model.GuestOS{ID: id, Name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

var fallbackGuests = map[string]string{
	"ubuntu64Guest":              "Ubuntu Linux (64-bit)",
	"debian11_64Guest":           "Debian 11 (64-bit)",
	"debian12_64Guest":           "Debian 12 (64-bit)",
	"rhel8_64Guest":              "Red Hat Enterprise Linux 8 (64-bit)",
	"rhel9_64Guest":              "Red Hat Enterprise Linux 9 (64-bit)",
	"rockylinux_64Guest":         "Rocky Linux (64-bit)",
	"almalinux_64Guest":          "AlmaLinux (64-bit)",
	"centos8_64Guest":            "CentOS 8 (64-bit)",
	"fedora64Guest":              "Fedora Linux (64-bit)",
	"sles15_64Guest":             "SUSE Linux Enterprise 15 (64-bit)",
	"other6xLinux64Guest":        "Linux 6.x kernel (64-bit)",
	"otherLinux64Guest":          "Other Linux (64-bit)",
	"windows2019srv_64Guest":     "Windows Server 2019 (64-bit)",
	"windows2022srv_64Guest":     "Windows Server 2022 (64-bit)",
	"windows2019srvNext_64Guest": "Windows Server 2019 Next (64-bit)",
	"windows11_64Guest":          "Windows 11 (64-bit)",
	"windows10_64Guest":          "Windows 10 (64-bit)",
	"windows8_64Guest":           "Windows 8 (64-bit)",
	"winLonghorn64Guest":         "Windows Server 2008 R2 (64-bit)",
}

// FileInfo is an entry of a datastore folder. Path is the folder it is in,
// not the entry's own path.
type FileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime string `json:"mod_time,omitempty"`
}

func (c *Client) DatastoreFiles(ctx context.Context, datastore, path string) ([]FileInfo, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	ds, err := c.finder.Datastore(ctx, datastore)
	if err != nil {
		return nil, err
	}
	var mds mo.Datastore
	if err := c.pc.RetrieveOne(ctx, ds.Reference(), []string{"browser"}, &mds); err != nil {
		return nil, err
	}
	browser := object.NewHostDatastoreBrowser(c.vc, mds.Browser)
	spec := &types.HostDatastoreBrowserSearchSpec{
		Details: &types.FileQueryFlags{FileSize: true, Modification: true, FileType: true},
	}
	task, err := browser.SearchDatastore(ctx, ds.Path(path), spec)
	if err != nil {
		return nil, err
	}
	res, err := task.WaitForResult(ctx, nil)
	if err != nil {
		return nil, err
	}
	sr, ok := res.Result.(types.HostDatastoreBrowserSearchResults)
	if !ok {
		return nil, fmt.Errorf("unexpected search result")
	}
	out := make([]FileInfo, 0, len(sr.File))
	for _, f := range sr.File {
		info := f.GetFileInfo()
		fi := FileInfo{Name: info.Path, Path: path, Size: info.FileSize}
		if info.Modification != nil {
			fi.ModTime = info.Modification.UTC().Format(time.RFC3339)
		}
		if _, ok := f.(*types.FolderFileInfo); ok {
			fi.IsDir = true
		}
		out = append(out, fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
