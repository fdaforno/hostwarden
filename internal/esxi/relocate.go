package esxi

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"hostwarden/internal/model"
)

// VM relocation between two datastores of the same host.
//
// This is a cold (or suspended) move, not Storage vMotion: the VM is stopped,
// its folder is copied to the target datastore, and it is registered there.
// The source copy is only removed after the target is registered and started.

// CheckRelocate runs the compatibility checks for moving VM id to targetDS on
// the same host. Problems are reported in the result, not as an error.
func (c *Client) CheckRelocate(ctx context.Context, id, targetDS string) (*model.MigrationCheck, error) {
	check := &model.MigrationCheck{Blockers: []string{}, Warnings: []string{}, CPUCompatible: true}
	block := func(format string, a ...any) { check.Blockers = append(check.Blockers, fmt.Sprintf(format, a...)) }
	warn := func(format string, a ...any) { check.Warnings = append(check.Warnings, fmt.Sprintf(format, a...)) }

	if c.ssh == nil {
		block("SSH must be enabled on the host")
		return check, nil
	}
	layout, vm, err := c.vmLayout(ctx, id)
	if err != nil {
		return nil, err
	}
	check.PowerState = layout.power

	sourceDS := dsFromPath(vm.VMXPath)
	if sourceDS == targetDS {
		block("The virtual machine is already on datastore %q", targetDS)
		return check, nil
	}

	for _, d := range vm.Disks {
		if path.Dir(dsPathToFS(d.Path)) != layout.dir {
			block("%s is stored outside the virtual machine folder (%s); only virtual machines kept in a single folder can be relocated", d.Label, d.Path)
		}
		check.ProvisionedGB += d.CapacityGB
	}
	if vm.SnapshotCount > 0 {
		warn("The virtual machine has snapshots: they are moved along with the machine")
	}

	size, err := dirSize(ctx, c, layout.dir)
	if err != nil {
		return nil, err
	}
	check.SizeBytes = size

	stores, err := c.Datastores(ctx)
	if err != nil {
		return nil, err
	}
	var target *model.Datastore
	for i := range stores {
		if stores[i].Name == targetDS {
			target = &stores[i]
		}
	}
	switch {
	case target == nil:
		block("Datastore %q does not exist on the host", targetDS)
	case !target.Accessible:
		block("Datastore %q is not accessible on the host", targetDS)
	default:
		check.TargetFreeBytes = target.FreeB
		if size > target.FreeB {
			block("Not enough free space on %s: %d GB needed, %d GB free", targetDS, toGB(size), toGB(target.FreeB))
		}
		targetDir := "/vmfs/volumes/" + targetDS + "/" + layout.dirName
		if out, err := c.sshRun(ctx, "[ -e "+shq(targetDir)+" ] && echo exists || echo free"); err != nil {
			return nil, err
		} else if strings.TrimSpace(out) != "free" {
			block("A folder named %q already exists on %s", layout.dirName, targetDS)
		}
	}

	for _, cd := range vm.Cdroms {
		if cd.ISO != "" {
			warn("%s uses %s: the image stays where it is", cd.Label, cd.ISO)
		}
	}

	switch {
	case layout.power == "poweredOff":
		check.RecommendedMode = model.MigrateCold
	default:
		check.RecommendedMode = model.MigrateSuspend
	}
	return check, nil
}

// RelocateVM moves VM id from its current datastore to targetDS on the same
// host. The source VM is unregistered before the target is registered; on
// failure the copy is removed and the source is put back.
func (c *Client) RelocateVM(ctx context.Context, id string, req *model.RelocateRequest, progress MigrationProgress) (*MigrationResult, error) {
	check, err := c.CheckRelocate(ctx, id, req.TargetDatastore)
	if err != nil {
		return nil, err
	}
	if len(check.Blockers) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidState, strings.Join(check.Blockers, "; "))
	}
	layout, vm, err := c.vmLayout(ctx, id)
	if err != nil {
		return nil, err
	}
	running := layout.power == "poweredOn"
	switch req.Mode {
	case model.MigrateCold, model.MigrateSuspend, model.MigrateStopStart:
	default:
		return nil, fmt.Errorf("%w: mode must be cold, suspend or stop_start", ErrInvalidState)
	}
	if running && req.Mode == model.MigrateCold {
		return nil, fmt.Errorf("%w: the virtual machine is powered on; choose suspend or stop_start", ErrInvalidState)
	}

	targetParent := "/vmfs/volumes/" + req.TargetDatastore
	targetDir := targetParent + "/" + layout.dirName
	targetVMX := targetDir + "/" + path.Base(layout.vmxFS)

	result := &MigrationResult{}
	total := check.SizeBytes
	report := func(phase string, done int64) {
		if progress != nil {
			progress(phase, done, total)
		}
	}

	// --- 1. stop the source if it is running ---
	if running {
		if req.Mode == model.MigrateSuspend {
			report("Suspending the virtual machine", 0)
			if _, err := c.SetPower(ctx, id, "suspend"); err != nil {
				return nil, err
			}
			if err := waitPower(ctx, c, id, "suspended", powerOnTimeout); err != nil {
				return nil, err
			}
		} else {
			report("Shutting down the guest operating system", 0)
			if err := shutDown(ctx, c, id); err != nil {
				return nil, err
			}
		}
		if total, err = dirSize(ctx, c, layout.dir); err != nil {
			total = check.SizeBytes
		}
	}

	// --- 2. unregister the source before copying (same host: avoids UUID conflict) ---
	report("Unregistering the virtual machine from the source datastore", total)
	if out, err := c.sshRun(ctx, "vim-cmd vmsvc/unregister "+shq(id)); err != nil {
		if running {
			_, _ = c.SetPower(ctx, id, "on")
		}
		return nil, fmt.Errorf("unregister on the source datastore: %s", remoteFailure(err, out))
	}

	newID := ""
	rollback := func(cause error) error {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		if newID != "" {
			_, _ = c.sshRun(rctx, "vim-cmd vmsvc/power.off "+shq(newID)+" >/dev/null 2>&1; vim-cmd vmsvc/unregister "+shq(newID))
		}
		if layout.dirName != "" && layout.dirName != "." && layout.dirName != "/" {
			_, _ = c.sshRun(rctx, "rm -rf "+shq(targetDir))
		}
		out, err := c.sshRun(rctx, "vim-cmd solo/registervm "+shq(layout.vmxFS))
		if err != nil {
			return fmt.Errorf("%w; the source virtual machine could NOT be registered again (%s): register %s manually", cause, remoteFailure(err, out), layout.vmxFS)
		}
		if running {
			restoredID := strings.TrimSpace(out)
			if _, err := c.SetPower(rctx, restoredID, "on"); err != nil {
				return fmt.Errorf("%w; the source virtual machine is registered again but could not be started: %v", cause, err)
			}
		}
		return fmt.Errorf("%w; the source virtual machine was restored", cause)
	}

	// --- 3. copy the folder to the target datastore ---
	report("Copying the virtual machine files", 0)
	stopProgress := watchCopy(ctx, c, targetDir, func(done int64) { report("Copying the virtual machine files", done) })
	copyCmd := fmt.Sprintf("cp -r %s %s", shq(layout.dir), shq(targetParent+"/"))
	if out, err := c.sshRun(ctx, copyCmd); err != nil {
		stopProgress()
		return nil, rollback(fmt.Errorf("copy to the target datastore failed: %s", remoteFailure(err, out)))
	}
	stopProgress()
	report("Copying the virtual machine files", total)

	// --- 4. adapt the copy ---
	report("Configuring the virtual machine on the target datastore", total)
	warns, err := adaptVMX(ctx, c, targetVMX)
	if err != nil {
		return nil, rollback(err)
	}
	result.Warnings = append(result.Warnings, warns...)

	if req.ReclaimThin && vm.SnapshotCount == 0 {
		for _, d := range vm.Disks {
			if !d.Thin {
				continue
			}
			report("Reclaiming unused space of "+d.Label, total)
			disk := targetDir + "/" + path.Base(dsPathToFS(d.Path))
			if out, err := c.sshRun(ctx, "vmkfstools -K "+shq(disk)); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s could not be re-thinned: %s", d.Label, remoteFailure(err, lastLine(out))))
			}
		}
	}

	// --- 5. register and start on the target datastore ---
	report("Registering the virtual machine on the target datastore", total)
	out, err := c.sshRun(ctx, "vim-cmd solo/registervm "+shq(targetVMX))
	if err != nil {
		return nil, rollback(fmt.Errorf("register on the target datastore: %w (output: %s)", err, strings.TrimSpace(out)))
	}
	newID = strings.TrimSpace(out)
	if _, err := strconv.Atoi(newID); err != nil {
		newID = ""
		return nil, rollback(fmt.Errorf("register on the target datastore: unexpected output %q", strings.TrimSpace(out)))
	}

	if running {
		report("Powering on the virtual machine on the target datastore", total)
		if _, err := c.SetPower(ctx, newID, "on"); err != nil {
			return nil, rollback(fmt.Errorf("power on on the target datastore: %w", err))
		}
		if err := waitPower(ctx, c, newID, "poweredOn", powerOnTimeout); err != nil {
			return nil, rollback(fmt.Errorf("power on on the target datastore: %w", err))
		}
	}

	// --- 6. retire the source (target is up: failures here are only warnings) ---
	report("Removing the virtual machine from the source datastore", total)
	if req.DeleteSource {
		if layout.dirName != "" && layout.dirName != "." && layout.dirName != "/" {
			if out, err := c.sshRun(ctx, "rm -rf "+shq(layout.dir)); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("The source files could not be deleted: %v (%s)", err, strings.TrimSpace(out)))
			}
		}
	} else {
		result.Warnings = append(result.Warnings, "The source files were kept in "+vm.VMXPath[:strings.LastIndex(vm.VMXPath, "/")])
	}

	result.NewVMID = newID
	return result, nil
}
