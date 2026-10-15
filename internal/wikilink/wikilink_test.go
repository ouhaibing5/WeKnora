package wikilink

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		raw     string
		want    Mode
		wantErr bool
	}{
		{"", ModeLink, false},
		{"link", ModeLink, false},
		{"LINK", ModeLink, false},
		{"text", ModeText, false},
		{"plain", ModeText, false},
		{"title", ModeText, false},
		{"  text  ", ModeText, false},
		{"markdown", ModeLink, true},
	}
	for _, tt := range tests {
		got, err := ParseMode(tt.raw)
		if tt.wantErr {
			require.Error(t, err, "raw=%q", tt.raw)
			assert.Contains(t, err.Error(), QueryParam)
		} else {
			require.NoError(t, err, "raw=%q", tt.raw)
		}
		assert.Equal(t, tt.want, got, "raw=%q", tt.raw)
	}
}

func TestResolveMode_QueryWinsOverDefault(t *testing.T) {
	t.Setenv(EnvVar, "text")
	// Explicit query value overrides the deployment default.
	mode, err := ResolveMode(context.Background(), "link")
	require.NoError(t, err)
	assert.Equal(t, ModeLink, mode)
	// Empty query falls back to the env default.
	mode, err = ResolveMode(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, ModeText, mode)
}

func TestResolveMode_BadDefaultDegradesToLink(t *testing.T) {
	t.Setenv(EnvVar, "nonsense")
	mode, err := ResolveMode(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, ModeLink, mode)
}

func TestRewriter_DisabledInLinkMode(t *testing.T) {
	w := NewRewriter(ModeLink)
	assert.False(t, w.Enabled())
	assert.Equal(t, "see [[entity/foo| Foo ]]", w.String(context.Background(), "see [[entity/foo| Foo ]]"))
}

func TestRewriter_String(t *testing.T) {
	w := NewRewriter(ModeText)
	require.True(t, w.Enabled())

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"title wins", "见[[entity/nian-jia|年假管理办法]]。", "见年假管理办法。"},
		{"bare slug strips type prefix", "[[concept/qing-jia]]", "qing-jia"},
		{"empty title falls back to slug", "[[entity/foo-bar|]]", "foo-bar"},
		{"multiple links", "[[entity/a|甲]] 和 [[concept/b|乙]]", "甲 和 乙"},
		{"no link passthrough", "plain text, no links", "plain text, no links"},
		{"summary uuid slug", "[[summary/abc-123]]", "abc-123"},
		{"slug without prefix", "[[index]]", "index"},
		{"markdown around link", "**[[entity/a|甲]]**", "**甲**"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, w.String(context.Background(), tt.in))
		})
	}
}

func TestDisplayTextAndHumanizeSlug(t *testing.T) {
	assert.Equal(t, "年假管理办法", DisplayText("entity/nian-jia|年假管理办法"))
	assert.Equal(t, "foo-bar", DisplayText("entity/foo-bar"))
	assert.Equal(t, "foo-bar", HumanizeSlug("entity/foo-bar"))
	assert.Equal(t, "index", HumanizeSlug("index"))
	// A trailing slash with no name is left intact rather than emptied.
	assert.Equal(t, "entity/", HumanizeSlug("entity/"))
}

func TestRewriter_CopyReferencesDoesNotMutateSource(t *testing.T) {
	w := NewRewriter(ModeText)
	src := &types.SearchResult{Content: "chunk [[entity/a|甲]]", MatchedContent: "match [[entity/a|甲]]"}
	out := w.CopyReferences(context.Background(), []*types.SearchResult{src})

	require.Len(t, out, 1)
	assert.Equal(t, "chunk 甲", out[0].Content)
	assert.Equal(t, "match 甲", out[0].MatchedContent)
	assert.Equal(t, "chunk [[entity/a|甲]]", src.Content, "source must be untouched")
}

func TestStreamRewriter_HoldsLinkSplitAcrossDeltas(t *testing.T) {
	s := NewStreamRewriter(NewRewriter(ModeText))
	ctx := context.Background()

	first := s.Push(ctx, "answer-1", "见 [[entity/nian-jia", false, nil)
	assert.Equal(t, "见 ", first, "the incomplete link must be held back")

	second := s.Push(ctx, "answer-1", "|年假管理办法]] 完毕", false, nil)
	assert.Equal(t, "年假管理办法 完毕", second)
}

func TestStreamRewriter_HoldsLoneTrailingBracket(t *testing.T) {
	s := NewStreamRewriter(NewRewriter(ModeText))
	ctx := context.Background()

	// A single trailing '[' may become '[[' in the next chunk.
	first := s.Push(ctx, "a", "done [", false, nil)
	assert.Equal(t, "done ", first)

	second := s.Push(ctx, "a", "[entity/x|甲]]", false, nil)
	assert.Equal(t, "甲", second)
}

func TestStreamRewriter_FlushReleasesHeldTail(t *testing.T) {
	s := NewStreamRewriter(NewRewriter(ModeText))
	ctx := context.Background()

	s.Push(ctx, "answer-1", "tail [[entity/a", false, map[string]interface{}{"event_id": "answer-1"})
	held := s.FlushAll(ctx)
	require.Contains(t, held, "answer-1")
	assert.Equal(t, "[[entity/a", held["answer-1"].Content,
		"an incomplete link cannot be flattened but must not be swallowed")
	assert.Equal(t, map[string]interface{}{"event_id": "answer-1"}, held["answer-1"].Meta)
}

func TestStreamRewriter_DisabledPassesThrough(t *testing.T) {
	s := NewStreamRewriter(NewRewriter(ModeLink))
	assert.False(t, s.Enabled())
	assert.Equal(t, "[[entity/a|甲]]", s.Push(context.Background(), "k", "[[entity/a|甲]]", false, nil))
	assert.Nil(t, s.FlushAll(context.Background()))
}

func TestHoldbackCutoff(t *testing.T) {
	assert.Equal(t, len("complete [[a|b]] text"), HoldbackCutoff("complete [[a|b]] text"))
	assert.Equal(t, len("see "), HoldbackCutoff("see [[entity/foo"))
	assert.Equal(t, len("see "), HoldbackCutoff("see ["))
	assert.Equal(t, len("no brackets"), HoldbackCutoff("no brackets"))
}
