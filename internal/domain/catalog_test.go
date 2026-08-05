package domain

import (
	"testing"

	"github.com/google/uuid"
)

func TestCatalogQueryValidate(t *testing.T) {
	latitude, longitude := -6.2, 106.8
	categoryID := uuid.New()
	minPrice, maxPrice := int64(100), int64(200)
	tests := []struct {
		name    string
		query   CatalogQuery
		wantErr bool
	}{
		{name: "default catalog", query: CatalogQuery{RadiusKM: 25, Sort: "newest"}},
		{name: "distance search", query: CatalogQuery{Latitude: &latitude, Longitude: &longitude, RadiusKM: 25, Sort: "distance_asc"}},
		{name: "missing longitude", query: CatalogQuery{Latitude: &latitude, RadiusKM: 25, Sort: "distance_asc"}, wantErr: true},
		{name: "radius too large", query: CatalogQuery{RadiusKM: 101, Sort: "newest"}, wantErr: true},
		{name: "invalid sort", query: CatalogQuery{RadiusKM: 25, Sort: "closest"}, wantErr: true},
		{name: "all filters", query: CatalogQuery{RadiusKM: 25, Sort: "price_asc", CategoryID: &categoryID, Type: "group", MinPrice: &minPrice, MaxPrice: &maxPrice}},
		{name: "invalid type", query: CatalogQuery{RadiusKM: 25, Sort: "newest", Type: "hybrid"}, wantErr: true},
		{name: "min above max", query: CatalogQuery{RadiusKM: 25, Sort: "newest", MinPrice: &maxPrice, MaxPrice: &minPrice}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if (test.query.Validate() != nil) != test.wantErr {
				t.Fatalf("Validate() error mismatch")
			}
		})
	}
}
