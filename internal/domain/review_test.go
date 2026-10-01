package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPublicReviewExcludesIdentity(t *testing.T) {
	response := ReviewListResponse{Items: []PublicReview{{Rating: 5, Comment: "great", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"student", "parent", "email", "enrollment", "first_name", "last_name"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("public response contains %s: %s", forbidden, data)
		}
	}
}
