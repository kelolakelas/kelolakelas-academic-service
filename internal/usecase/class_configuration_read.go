package usecase

import (
	"context"
	"math"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type ClassConfigurationRead struct {
	Store repository.ClassConfigurationReader
}

func (u *ClassConfigurationRead) Class(ctx context.Context, tenant, id uuid.UUID) (*domain.ClassResponse, error) {
	c, err := u.Store.Class(ctx, tenant, id)
	if err != nil {
		return nil, err
	}
	result := classResponseFrom(c)
	if c.Category != nil && c.Category.TenantID == tenant && c.Category.ID == c.CategoryID {
		result.Category = &domain.ClassCategorySummary{ID: c.Category.ID, Name: c.Category.Name}
	}
	return result, nil
}

func (u *ClassConfigurationRead) Schedules(ctx context.Context, tenant, id uuid.UUID, q domain.ListQuery) (*domain.ScheduleListResponse, error) {
	items, total, err := u.Store.Schedules(ctx, tenant, id, q)
	if err != nil {
		return nil, err
	}
	return &domain.ScheduleListResponse{Items: items, Pagination: domain.Pagination{Page: q.Page, PageSize: q.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(q.PageSize)))}}, nil
}
