package domain

// Query limits keep pagination offsets and search patterns bounded before they
// reach the database. MaxSearchLength matches the varchar(255) academic names
// and is also the documented maximum for list searches.
const (
	MaxPage         = 1000
	MaxPageSize     = 100
	MaxSearchLength = 255
	MaxInteger      = 2147483647
)
