package repository

import "strings"

// escapeLikePattern makes user input literal for PostgreSQL LIKE/ILIKE while
// retaining the surrounding wildcards added by each query.
func escapeLikePattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
