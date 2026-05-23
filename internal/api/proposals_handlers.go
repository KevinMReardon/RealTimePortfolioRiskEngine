package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

type proposalListResponse struct {
	Proposals []proposalJSON `json:"proposals"`
}

type proposalJSON struct {
	ProposalID        string          `json:"proposal_id"`
	PortfolioID       string          `json:"portfolio_id"`
	AgentSessionID    *string         `json:"agent_session_id,omitempty"`
	TradeIdeaIndex    *int            `json:"trade_idea_index,omitempty"`
	Symbol            string          `json:"symbol"`
	Side              string          `json:"side"`
	Status            string          `json:"status"`
	RowVersion        int             `json:"row_version"`
	PayloadHash       string          `json:"payload_hash"`
	PolicyInputsHash  string          `json:"policy_inputs_hash"`
	PolicyConfigHash  string          `json:"policy_config_hash"`
	RationaleSnapshot *string         `json:"rationale_snapshot,omitempty"`
	CriticVerdict     json.RawMessage `json:"critic_verdict,omitempty"`
	CriticCompletedAt *string         `json:"critic_completed_at,omitempty"`
	CriticModel       *string         `json:"critic_model,omitempty"`
	ApprovalSource    *string         `json:"approval_source,omitempty"`
}

type proposalApproveRequest struct {
	PayloadHash string `json:"payload_hash" binding:"required"`
	RowVersion  int    `json:"row_version" binding:"required"`
}

type proposalDenyRequest struct {
	PayloadHash string `json:"payload_hash" binding:"required"`
	RowVersion  int    `json:"row_version" binding:"required"`
	DenyReason  string `json:"deny_reason" binding:"required"`
}

func proposalToJSON(p proposals.Proposal) proposalJSON {
	out := proposalJSON{
		ProposalID:        p.ProposalID.String(),
		PortfolioID:       p.PortfolioID.String(),
		Symbol:            p.Symbol,
		Side:              p.Side,
		Status:            p.Status,
		RowVersion:        p.RowVersion,
		PayloadHash:       p.PayloadHash,
		PolicyInputsHash:  p.PolicyInputsHash,
		PolicyConfigHash:  p.PolicyConfigHash,
		RationaleSnapshot: p.RationaleSnapshot,
	}
	if p.AgentSessionID != nil {
		s := p.AgentSessionID.String()
		out.AgentSessionID = &s
	}
	out.TradeIdeaIndex = p.TradeIdeaIndex
	if len(p.CriticVerdict) > 0 {
		out.CriticVerdict = append(json.RawMessage(nil), p.CriticVerdict...)
	}
	if p.CriticCompletedAt != nil {
		s := p.CriticCompletedAt.UTC().Format(time.RFC3339Nano)
		out.CriticCompletedAt = &s
	}
	out.CriticModel = p.CriticModel
	out.ApprovalSource = p.ApprovalSource
	return out
}

func getProposalsHandler(store *proposals.Store, readStore PortfolioReadStore, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		pid, ok := ensurePortfolioAccess(c, readStore, priceStreamPartitions)
		if !ok {
			return
		}
		if store == nil {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "proposals store not configured", nil)
			return
		}
		list, err := store.ListByPortfolio(c.Request.Context(), pid, proposals.ListFilter{})
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		out := make([]proposalJSON, 0, len(list))
		for i := range list {
			out = append(out, proposalToJSON(list[i]))
		}
		c.JSON(http.StatusOK, proposalListResponse{Proposals: out})
	}
}

func postProposalApproveHandler(store *proposals.Store, readStore PortfolioReadStore, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		pid, ok := ensurePortfolioAccess(c, readStore, priceStreamPartitions)
		if !ok {
			return
		}
		if store == nil {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "proposals store not configured", nil)
			return
		}
		propID, err := uuid.Parse(c.Param("proposal_id"))
		if err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "proposal_id must be a UUID", nil)
			return
		}
		user, ok := authUserFromContext(c)
		if !ok {
			respondAPIError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", nil)
			return
		}
		var body proposalApproveRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}
		err = store.ApproveProposal(c.Request.Context(), proposals.ApproveParams{
			PortfolioID: pid,
			ProposalID:  propID,
			UserID:      user.UserID,
			PayloadHash: body.PayloadHash,
			RowVersion:  body.RowVersion,
		})
		if err != nil {
			if err == proposals.ErrApproveConflict {
				respondAPIError(c, http.StatusConflict, ErrCodeConflict, "approve conflict (stale version or payload mismatch)", nil)
				return
			}
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "approved"})
	}
}

func postProposalDenyHandler(store *proposals.Store, readStore PortfolioReadStore, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		pid, ok := ensurePortfolioAccess(c, readStore, priceStreamPartitions)
		if !ok {
			return
		}
		if store == nil {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "proposals store not configured", nil)
			return
		}
		propID, err := uuid.Parse(c.Param("proposal_id"))
		if err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "proposal_id must be a UUID", nil)
			return
		}
		user, ok := authUserFromContext(c)
		if !ok {
			respondAPIError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", nil)
			return
		}
		var body proposalDenyRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}
		err = store.DenyProposal(c.Request.Context(), proposals.DenyParams{
			PortfolioID: pid,
			ProposalID:  propID,
			UserID:      user.UserID,
			PayloadHash: body.PayloadHash,
			RowVersion:  body.RowVersion,
			DenyReason:  body.DenyReason,
		})
		if err != nil {
			if err == proposals.ErrDenyConflict {
				respondAPIError(c, http.StatusConflict, ErrCodeConflict, "deny conflict (stale version or payload mismatch)", nil)
				return
			}
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "rejected"})
	}
}
