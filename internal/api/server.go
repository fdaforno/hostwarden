package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"hostwarden/internal/esxi"
	"hostwarden/internal/model"
	"hostwarden/internal/store"
)

type isoStatus struct {
	URL       string    `json:"url"`
	Name      string    `json:"name"`
	Folder    string    `json:"folder"`
	Datastore string    `json:"datastore"`
	Status    string    `json:"status"` // running | ok | error
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at"`
	DoneAt    time.Time `json:"done_at,omitempty"`
}

type Server struct {
	store         *store.Store
	mgr           *esxi.Manager
	logger        *slog.Logger
	sshKnownHosts string
	isoStatuses   sync.Map // key string -> *isoStatus
	jobs          sync.Map // id string -> *job
	cache         *inventoryCache
	tasks         *taskLog
	running       sync.Map // schedule id -> struct{}: schedules being executed right now

	// Aggregated alarms (disk health so far), cached server-side so the
	// root summary can poll without opening an SSH session per host.
	alarmMu  sync.Mutex
	alarmVal []model.Alarm
	alarmAt  time.Time

	// Per-datastore disk health, populated by healthRefreshLoop every
	// 10 minutes (never on UI request).
	health *healthCache
}

func NewServer(st *store.Store, mgr *esxi.Manager, logger *slog.Logger, sshKnownHosts string) *Server {
	s := &Server{store: st, mgr: mgr, logger: logger, sshKnownHosts: sshKnownHosts, cache: newInventoryCache(), tasks: &taskLog{}, health: newHealthCache()}
	go s.schedulerLoop()
	go s.healthRefreshLoop()
	return s
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})

	// auth
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.auth(s.handleLogout))
	mux.HandleFunc("GET /api/v1/auth/me", s.auth(s.handleMe))
	mux.HandleFunc("POST /api/v1/auth/password", s.auth(s.handleChangePassword))

	// OIDC browser redirects (no auth: the identity provider authenticates)
	mux.HandleFunc("GET /auth/oidc/login", s.handleOIDCLogin)
	mux.HandleFunc("GET /auth/oidc/callback", s.handleOIDCCallback)

	// OIDC configuration and role definitions (administrators only)
	mux.HandleFunc("GET /api/v1/oidc/config", s.auth(s.admin(s.handleGetOIDCConfig)))
	mux.HandleFunc("PUT /api/v1/oidc/config", s.auth(s.admin(s.handlePutOIDCConfig)))
	mux.HandleFunc("GET /api/v1/roles", s.auth(s.handleListRoles))
	mux.HandleFunc("POST /api/v1/roles", s.auth(s.admin(s.handlePutRole)))
	mux.HandleFunc("PUT /api/v1/roles/{id}", s.auth(s.admin(s.handlePutRole)))
	mux.HandleFunc("DELETE /api/v1/roles/{id}", s.auth(s.admin(s.handleDeleteRole)))

	// users (administrators only)
	mux.HandleFunc("GET /api/v1/users", s.auth(s.admin(s.handleListUsers)))
	mux.HandleFunc("POST /api/v1/users", s.auth(s.admin(s.handleCreateUser)))
	mux.HandleFunc("PUT /api/v1/users/{id}", s.auth(s.admin(s.handleUpdateUser)))
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.auth(s.admin(s.handleDeleteUser)))

	// hosts
	mux.HandleFunc("GET /api/v1/hosts", s.auth(s.handleListHosts))
	mux.HandleFunc("POST /api/v1/hosts", s.auth(s.handleCreateHost))
	mux.HandleFunc("GET /api/v1/hosts/{hid}", s.auth(s.handleGetHost))
	mux.HandleFunc("PUT /api/v1/hosts/{hid}", s.auth(s.handleUpdateHost))
	mux.HandleFunc("DELETE /api/v1/hosts/{hid}", s.auth(s.handleDeleteHost))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/test", s.auth(s.handleTestHost))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/datastores", s.auth(s.handleListDatastores))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/datastores/{ds}/health", s.auth(s.handleDatastoreHealth))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/networks", s.auth(s.handleListNetworks))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/guest-os", s.auth(s.handleListGuestOS))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/datastores/{ds}/files", s.auth(s.handleDatastoreFiles))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/datastores/{ds}/files/upload", s.auth(s.handleUploadFile))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/datastores/{ds}/files/download", s.auth(s.admin(s.handleDownloadFile)))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/datastores/{ds}/iso/download", s.auth(s.handleDownloadISO))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/datastores/{ds}/iso/download/status", s.auth(s.handleDownloadISOStatus))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/datastores/{ds}/cloud-image/download", s.auth(s.handleDownloadCloudImage))
	mux.HandleFunc("GET /api/v1/cloud-images", s.auth(s.handleListCloudImages))
	mux.HandleFunc("DELETE /api/v1/hosts/{hid}/datastores/{ds}/files", s.auth(s.handleDeleteFile))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/esxcli", s.auth(s.handleEsxcli))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/power", s.auth(s.handleHostPower))

	// vms
	mux.HandleFunc("GET /api/v1/hosts/{hid}/vms", s.auth(s.handleListVMs))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms", s.auth(s.handleCreateVM))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/register", s.auth(s.handleRegisterVM))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/vms/{vmid}", s.auth(s.handleGetVM))
	mux.HandleFunc("PUT /api/v1/hosts/{hid}/vms/{vmid}", s.auth(s.handleUpdateVM))
	mux.HandleFunc("DELETE /api/v1/hosts/{hid}/vms/{vmid}", s.auth(s.handleDeleteVM))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/power", s.auth(s.handlePowerVM))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/vms/{vmid}/console", s.auth(s.handleVMConsole))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/migrate/check", s.auth(s.handleMigrateCheck))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/migrate", s.auth(s.handleMigrate))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/relocate/check", s.auth(s.handleRelocateCheck))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/relocate", s.auth(s.handleRelocate))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/clone", s.auth(s.handleCloneVM))
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.auth(s.handleJobStatus))
	mux.HandleFunc("GET /api/v1/tasks", s.auth(s.handleListTasks))
	mux.HandleFunc("GET /api/v1/alarms", s.auth(s.handleListAlarms))
	mux.HandleFunc("POST /api/v1/alarms/refresh", s.auth(s.handleRefreshAlarms))

	// backups and schedules
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/backup", s.auth(s.handleBackupVM))
	mux.HandleFunc("GET /api/v1/backups", s.auth(s.handleListBackups))
	mux.HandleFunc("DELETE /api/v1/hosts/{hid}/backups/{ds}/{vm}/{stamp}", s.auth(s.handleDeleteBackup))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/backups/{ds}/{vm}/{stamp}/restore", s.auth(s.handleRestoreBackup))
	mux.HandleFunc("GET /api/v1/schedules", s.auth(s.handleListSchedules))
	mux.HandleFunc("POST /api/v1/schedules", s.auth(s.handlePutSchedule))
	mux.HandleFunc("PUT /api/v1/schedules/{id}", s.auth(s.handlePutSchedule))
	mux.HandleFunc("DELETE /api/v1/schedules/{id}", s.auth(s.handleDeleteSchedule))
	mux.HandleFunc("POST /api/v1/schedules/{id}/run", s.auth(s.handleRunSchedule))

	// host configuration
	mux.HandleFunc("GET /api/v1/hosts/{hid}/services", s.auth(s.handleListServices))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/services/{key}", s.auth(s.handleSetService))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/maintenance", s.auth(s.handleMaintenance))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/network", s.auth(s.handleGetNetwork))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/network/portgroups", s.auth(s.handleAddPortGroup))
	mux.HandleFunc("PUT /api/v1/hosts/{hid}/network/portgroups/{name}", s.auth(s.handleSetPortGroup))
	mux.HandleFunc("DELETE /api/v1/hosts/{hid}/network/portgroups/{name}", s.auth(s.handleRemovePortGroup))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/network/vswitches", s.auth(s.handleAddVSwitch))
	mux.HandleFunc("DELETE /api/v1/hosts/{hid}/network/vswitches/{name}", s.auth(s.handleRemoveVSwitch))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/time", s.auth(s.handleGetTime))
	mux.HandleFunc("PUT /api/v1/hosts/{hid}/time", s.auth(s.handleSetTime))

	// monitoring
	mux.HandleFunc("GET /api/v1/hosts/{hid}/performance", s.auth(s.handlePerformance))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/events", s.auth(s.handleEvents))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/vms/{vmid}/performance", s.auth(s.handlePerformance))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/vms/{vmid}/events", s.auth(s.handleEvents))
	mux.HandleFunc("GET /api/v1/hosts/{hid}/vms/{vmid}/snapshots", s.auth(s.handleListSnapshots))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/snapshots", s.auth(s.handleCreateSnapshot))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/snapshots/remove-all", s.auth(s.handleRemoveAllSnapshots))
	mux.HandleFunc("POST /api/v1/hosts/{hid}/vms/{vmid}/snapshots/{snapid}/revert", s.auth(s.handleRevertSnapshot))
	mux.HandleFunc("DELETE /api/v1/hosts/{hid}/vms/{vmid}/snapshots/{snapid}", s.auth(s.handleDeleteSnapshot))

	return s.logMiddleware(s.recordTasks(s.invalidateOnWrite(mux)))
}

// auth wraps handlers requiring a valid bearer token.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("Authorization")
		tok = strings.TrimPrefix(tok, "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token") // convenience for downloads
		}
		user, err := s.store.UserByToken(tok)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hostwarden"`)
			errJSON(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if ref := taskOf(r); ref != nil {
			s.tasks.update(ref.task, func(t *model.Task) { t.Initiator = user.Username })
		}
		if s.changesSomething(r) && !s.hasPermission(user, s.permissionFor(r)) {
			errJSON(w, http.StatusForbidden, "insufficient permissions for this action")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		h(w, r.WithContext(ctx))
	}
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(rw, r)
		s.logger.Info("http", "method", r.Method, "path", r.URL.Path, "status", rw.code, "dur", time.Since(start).Round(time.Millisecond))
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

// Unwrap lets http.ResponseController reach the underlying writer (needed to
// hijack the connection for the console WebSocket).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}
