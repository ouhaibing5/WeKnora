package agent

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

const agentRetrievedImageRequirementMarker = "## Retrieved Image Output Requirement"

const agentRetrievedImageSystemRequirement = `

## Retrieved Image Output Requirement
Retrieved tool results for this turn contain Markdown images. Relevance is NOT implied by retrieval: judge each image against the user's question.
- Include an image ONLY when it directly and specifically supports the part of the answer where it appears (e.g. it depicts the exact subject the user asked about).
- If no retrieved image is clearly relevant to the question, include NO images — a text-only answer is correct and preferred over forcing an unrelated one. Never include decorative, logo, header/footer, or UI-screenshot images.
- When you do include a relevant image, preserve the complete Markdown image syntax and URL exactly; never invent, shorten, normalize, or replace the URL.
- Use ASCII half-width parentheses exactly as ![alt](url); never use full-width （ or ）.
- Place each included image immediately after the paragraph it supports; when several are each relevant to different sections, distribute them accordingly.
- Before finishing, verify every included image is directly relevant and remove any that is not, even if that leaves the answer with no images.`

func stepContainsMarkdownImage(step types.AgentStep) bool {
	for _, toolCall := range step.ToolCalls {
		if toolCall.Result != nil &&
			toolCall.Result.Success &&
			searchutil.MarkdownImageRegex.MatchString(toolCall.Result.Output) {
			return true
		}
	}
	return false
}

func appendAgentRetrievedImageRequirement(messages []chat.Message) []chat.Message {
	for _, message := range messages {
		if strings.Contains(message.Content, agentRetrievedImageRequirementMarker) {
			return messages
		}
	}
	// Append after the current prefix instead of editing the system prompt.
	// Provider prompt caches are prefix matches: mutating the system block
	// invalidates the tools and the whole transcript for every later round.
	return append(messages, chat.Message{
		Role:    "user",
		Content: strings.TrimSpace(agentRetrievedImageSystemRequirement),
	})
}
