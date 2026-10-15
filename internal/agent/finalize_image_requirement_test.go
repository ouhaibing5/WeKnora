package agent

import (
	"strings"
	"testing"
)

func TestFinalAnswerImageRequirement(t *testing.T) {
	if got := finalAnswerImageRequirement(false); got != "" {
		t.Fatalf("text-only tool results should not add an image requirement: %q", got)
	}

	got := finalAnswerImageRequirement(true)
	for _, required := range []string{
		"Include an image ONLY if it directly and specifically supports",
		"include NO images",
		"ASCII half-width parentheses",
		"directly relevant",
	} {
		if !strings.Contains(got, required) {
			t.Fatalf("expected %q in final-answer image requirement:\n%s", required, got)
		}
	}

	// Relevance-gated: the old "must include at least one" mandate must be gone,
	// so a text-only answer is valid when nothing is directly relevant.
	for _, forbidden := range []string{
		"MUST include at least one",
		"relevant by default",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("old image mandate %q should be removed:\n%s", forbidden, got)
		}
	}
}
