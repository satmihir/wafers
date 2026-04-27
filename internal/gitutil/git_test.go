package gitutil

import (
	"context"
	"testing"
)

func TestValidateBranch(t *testing.T) {
	ctx := context.Background()
	valid := []string{"agent/foo", "foo", "feature.with-dots"}
	for _, branch := range valid {
		if err := ValidateBranch(ctx, branch); err != nil {
			t.Fatalf("ValidateBranch(%q) returned error: %v", branch, err)
		}
	}
	invalid := []string{"", "bad branch", "foo..bar", "-bad"}
	for _, branch := range invalid {
		if err := ValidateBranch(ctx, branch); err == nil {
			t.Fatalf("ValidateBranch(%q) returned nil", branch)
		}
	}
}
