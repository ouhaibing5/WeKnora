// Package wikilink flattens internal wiki-link markup in outbound API
// responses.
//
// WeKnora's Wiki feature lets an agent cite pages inline as `[[slug]]` or
// `[[slug|title]]`. The built-in web UI renders that markup as a clickable
// cross-reference, so it must be preserved for the bundled front end. External
// API integrators, however, usually want readable prose with no markup and no
// romanized slug. They can request that with the `wiki_links` query parameter,
// which mirrors how `resource_urls` controls the form of storage references.
//
// The default `link` mode is a pass-through: response content is left
// byte-identical, so existing callers (including the web UI) are unaffected.
package wikilink

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/logger"
)

// Mode selects how wiki links are represented in an API response.
type Mode string

const (
	// ModeLink keeps the raw `[[slug]]` / `[[slug|title]]` markup so a client
	// (the built-in web UI) can render clickable cross-references. This is the
	// default and leaves response content byte-identical.
	ModeLink Mode = "link"
	// ModeText flattens each wiki link to its human-readable display text: the
	// title when present (`[[slug|title]]` -> `title`), otherwise the slug with
	// its type prefix stripped (`[[entity/foo-bar]]` -> `foo-bar`). This matches
	// exactly the label the web UI shows for the link, so an integrator receives
	// plain readable text instead of markup.
	ModeText Mode = "text"
)

const (
	// QueryParam is the request query parameter that selects the mode per call.
	QueryParam = "wiki_links"
	// EnvVar sets the deployment-wide default mode for requests that omit
	// QueryParam. An integrator that always wants plain text can set it once
	// instead of passing the parameter on every call.
	EnvVar = "WIKI_LINK_MODE"
)

// ParseMode validates a mode supplied by a client or by configuration. An empty
// value yields ModeLink so an unset parameter or setting keeps the default. A
// few intuitive synonyms collapse to ModeText so integrators do not have to
// guess the exact token.
func ParseMode(raw string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ModeLink:
		return ModeLink, nil
	case ModeText, "plain", "title":
		return ModeText, nil
	default:
		return ModeLink, fmt.Errorf(
			"invalid %s value %q: expected %q or %q", QueryParam, raw, ModeLink, ModeText)
	}
}

// badDefault keeps a misconfigured EnvVar to a single log line per distinct bad
// value instead of one per request, while still warning again if an operator
// rolls out a second typo. The value itself is re-read every time so a fix
// takes effect without a restart.
var badDefault struct {
	sync.Mutex
	warned map[string]bool
}

func warnBadDefaultOnce(ctx context.Context, raw string, err error) {
	badDefault.Lock()
	defer badDefault.Unlock()
	if badDefault.warned[raw] {
		return
	}
	if badDefault.warned == nil {
		badDefault.warned = make(map[string]bool)
	}
	badDefault.warned[raw] = true
	logger.Warnf(ctx, "ignoring %s: %v; falling back to %q", EnvVar, err, ModeLink)
}

// DefaultMode returns the deployment-wide default from EnvVar. An unset or
// unparseable value yields ModeLink, so a typo degrades to the safe default
// rather than failing every request.
func DefaultMode(ctx context.Context) Mode {
	raw := strings.TrimSpace(os.Getenv(EnvVar))
	if raw == "" {
		return ModeLink
	}
	mode, err := ParseMode(raw)
	if err != nil {
		warnBadDefaultOnce(ctx, raw, err)
		return ModeLink
	}
	return mode
}

// ResolveMode combines the per-request query value with the deployment default.
// An explicit query value wins; an invalid one is returned as an error so
// integrators find their typo instead of silently receiving markup.
func ResolveMode(ctx context.Context, queryValue string) (Mode, error) {
	if strings.TrimSpace(queryValue) != "" {
		return ParseMode(queryValue)
	}
	return DefaultMode(ctx), nil
}
