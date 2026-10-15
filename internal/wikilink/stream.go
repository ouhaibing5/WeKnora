package wikilink

import (
	"context"
	"regexp"
	"sync"
	"unicode/utf8"
)

// ── Incomplete-link detection ──
//
// Content reaches clients in chunks (SSE answer deltas). A wiki link may be
// split across two chunks, so a rewrite that only sees one chunk would emit a
// broken `[[slug` fragment. These helpers locate an incomplete link at the tail
// of a chunk so the caller can hold it back until the next chunk completes it.

// incompleteWikiLinkSuffixRe matches a wiki-link fragment that reaches the end
// of the string: either an unclosed `[[...` or a lone trailing `[` that could
// become `[[` once the next chunk arrives.
var incompleteWikiLinkSuffixRe = regexp.MustCompile(`\[\[[^\]]*$|\[$`)

// maxIncompleteWikiLinkBytes bounds how much trailing text may be treated as an
// unfinished wiki link. A slug plus a title is far shorter than this; the cap
// only stops prose that merely contains a stray bracket from stalling the
// stream forever.
const maxIncompleteWikiLinkBytes = 2048

// FindIncompleteWikiLink returns the byte offset of a potentially truncated
// wiki link at the tail of s, or -1 if none.
func FindIncompleteWikiLink(s string) int {
	loc := incompleteWikiLinkSuffixRe.FindStringIndex(s)
	if loc == nil || len(s)-loc[0] > maxIncompleteWikiLinkBytes {
		return -1
	}
	return loc[0]
}

// HoldbackCutoff returns the offset at which chunk stops being safe to flush,
// or len(chunk) when the whole chunk can be emitted.
func HoldbackCutoff(chunk string) int {
	if idx := FindIncompleteWikiLink(chunk); idx >= 0 {
		return idx
	}
	return len(chunk)
}

// maxHeldBytes bounds the per-key holdback buffer, so a pathological stream
// (for example an unclosed `[[` that never terminates) cannot buffer an entire
// answer.
const maxHeldBytes = 4096

// StreamRewriter flattens wiki links in a stream of content deltas.
//
// Each logical stream is identified by a key (WeKnora uses the SSE event id, the
// same key clients accumulate on). Push returns only the prefix that is safe to
// emit now, retaining any tail that may be an incomplete link until the next
// Push for that key.
//
// Safe for concurrent use.
type StreamRewriter struct {
	rewriter *Rewriter

	mu   sync.Mutex
	held map[string]heldContent
}

// heldContent is a retained tail plus the metadata of the event it came from, so
// a tail released after its stream ended can be re-emitted as an equivalent
// event rather than a bare fragment.
type heldContent struct {
	content string
	meta    interface{}
}

// Held is one released tail: the flattened content and the metadata carried by
// the last Push that contributed to it.
type Held struct {
	Content string
	Meta    interface{}
}

// NewStreamRewriter wraps rewriter with per-stream holdback state.
func NewStreamRewriter(rewriter *Rewriter) *StreamRewriter {
	return &StreamRewriter{rewriter: rewriter, held: make(map[string]heldContent)}
}

// Enabled reports whether this StreamRewriter can flatten anything.
func (s *StreamRewriter) Enabled() bool {
	return s != nil && s.rewriter.Enabled()
}

// Rewriter exposes the underlying Rewriter for fields that arrive whole
// (references) and therefore need no holdback.
func (s *StreamRewriter) Rewriter() *Rewriter {
	if s == nil {
		return nil
	}
	return s.rewriter
}

// Push feeds the next chunk of the stream identified by key and returns the
// flattened content that is ready to emit. Set flush on the stream's terminal
// chunk to release any held tail. meta is retained opaquely alongside the tail
// and handed back by FlushAll so a late release can carry the same metadata as
// the event it was cut from.
func (s *StreamRewriter) Push(ctx context.Context, key, chunk string, flush bool, meta interface{}) string {
	if !s.Enabled() {
		return chunk
	}

	s.mu.Lock()
	pending := s.held[key].content + chunk
	cutoff := len(pending)
	if !flush {
		cutoff = HoldbackCutoff(pending)
		// Never buffer without bound: release the excess even though it may
		// contain a partial link, which is what an un-flattened stream would
		// have shown anyway.
		if len(pending)-cutoff > maxHeldBytes {
			cutoff = runeStart(pending, len(pending)-maxHeldBytes)
		}
	}
	emit := pending[:cutoff]
	if remainder := pending[cutoff:]; remainder == "" {
		delete(s.held, key)
	} else {
		s.held[key] = heldContent{content: remainder, meta: meta}
	}
	s.mu.Unlock()

	return s.rewriter.String(ctx, emit)
}

// runeStart moves idx back to the nearest UTF-8 sequence boundary so a
// byte-based cut never splits a character in half. Pattern-derived cutoffs
// already land on ASCII brackets; this only matters for the maxHeldBytes valve.
func runeStart(s string, idx int) int {
	for idx > 0 && !utf8.RuneStart(s[idx]) {
		idx--
	}
	return idx
}

// FlushAll releases every held tail, flattened, keyed by stream. Callers use it
// when a stream ends without a terminal chunk (for example a client disconnect)
// so buffered content is not silently dropped.
func (s *StreamRewriter) FlushAll(ctx context.Context) map[string]Held {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	if len(s.held) == 0 {
		s.mu.Unlock()
		return nil
	}
	pending := s.held
	s.held = make(map[string]heldContent)
	s.mu.Unlock()

	out := make(map[string]Held, len(pending))
	for key, held := range pending {
		out[key] = Held{Content: s.rewriter.String(ctx, held.content), Meta: held.meta}
	}
	return out
}
