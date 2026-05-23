package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

// CriticVerdict is the structured self-critic output persisted on proposed_trades.
type CriticVerdict struct {
	Allow      bool   `json:"allow"`
	ReasonCode string `json:"reason_code"`
	Notes      string `json:"notes"`
}

// ProposalReader loads proposals for critic review.
type ProposalReader interface {
	GetByIDForPortfolio(ctx context.Context, portfolioID, proposalID uuid.UUID) (proposals.Proposal, error)
}

// CriticVerdictWriter persists critic audit fields.
type CriticVerdictWriter interface {
	SaveCriticVerdict(ctx context.Context, p proposals.SaveCriticVerdictParams) error
}

// Critic runs a second LLM pass (no tools) before autonomous submit.
type Critic struct {
	Client AnthropicClient
	Store  interface {
		ProposalReader
		CriticVerdictWriter
	}
	Model string
	Log   *zap.Logger
}

const criticSystemPrompt = "" +
	"You are an adversarial trading risk reviewer. " +
	"Given a proposed trade and rationale, decide whether it is reasonable to auto-submit on a paper account. " +
	"Output JSON only with keys: allow (boolean), reason_code (short snake_case string), notes (brief string). " +
	"Set allow=false when the trade is unclear, oversized, contradictory, or lacks sufficient rationale. " +
	"You cannot override deterministic policy; focus on qualitative risk and coherence."

// Review loads the proposal, calls the model, persists the verdict, and returns the parsed result.
func (c *Critic) Review(ctx context.Context, portfolioID, proposalID uuid.UUID) (CriticVerdict, error) {
	if c == nil || c.Client == nil || c.Store == nil {
		return CriticVerdict{Allow: false, ReasonCode: "critic_unconfigured", Notes: "critic not configured"}, nil
	}
	log := c.Log
	if log == nil {
		log = zap.NewNop()
	}
	prop, err := c.Store.GetByIDForPortfolio(ctx, portfolioID, proposalID)
	if err != nil {
		return CriticVerdict{}, fmt.Errorf("critic: load proposal: %w", err)
	}
	userPayload, err := json.Marshal(map[string]any{
		"proposal_id": proposalID.String(),
		"symbol":      prop.Symbol,
		"side":        prop.Side,
		"quantity":    prop.Quantity,
		"notional_usd": prop.NotionalUSD,
		"order_type":  prop.OrderType,
		"time_in_force": prop.TimeInForce,
		"rationale":   redactRationale(prop.RationaleSnapshot),
		"policy_result": redactJSON(prop.PolicyResult),
	})
	if err != nil {
		return CriticVerdict{}, fmt.Errorf("critic: marshal input: %w", err)
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = "claude-sonnet-4.6"
	}
	resp, err := c.Client.CreateMessage(ctx, AnthropicMessageRequest{
		Model:  model,
		System: criticSystemPrompt,
		Messages: []AnthropicMessage{{
			Role: "user",
			Content: []AnthropicContentBlock{{
				Type: "text",
				Text: string(userPayload),
			}},
		}},
	})
	if err != nil {
		return CriticVerdict{Allow: false, ReasonCode: "critic_provider_error", Notes: err.Error()}, nil
	}
	verdict, parseErr := parseCriticVerdict(resp.OutputText)
	if parseErr != nil {
		log.Warn("critic_parse_failed", zap.Error(parseErr), zap.String("proposal_id", proposalID.String()))
		verdict = CriticVerdict{Allow: false, ReasonCode: "critic_parse_failed", Notes: parseErr.Error()}
	}
	raw, _ := json.Marshal(verdict)
	if err := c.Store.SaveCriticVerdict(ctx, proposals.SaveCriticVerdictParams{
		PortfolioID: portfolioID,
		ProposalID:  proposalID,
		Verdict:     raw,
		CompletedAt: time.Now().UTC(),
		Model:       model,
	}); err != nil {
		return verdict, fmt.Errorf("critic: persist verdict: %w", err)
	}
	return verdict, nil
}

func parseCriticVerdict(text string) (CriticVerdict, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return CriticVerdict{}, fmt.Errorf("empty critic output")
	}
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	var v CriticVerdict
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return CriticVerdict{}, err
	}
	v.ReasonCode = strings.TrimSpace(v.ReasonCode)
	v.Notes = strings.TrimSpace(v.Notes)
	if v.ReasonCode == "" {
		if v.Allow {
			v.ReasonCode = "approved"
		} else {
			v.ReasonCode = "veto"
		}
	}
	return v, nil
}

func redactRationale(s *string) string {
	if s == nil {
		return ""
	}
	b, err := json.Marshal(*s)
	if err != nil {
		return ""
	}
	return string(redactJSON(b))
}
