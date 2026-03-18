package service

import (
	"encoding/json"
	"fmt"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

type BlueprintService struct {
	db     *db.DB
	deploy *DeployService
}

func NewBlueprintService(db *db.DB, deploy *DeployService) *BlueprintService {
	return &BlueprintService{db: db, deploy: deploy}
}

func (s *BlueprintService) List() ([]models.Blueprint, error) {
	return s.db.ListBlueprints()
}

func (s *BlueprintService) Get(id int64) (*models.Blueprint, error) {
	return s.db.GetBlueprint(id)
}

func (s *BlueprintService) Create(b *models.Blueprint) error {
	if b.ConfigJSON == "" {
		b.ConfigJSON = "{}"
	}
	return s.db.CreateBlueprint(b)
}

func (s *BlueprintService) Update(b *models.Blueprint) error {
	return s.db.UpdateBlueprint(b)
}

func (s *BlueprintService) Delete(id int64) error {
	return s.db.DeleteBlueprint(id)
}

func (s *BlueprintService) DeployFromBlueprint(blueprintID int64, vmName string, userID int64) (*DeployResponse, error) {
	bp, err := s.db.GetBlueprint(blueprintID)
	if err != nil {
		return nil, fmt.Errorf("blueprint not found: %w", err)
	}

	var req DeployRequest
	if err := json.Unmarshal([]byte(bp.ConfigJSON), &req); err != nil {
		return nil, fmt.Errorf("invalid blueprint config: %w", err)
	}

	if vmName != "" {
		req.VMName = vmName
	}
	if bp.TemplateID != nil {
		req.TemplateID = *bp.TemplateID
	}
	if bp.TargetID != nil {
		req.TargetID = *bp.TargetID
	}

	return s.deploy.Start(&req, userID)
}
