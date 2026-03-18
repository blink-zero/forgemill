package service

import (
	"context"
	"fmt"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
)

type TemplateService struct {
	db      *db.DB
	targets *TargetService
}

func NewTemplateService(db *db.DB, targets *TargetService) *TemplateService {
	return &TemplateService{db: db, targets: targets}
}

func (s *TemplateService) List() ([]models.Template, error) {
	return s.db.ListTemplates()
}

func (s *TemplateService) Get(id int64) (*models.Template, error) {
	return s.db.GetTemplate(id)
}

func (s *TemplateService) ListByTarget(targetID int64) ([]models.Template, error) {
	return s.db.ListTemplatesByTarget(targetID)
}

func (s *TemplateService) GetDetail(ctx context.Context, id int64) (*provider.TemplateDetail, error) {
	tpl, err := s.db.GetTemplate(id)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}

	p, err := s.targets.GetProvider(tpl.TargetID)
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	return p.GetTemplateDetail(ctx, tpl.Moref)
}

func (s *TemplateService) DeletePreview(id int64) (*db.TemplateDeletePreview, error) {
	tpl, err := s.db.GetTemplate(id)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}
	_ = tpl
	return s.db.GetTemplateDeletePreview(id)
}

// Delete removes a template. If destroy is true, also removes it from the hypervisor.
// If keepVMs is true, managed VMs are unlinked instead of deleted.
func (s *TemplateService) Delete(ctx context.Context, id int64, destroy bool, keepVMs bool) error {
	tpl, err := s.db.GetTemplate(id)
	if err != nil {
		return fmt.Errorf("template not found: %w", err)
	}

	if destroy {
		p, err := s.targets.GetProvider(tpl.TargetID)
		if err != nil {
			return fmt.Errorf("get provider: %w", err)
		}
		defer p.Disconnect()

		if err := p.Connect(ctx); err != nil {
			return fmt.Errorf("connect: %w", err)
		}

		if err := p.DeleteVM(ctx, tpl.Moref); err != nil {
			return fmt.Errorf("delete from hypervisor: %w", err)
		}
	}

	return s.db.DeleteTemplate(id, keepVMs)
}
