package wikilink

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// linkPattern matches a wiki link — `[[slug]]` or `[[slug|title]]`. The inner
// class excludes `]` so the match stops at the first closing bracket, which is
// exactly how the web UI's citationMarkdown parser tokenises a wiki link.
var linkPattern = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Rewriter flattens wiki links to their display text. It is stateless and safe
// for concurrent use; create one per request.
type Rewriter struct {
	mode Mode
}

// NewRewriter returns a Rewriter for mode. Only ModeText rewrites; ModeLink
// yields a disabled Rewriter whose methods return content unchanged.
func NewRewriter(mode Mode) *Rewriter { return &Rewriter{mode: mode} }

// Enabled reports whether this Rewriter flattens wiki links.
func (w *Rewriter) Enabled() bool { return w != nil && w.mode == ModeText }

// String flattens every wiki link in content to its display text. Content
// without wiki links is returned unchanged.
func (w *Rewriter) String(_ context.Context, content string) string {
	if !w.Enabled() || content == "" {
		return content
	}
	return linkPattern.ReplaceAllStringFunc(content, func(match string) string {
		// match is "[[inner]]"; drop the two brackets on each side.
		return DisplayText(match[2 : len(match)-2])
	})
}

// DisplayText returns the human-readable label for a wiki link's inner text
// (`slug` or `slug|title`). It matches the web UI's link label exactly: the
// title when one is present, otherwise the slug with its type prefix stripped.
func DisplayText(inner string) string {
	if pipeIdx := strings.Index(inner, "|"); pipeIdx >= 0 {
		if title := strings.TrimSpace(inner[pipeIdx+1:]); title != "" {
			return title
		}
		return HumanizeSlug(strings.TrimSpace(inner[:pipeIdx]))
	}
	return HumanizeSlug(strings.TrimSpace(inner))
}

// HumanizeSlug strips a slug's type prefix (`entity/foo-bar` -> `foo-bar`),
// matching how the web UI labels a bare `[[slug]]` that carries no title. A
// slug with no prefix, or an empty name after the prefix, is returned as-is.
func HumanizeSlug(slug string) string {
	if idx := strings.Index(slug, "/"); idx >= 0 && idx < len(slug)-1 {
		return slug[idx+1:]
	}
	return slug
}

// CopyReferences returns rewritten copies of retrieval results with any wiki
// links in their text flattened. It copies rather than mutates because an SSE
// references payload shares its *SearchResult pointers with the stream replay
// buffer and the assistant message being persisted.
func (w *Rewriter) CopyReferences(ctx context.Context, refs []*types.SearchResult) []*types.SearchResult {
	if !w.Enabled() || refs == nil {
		return refs
	}
	out := make([]*types.SearchResult, len(refs))
	for i, ref := range refs {
		if ref == nil {
			continue
		}
		rewritten := *ref
		rewritten.Content = w.String(ctx, ref.Content)
		rewritten.MatchedContent = w.String(ctx, ref.MatchedContent)
		out[i] = &rewritten
	}
	return out
}

// RewriteMessagesResponse returns message copies with wiki links flattened, so
// callers keep the originals untouched (for example objects from a service
// cache). In the default link mode the messages are returned as-is.
func (w *Rewriter) RewriteMessagesResponse(ctx context.Context, messages []*types.Message) []*types.Message {
	if !w.Enabled() || len(messages) == 0 {
		return messages
	}
	out := cloneMessages(messages)
	for _, message := range out {
		if message == nil {
			continue
		}
		message.Content = w.String(ctx, message.Content)
		message.KnowledgeReferences = w.CopyReferences(ctx, message.KnowledgeReferences)
	}
	return out
}

// cloneMessages deep-copies messages via JSON so nested slices and references
// are independent of the source slice.
func cloneMessages(messages []*types.Message) []*types.Message {
	data, err := json.Marshal(messages)
	if err != nil {
		return messages
	}
	var out []*types.Message
	if err := json.Unmarshal(data, &out); err != nil {
		return messages
	}
	return out
}
