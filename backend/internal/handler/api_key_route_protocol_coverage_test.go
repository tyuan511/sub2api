package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type apiKeyRouteProtocolOrderCase struct {
	file             string
	function         string
	selectionTokens  []string
	sideEffectTokens []string
}

// This source-level contract complements the runtime failover tests. It makes
// every protocol entry prove that physical-group selection stays ahead of
// audit attribution, billing/capacity accounting, account scheduling, task
// creation, downstream acceptance, and upstream calls.
func TestAPIKeyRouteProtocolSelectionPrecedesActualGroupSideEffects(t *testing.T) {
	tests := []apiKeyRouteProtocolOrderCase{
		{file: "gateway_handler.go", function: "Messages", selectionTokens: []string{"ensureInitial(c, gatewayCandidateCheck)"}, sideEffectTokens: []string{"CheckBillingEligibility(", "SelectAccount"}},
		{file: "gateway_handler_responses.go", function: "Responses", selectionTokens: []string{"ensureInitial(c, responsesCandidateCheck)"}, sideEffectTokens: []string{"CheckBillingEligibility(", "SelectAccount"}},
		{file: "gateway_handler_chat_completions.go", function: "ChatCompletions", selectionTokens: []string{"ensureInitial(c, gatewayCCCandidateCheck)"}, sideEffectTokens: []string{"checkSecurityAudit(", "CheckBillingEligibility(", "SelectAccount"}},
		{file: "openai_gateway_handler.go", function: "Responses", selectionTokens: []string{"ensureInitial(c, responseCandidateCheck)"}, sideEffectTokens: []string{"CheckBillingEligibility(", "SelectAccount"}},
		{file: "openai_gateway_handler.go", function: "Messages", selectionTokens: []string{"ensureInitial(c, messagesCandidateCheck)"}, sideEffectTokens: []string{"CheckBillingEligibility(", "SelectAccount"}},
		{file: "openai_chat_completions.go", function: "ChatCompletions", selectionTokens: []string{"ensureInitial(c, chatCandidateCheck)"}, sideEffectTokens: []string{"CheckBillingEligibility(", "SelectAccount"}},
		{file: "openai_embeddings.go", function: "Embeddings", selectionTokens: []string{"ensureInitial(c, embeddingsCandidateCheck)"}, sideEffectTokens: []string{"CheckBillingEligibility(", "SelectAccount"}},
		{file: "openai_images.go", function: "Images", selectionTokens: []string{"ensureInitial(c, imagesCandidateCheck)"}, sideEffectTokens: []string{"CheckBillingEligibility(", "SelectAccount"}},
		{file: "openai_live.go", function: "Live", selectionTokens: []string{"ensureInitial(c, liveCandidateCheck)"}, sideEffectTokens: []string{"checkSecurityAudit(", "CheckBillingEligibility(", "CreateLiveCall("}},
		{file: "openai_live.go", function: "LiveSideband", selectionTokens: []string{"activateAPIKeyRouteOwnedGroup("}, sideEffectTokens: []string{"coderws.Accept(", "ProxyLiveSideband("}},
		{file: "openai_gateway_handler.go", function: "ResponsesWebSocket", selectionTokens: []string{"activateAPIKeyRouteOwnedGroup(", "ensureInitial(c, wsCandidateCheck)"}, sideEffectTokens: []string{"CheckBillingEligibility(", "SelectAccount"}},
		{file: "gemini_v1beta_handler.go", function: "GeminiV1BetaModels", selectionTokens: []string{"ensureInitial(c, geminiCandidateCheck)"}, sideEffectTokens: []string{"checkSecurityAudit(", "CheckBillingEligibility(", "SelectAccount"}},
		{file: "grok_media.go", function: "handleGrokMedia", selectionTokens: []string{"activateAPIKeyRouteOwnedGroup(", "ensureInitial(c, grokMediaCandidateCheck)"}, sideEffectTokens: []string{"checkSecurityAudit(", "CheckBillingEligibility(", "SelectAccount", "ForwardGrokMedia("}},
		{file: "grok_audio.go", function: "GrokRealtime", selectionTokens: []string{"ensureInitial(c, grokRealtimeCandidateCheck)"}, sideEffectTokens: []string{"CheckBillingEligibility(", "SelectAccount", "coderws.Accept("}},
		{file: "grok_audio.go", function: "GrokVoice", selectionTokens: []string{"ensureInitial(c, grokVoiceCandidateCheck)"}, sideEffectTokens: []string{"checkSecurityAudit(", "CheckBillingEligibility(", "SelectAccount", "ForwardGrokVoice("}},
		{file: "batch_image_handler.go", function: "Submit", selectionTokens: []string{"ensureInitial(c, batchCandidateCheck)"}, sideEffectTokens: []string{"checkSecurityAuditBeforeSubmit(", "h.service.Submit("}},
		{file: "image_task_handler.go", function: "Submit", selectionTokens: []string{"ensureInitial(c, asyncCandidateCheck)"}, sideEffectTokens: []string{"checkSecurityAuditBeforeSubmit(", "h.tasks.Create("}},
	}

	for _, tt := range tests {
		t.Run(tt.file+"/"+tt.function, func(t *testing.T) {
			source := stripGoComments(goFunctionSource(t, tt.file, tt.function))
			latestSelection := -1
			for _, token := range tt.selectionTokens {
				index := strings.Index(source, token)
				require.NotEqualf(t, -1, index, "missing physical-group selection token %q", token)
				if index > latestSelection {
					latestSelection = index
				}
			}
			for _, token := range tt.sideEffectTokens {
				index := strings.Index(source, token)
				require.NotEqualf(t, -1, index, "missing side-effect boundary token %q", token)
				require.Lessf(t, latestSelection, index, "physical-group selection must precede %s", token)
			}
		})
	}
}
