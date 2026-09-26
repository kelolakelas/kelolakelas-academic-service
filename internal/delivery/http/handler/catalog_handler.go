package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/gorm"
)

type CatalogHandler struct{ usecase usecase.CatalogUsecase }

func NewCatalogHandler(uc usecase.CatalogUsecase) *CatalogHandler {
	return &CatalogHandler{usecase: uc}
}

// GetCatalogClass godoc
// @Summary Get a public catalog class
// @Tags Catalog
// @Produce json
// @Param id path string true "Class UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.CatalogItem}
// @Failure 404 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/catalog/classes/{id} [get]
func (h *CatalogHandler) GetClass(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Class not found", "data": nil})
		return
	}
	item, err := h.usecase.GetClass(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrCatalogClosed) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Public catalog is closed", "data": nil})
			return
		}
		if errors.Is(err, domain.ErrCatalogPolicyUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Public catalog policy unavailable", "data": nil})
			return
		}
		status := http.StatusInternalServerError
		if err == gorm.ErrRecordNotFound {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"status": "error", "message": "Failed to fetch public class", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Public class fetched successfully", "data": item})
}

// ListCatalogClasses godoc
// @Summary List public catalog classes
// @Description Lists active classes. Coordinates must be supplied together; when supplied, only tenants with a location within radius_km are returned.
// @Tags Catalog
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Items per page" default(20)
// @Param search query string false "Search class name"
// @Param category_id query string false "Category UUID"
// @Param tenant_id query string false "Tenant UUID"
// @Param type query string false "private|group"
// @Param min_price query integer false "Minimum price in smallest currency unit"
// @Param max_price query integer false "Maximum price in smallest currency unit"
// @Param latitude query number false "User latitude"
// @Param longitude query number false "User longitude"
// @Param radius_km query number false "Search radius in kilometres" default(25) maximum(100)
// @Param sort query string false "distance_asc|name_asc|price_asc|price_desc|newest" default(newest)
// @Success 200 {object} domain.HTTPResponse{data=domain.CatalogListResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 422 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/catalog/classes [get]
func (h *CatalogHandler) ListClasses(c *gin.Context) {
	query := domain.CatalogQuery{ListQuery: domain.ListQuery{Page: 1, PageSize: 20, Search: c.Query("search")}, RadiusKM: 25, Sort: "newest"}
	for key, target := range map[string]*int{"page": &query.Page, "page_size": &query.PageSize} {
		if value := c.Query(key); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || (key == "page_size" && parsed > 100) {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid pagination", "data": nil})
				return
			}
			*target = parsed
		}
	}
	for key, target := range map[string]**float64{"latitude": &query.Latitude, "longitude": &query.Longitude} {
		if value := c.Query(key); value != "" {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid coordinates", "data": nil})
				return
			}
			*target = &parsed
		}
	}
	for key, target := range map[string]**uuid.UUID{"category_id": &query.CategoryID, "tenant_id": &query.TenantID} {
		if value := c.Query(key); value != "" {
			parsed, err := uuid.Parse(value)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid " + key, "data": nil})
				return
			}
			*target = &parsed
		}
	}
	query.Type = c.Query("type")
	for key, target := range map[string]**int64{"min_price": &query.MinPrice, "max_price": &query.MaxPrice} {
		if value := c.Query(key); value != "" {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid " + key, "data": nil})
				return
			}
			*target = &parsed
		}
	}
	if value := c.Query("radius_km"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid radius", "data": nil})
			return
		}
		query.RadiusKM = parsed
	}
	if value := c.Query("sort"); value != "" {
		query.Sort = value
	}
	result, err := h.usecase.ListClasses(c.Request.Context(), query)
	if err != nil {
		status := http.StatusInternalServerError
		if err == domain.ErrInvalidCatalogQuery {
			status = http.StatusBadRequest
		}
		if errors.Is(err, domain.ErrCatalogPolicyUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Public catalog policy unavailable", "data": nil})
			return
		}
		c.JSON(status, gin.H{"status": "error", "message": "Failed to fetch public classes", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Public classes fetched successfully", "data": result})
}
