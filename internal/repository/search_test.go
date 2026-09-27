package repository

import "testing"

func TestEscapeLikePattern(t *testing.T) {
	if got, want := escapeLikePattern(`50%_\\`), `50\%\_\\\\`; got != want {
		t.Fatalf("escaped pattern=%q, want %q", got, want)
	}
}
