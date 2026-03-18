package service

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

type BulkDeployService struct {
	db     *db.DB
	deploy *DeployService
}

func NewBulkDeployService(db *db.DB, deploy *DeployService) *BulkDeployService {
	return &BulkDeployService{db: db, deploy: deploy}
}

type BulkDeployRequest struct {
	Name       string           `json:"name"`
	Parallel   bool             `json:"parallel"`
	TemplateID int64            `json:"template_id"`
	TargetID   int64            `json:"target_id"`
	BaseConfig DeployRequest    `json:"base_config"`
	VMs        []BulkVMInstance `json:"vms"`
}

type BulkVMInstance struct {
	VMName    string `json:"vm_name"`
	IPAddress string `json:"ip_address,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
}

func (s *BulkDeployService) Start(req *BulkDeployRequest, userID int64) (*models.BulkDeployment, error) {
	if len(req.VMs) == 0 {
		return nil, fmt.Errorf("at least one VM is required")
	}
	// MED-22: Enforce max VM count in service layer (not just handler)
	if len(req.VMs) > 50 {
		return nil, fmt.Errorf("bulk deployment limited to 50 VMs per request")
	}

	// Expand name patterns upfront
	expandedVMs := expandNamePatterns(req.VMs)

	// V3-M16: Wrap parent + child deployment creation in a transaction
	// to prevent inconsistent state on partial failure.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	bulk := &models.BulkDeployment{
		Name:      req.Name,
		Status:    "pending",
		TotalVMs:  len(expandedVMs),
		Parallel:  req.Parallel,
		CreatedBy: userID,
	}

	res, err := tx.Exec(
		`INSERT INTO bulk_deployments (name, status, total_vms, parallel, created_by) VALUES (?, ?, ?, ?, ?)`,
		bulk.Name, bulk.Status, bulk.TotalVMs, bulk.Parallel, bulk.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("create bulk deployment: %w", err)
	}
	bulk.ID, err = res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get bulk deployment ID: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Build deploy requests (individual deployments will be created by deploy.Start())
	var deployReqs []DeployRequest
	for _, vmInst := range expandedVMs {
		deployReq := req.BaseConfig
		deployReq.TemplateID = req.TemplateID
		deployReq.TargetID = req.TargetID
		deployReq.VMName = vmInst.VMName
		deployReq.BulkDeploymentID = &bulk.ID
		if vmInst.IPAddress != "" {
			deployReq.IPAddress = vmInst.IPAddress
		}
		if vmInst.Hostname != "" {
			deployReq.Hostname = vmInst.Hostname
		}
		deployReqs = append(deployReqs, deployReq)
	}

	go s.runBulk(bulk.ID, deployReqs, req.Parallel, userID)

	return bulk, nil
}

func (s *BulkDeployService) runBulk(bulkID int64, deployReqs []DeployRequest, parallel bool, userID int64) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in bulk deployment goroutine", "bulk_id", bulkID, "panic", r)
			s.db.UpdateBulkDeploymentStatus(bulkID, "failed")
		}
	}()
	s.db.UpdateBulkDeploymentStatus(bulkID, "running")

	if parallel {
		s.runParallel(bulkID, deployReqs, userID)
	} else {
		s.runSequential(bulkID, deployReqs, userID)
	}

	// Check final status
	bulk, err := s.db.GetBulkDeployment(bulkID)
	if err != nil {
		slog.Error("failed to get bulk deployment status", "bulk_id", bulkID, "error", err)
		return
	}

	// 5.15: Distinguish between complete failure, partial failure, and success
	if bulk.FailedVMs > 0 && bulk.FailedVMs == bulk.TotalVMs {
		s.db.UpdateBulkDeploymentStatus(bulkID, "failed")
	} else if bulk.FailedVMs > 0 {
		s.db.UpdateBulkDeploymentStatus(bulkID, "partial_failure")
	} else {
		s.db.UpdateBulkDeploymentStatus(bulkID, "completed")
	}
}

func (s *BulkDeployService) runSequential(bulkID int64, deployReqs []DeployRequest, userID int64) {
	completed := 0
	failed := 0
	for _, req := range deployReqs {
		_, err := s.deploy.Start(&req, userID)
		if err != nil {
			failed++
			slog.Error("bulk sub-deploy failed", "vm_name", req.VMName, "error", err)
		} else {
			completed++
		}
		s.db.UpdateBulkDeploymentProgress(bulkID, completed, failed)
	}
}

func (s *BulkDeployService) runParallel(bulkID int64, deployReqs []DeployRequest, userID int64) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	completed := 0
	failed := 0

	// V3-H7: Limit concurrent goroutines with a semaphore to prevent resource exhaustion
	sem := make(chan struct{}, 10)
	for _, req := range deployReqs {
		wg.Add(1)
		sem <- struct{}{} // acquire semaphore slot
		go func(r DeployRequest) {
			defer func() { <-sem }() // release semaphore slot
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("panic in bulk sub-deploy goroutine", "vm_name", r.VMName, "panic", rec)
					mu.Lock()
					failed++
					s.db.UpdateBulkDeploymentProgress(bulkID, completed, failed)
					mu.Unlock()
				}
			}()
			_, err := s.deploy.Start(&r, userID)
			mu.Lock()
			if err != nil {
				failed++
				slog.Error("bulk sub-deploy failed", "vm_name", r.VMName, "error", err)
			} else {
				completed++
			}
			s.db.UpdateBulkDeploymentProgress(bulkID, completed, failed)
			mu.Unlock()
		}(req)
	}

	wg.Wait()
}

func (s *BulkDeployService) Get(id int64) (*models.BulkDeployment, error) {
	bulk, err := s.db.GetBulkDeployment(id)
	if err != nil {
		return nil, err
	}
	deployments, err := s.db.ListDeploymentsByBulk(id)
	if err != nil {
		return nil, err
	}
	if deployments == nil {
		deployments = []models.Deployment{}
	}
	bulk.Deployments = deployments
	return bulk, nil
}

func (s *BulkDeployService) List() ([]models.BulkDeployment, error) {
	return s.db.ListBulkDeployments()
}

// SEC-2: ListByUser returns bulk deployments owned by a specific user.
func (s *BulkDeployService) ListByUser(userID int64) ([]models.BulkDeployment, error) {
	return s.db.ListBulkDeploymentsByUser(userID)
}

// expandNamePatterns expands {n} patterns in VM names.
// For example, "web-{n}" with 3 VMs becomes "web-1", "web-2", "web-3".
func expandNamePatterns(vms []BulkVMInstance) []BulkVMInstance {
	result := []BulkVMInstance{}
	for i, vm := range vms {
		name := strings.ReplaceAll(vm.VMName, "{n}", fmt.Sprintf("%d", i+1))
		result = append(result, BulkVMInstance{
			VMName:    name,
			IPAddress: vm.IPAddress,
			Hostname:  vm.Hostname,
		})
	}
	return result
}
