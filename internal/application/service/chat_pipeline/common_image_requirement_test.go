package chatpipeline

import (
	"strings"
	"testing"
)

func TestAppendRetrievedImageOutputRequirement(t *testing.T) {
	base := "Answer from retrieved evidence."
	withImage := appendRetrievedImageOutputRequirement(
		base,
		"context\n![流程图](resource://AbCdEfGhIjKlMnOpQrStUv)",
	)
	for _, required := range []string{
		base,
		"Include an image ONLY when it directly and specifically supports",
		"include NO images",
		"copy the complete Markdown image syntax and its URL verbatim",
		"ASCII half-width parentheses",
		"immediately after the paragraph it supports",
	} {
		if !strings.Contains(withImage, required) {
			t.Fatalf("expected %q in dynamic image requirement:\n%s", required, withImage)
		}
	}

	// Relevance-gated: the old "must include at least one" mandate must be gone.
	for _, forbidden := range []string{
		"MUST include at least one",
		"relevant by default",
	} {
		if strings.Contains(withImage, forbidden) {
			t.Fatalf("old image mandate %q should be removed:\n%s", forbidden, withImage)
		}
	}

	withoutImage := appendRetrievedImageOutputRequirement(base, "text-only retrieved context")
	if withoutImage != base {
		t.Fatalf("text-only context should not change the user turn: %q", withoutImage)
	}
}
