package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"hostwarden/internal/esxi"
	"hostwarden/internal/model"
)

// migrationClients resolves the source (path) and target (body) hosts of a migration request.
func (s *Server) migrationClients(w http.ResponseWriter, r *http.Request) (src, dst *esxi.Client, req *model.MigrateRequest, ok bool) {
	src, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return nil, nil, nil, false
	}
	req = &model.MigrateRequest{}
	if !decodeBody(w, r, req) {
		return nil, nil, nil, false
	}
	if req.TargetHostID == "" || req.TargetDatastore == "" {
		errJSON(w, http.StatusBadRequest, "target_host_id and target_datastore are required")
		return nil, nil, nil, false
	}
	if !vmxValueRE.MatchString(req.TargetDatastore) {
		errJSON(w, http.StatusBadRequest, "invalid target_datastore")
		return nil, nil, nil, false
	}
	if _, err := s.store.Host(req.TargetHostID); err != nil {
		apiError(w, err)
		return nil, nil, nil, false
	}
	dst, err = s.mgr.Client(req.TargetHostID)
	if err != nil {
		apiError(w, err)
		return nil, nil, nil, false
	}
	return src, dst, req, true
}

func (s *Server) handleMigrateCheck(w http.ResponseWriter, r *http.Request) {
	src, dst, req, ok := s.migrationClients(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	check, err := esxi.CheckMigration(ctx, src, dst, r.PathValue("vmid"), req.TargetDatastore)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, check)
}

func (s *Server) handleMigrate(w http.ResponseWriter, r *http.Request) {
	src, dst, req, ok := s.migrationClients(w, r)
	if !ok {
		return
	}
	switch req.Mode {
	case model.MigrateCold, model.MigrateSuspend, model.MigrateStopStart:
	default:
		errJSON(w, http.StatusBadRequest, "mode must be cold, suspend or stop_start")
		return
	}
	vmid := r.PathValue("vmid")
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	vm, err := src.GetVM(ctx, vmid)
	cancel()
	if err != nil {
		apiError(w, err)
		return
	}

	init := model.JobStatus{Kind: "migrate", VMName: vm.Name, SourceHostID: r.PathValue("hid"), TargetHostID: req.TargetHostID}
	writeJSON(w, http.StatusAccepted, s.startJob(r, init, func(ctx context.Context, j *job) (jobResult, error) {
		res, err := esxi.MigrateVM(ctx, src, dst, vmid, req, func(phase string, done, total int64) {
			j.update(func(st *model.JobStatus) { st.Phase, st.BytesDone, st.BytesTotal = phase, done, total })
		})
		if err != nil {
			return jobResult{}, err
		}
		return jobResult{NewVMID: res.NewVMID, Warnings: res.Warnings}, nil
	}))
}

func (s *Server) handleRelocateCheck(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	req := &model.RelocateRequest{}
	if !decodeBody(w, r, req) {
		return
	}
	if req.TargetDatastore == "" {
		errJSON(w, http.StatusBadRequest, "target_datastore is required")
		return
	}
	if !vmxValueRE.MatchString(req.TargetDatastore) {
		errJSON(w, http.StatusBadRequest, "invalid target_datastore")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	check, err := c.CheckRelocate(ctx, r.PathValue("vmid"), req.TargetDatastore)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, check)
}

func (s *Server) handleRelocate(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	req := &model.RelocateRequest{}
	if !decodeBody(w, r, req) {
		return
	}
	if req.TargetDatastore == "" {
		errJSON(w, http.StatusBadRequest, "target_datastore is required")
		return
	}
	if !vmxValueRE.MatchString(req.TargetDatastore) {
		errJSON(w, http.StatusBadRequest, "invalid target_datastore")
		return
	}
	switch req.Mode {
	case model.MigrateCold, model.MigrateSuspend, model.MigrateStopStart:
	default:
		errJSON(w, http.StatusBadRequest, "mode must be cold, suspend or stop_start")
		return
	}
	vmid := r.PathValue("vmid")
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	vm, err := c.GetVM(ctx, vmid)
	cancel()
	if err != nil {
		apiError(w, err)
		return
	}

	hid := r.PathValue("hid")
	init := model.JobStatus{Kind: "relocate", VMName: vm.Name, SourceHostID: hid, TargetHostID: hid}
	writeJSON(w, http.StatusAccepted, s.startJob(r, init, func(ctx context.Context, j *job) (jobResult, error) {
		res, err := c.RelocateVM(ctx, vmid, req, func(phase string, done, total int64) {
			j.update(func(st *model.JobStatus) { st.Phase, st.BytesDone, st.BytesTotal = phase, done, total })
		})
		if err != nil {
			return jobResult{}, err
		}
		return jobResult{NewVMID: res.NewVMID, Warnings: res.Warnings}, nil
	}))
}

// handleCloneVM starts a clone of a powered-off VM on its own host.
func (s *Server) handleCloneVM(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	req := &model.CloneRequest{}
	if !decodeBody(w, r, req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !vmxValueRE.MatchString(req.Name) || strings.HasPrefix(req.Name, ".") {
		errJSON(w, http.StatusBadRequest, "name must be 1-80 characters without quotes or slashes")
		return
	}
	if req.Datastore != "" && !vmxValueRE.MatchString(req.Datastore) {
		errJSON(w, http.StatusBadRequest, "invalid datastore")
		return
	}
	hid, vmid := r.PathValue("hid"), r.PathValue("vmid")
	init := model.JobStatus{Kind: "clone", VMName: req.Name, SourceHostID: hid, TargetHostID: hid}
	writeJSON(w, http.StatusAccepted, s.startJob(r, init, func(ctx context.Context, j *job) (jobResult, error) {
		newID, err := c.CloneVM(ctx, vmid, req, func(phase string) {
			j.update(func(st *model.JobStatus) { st.Phase = phase })
		})
		return jobResult{NewVMID: newID}, err
	}))
}
