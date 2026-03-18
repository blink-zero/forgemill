package service

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

// DefaultAuditRetentionDays is the default number of days to retain audit logs.
const DefaultAuditRetentionDays = 90

// auditBufferSize is the capacity of the async write buffer.
const auditBufferSize = 1000

type AuditService struct {
	db      *db.DB
	entries chan *models.AuditLog
	stop    chan struct{}
	wg      sync.WaitGroup
}

func NewAuditService(database *db.DB) *AuditService {
	s := &AuditService{
		db:      database,
		entries: make(chan *models.AuditLog, auditBufferSize),
		stop:    make(chan struct{}),
	}
	s.startWriter()
	return s
}

// startWriter launches the background goroutine that persists audit entries.
func (s *AuditService) startWriter() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case entry := <-s.entries:
				if err := s.db.CreateAuditLog(entry); err != nil {
					slog.Error("audit: failed to persist log entry", "action", entry.Action, "actor", entry.Actor, "error", err)
				}
			case <-s.stop:
				// Drain remaining entries before exiting
				for {
					select {
					case entry := <-s.entries:
						if err := s.db.CreateAuditLog(entry); err != nil {
							slog.Error("audit: failed to persist log entry during shutdown", "action", entry.Action, "error", err)
						}
					default:
						return
					}
				}
			}
		}
	}()
}

// StartRetentionCleanup runs a background goroutine that cleans up old audit logs daily.
// Reads audit_retention_days from app_settings (default 90). Set to 0 to disable cleanup.
func (s *AuditService) StartRetentionCleanup() {
	if s == nil || s.db == nil {
		return
	}
	go func() {
		// Run immediately on startup, then daily
		s.runCleanup()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.runCleanup()
			case <-s.stop:
				return
			}
		}
	}()
}

// Stop signals background goroutines to exit and waits for them to drain.
func (s *AuditService) Stop() {
	if s != nil && s.stop != nil {
		close(s.stop)
		s.wg.Wait()
	}
}

func (s *AuditService) runCleanup() {
	retentionDays := DefaultAuditRetentionDays
	if settings, err := s.db.GetAllSettings(); err == nil {
		if v, ok := settings["audit_retention_days"]; ok {
			if days, err := strconv.Atoi(v); err == nil && days >= 0 {
				retentionDays = days
			}
		}
	}
	if retentionDays == 0 {
		return // Cleanup disabled
	}
	deleted, err := s.db.DeleteOldAuditLogs(retentionDays)
	if err != nil {
		slog.Error("audit: retention cleanup failed", "error", err)
	} else if deleted > 0 {
		slog.Info("audit: retention cleanup completed", "deleted", deleted, "retention_days", retentionDays)
	}
}

// Log queues an audit entry for async persistence. Non-blocking — if the buffer
// is full, logs a warning and drops the entry rather than blocking the request.
func (s *AuditService) Log(actor string, actorID *int64, action, resourceType, resourceID, ipAddress string, metadata map[string]interface{}) {
	if s == nil || s.entries == nil {
		return
	}
	meta := json.RawMessage("{}")
	if len(metadata) > 0 {
		if b, err := json.Marshal(metadata); err == nil {
			meta = b
		}
	}
	entry := &models.AuditLog{
		Actor:        actor,
		ActorID:      actorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Metadata:     meta,
		IPAddress:    ipAddress,
	}
	select {
	case s.entries <- entry:
		// Queued successfully
	default:
		// Buffer full — log warning but don't block the request
		slog.Warn("audit: buffer full, dropping entry", "action", action, "actor", actor)
	}
}

// IPFromRequest extracts the client IP from an HTTP request.
func IPFromRequest(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
