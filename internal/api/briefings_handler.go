package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/agent"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

type briefingCreateRequest struct {
	UserInput   map[string]any `json:"user_input"`
	Model       string         `json:"model"`
	Temperature *float64       `json:"temperature"`
	MaxTokens   *int           `json:"max_tokens"`
	Scheduled   bool           `json:"scheduled"`
	RunDate     string         `json:"run_date"`
}

type briefingCreateResponse struct {
	SessionID string               `json:"session_id"`
	Status    string               `json:"status"`
	Output    *agent.BriefingOutput `json:"output,omitempty"`
}

func parseRunDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	tt, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, err
	}
	return tt.UTC(), nil
}

func ensurePortfolioAccess(c *gin.Context, readStore PortfolioReadStore, priceStreamPartitions []uuid.UUID) (uuid.UUID, bool) {
	pid, ok := validatePortfolioPathID(c, priceStreamPartitions)
	if !ok {
		return uuid.Nil, false
	}
	if user, hasUser := authUserFromContext(c); hasUser {
		if checker, ok := readStore.(portfolioOwnerChecker); ok {
			owned, err := checker.PortfolioOwnedByUser(c.Request.Context(), pid, user.UserID)
			if err != nil {
				respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
				return uuid.Nil, false
			}
			if !owned {
				respondAPIError(c, http.StatusForbidden, ErrCodeForbidden, "forbidden", nil)
				return uuid.Nil, false
			}
		}
	}
	_, found, err := readStore.LoadPortfolioAssemblerInput(c.Request.Context(), pid)
	if err != nil {
		respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
		return uuid.Nil, false
	}
	if !found {
		respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "portfolio not found", nil)
		return uuid.Nil, false
	}
	return pid, true
}

func postBriefingHandler(
	svc agent.AgentService,
	readStore PortfolioReadStore,
	priceStreamPartitions []uuid.UUID,
	defaultMaxTokens int,
	defaultTemperature float64,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		pid, ok := ensurePortfolioAccess(c, readStore, priceStreamPartitions)
		if !ok {
			return
		}
		var reqBody briefingCreateRequest
		if err := c.ShouldBindJSON(&reqBody); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}
		runDate, err := parseRunDate(reqBody.RunDate)
		if err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "run_date must be YYYY-MM-DD", nil)
			return
		}
		userInput := reqBody.UserInput
		if userInput == nil {
			userInput = map[string]any{}
		}
		userInputRaw, err := jsonMarshal(userInput)
		if err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "user_input must be valid JSON object", nil)
			return
		}
		var requestedBy *uuid.UUID
		if user, hasUser := authUserFromContext(c); hasUser {
			requestedBy = &user.UserID
		}
		req := agent.RunBriefingRequest{
			PortfolioID:       pid,
			RequestedByUserID: requestedBy,
			RunDate:           runDate,
			Model:             strings.TrimSpace(reqBody.Model),
			Temperature:       reqBody.Temperature,
			MaxTokens:         reqBody.MaxTokens,
			UserInput:         userInputRaw,
		}
		if req.MaxTokens == nil && defaultMaxTokens > 0 {
			v := defaultMaxTokens
			req.MaxTokens = &v
		}
		if req.Temperature == nil && defaultTemperature >= 0 {
			v := defaultTemperature
			req.Temperature = &v
		}
		var result agent.RunBriefingResult
		if reqBody.Scheduled {
			result, err = svc.CreateBriefingScheduled(c.Request.Context(), req)
		} else {
			result, err = svc.CreateBriefingOnDemand(c.Request.Context(), req)
		}
		if err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "validation failed") || strings.Contains(msg, "invalid_output") {
				respondAPIError(c, http.StatusUnprocessableEntity, ErrCodeValidation, "briefing output failed validation", nil)
				return
			}
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		resp := briefingCreateResponse{
			SessionID: result.Session.SessionID.String(),
			Status:    result.Session.Status,
		}
		if result.Session.Status == "succeeded" {
			out := result.Output
			resp.Output = &out
		}
		c.JSON(http.StatusCreated, resp)
	}
}

func getLatestBriefingHandler(svc agent.AgentService, readStore PortfolioReadStore, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		pid, ok := ensurePortfolioAccess(c, readStore, priceStreamPartitions)
		if !ok {
			return
		}
		row, found, err := svc.GetLatestBriefing(c.Request.Context(), pid)
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		if !found {
			respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "briefing not found", nil)
			return
		}
		c.JSON(http.StatusOK, row)
	}
}

func getBriefingsHandler(svc agent.AgentService, readStore PortfolioReadStore, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		pid, ok := ensurePortfolioAccess(c, readStore, priceStreamPartitions)
		if !ok {
			return
		}
		limit := 50
		offset := 0
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				limit = v
			}
		}
		if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
				offset = v
			}
		}
		statuses := []string{}
		if raw := strings.TrimSpace(c.Query("status")); raw != "" {
			for _, s := range strings.Split(raw, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					statuses = append(statuses, s)
				}
			}
		}
		rows, err := svc.ListBriefings(c.Request.Context(), pid, events.AgentSessionListFilter{
			Statuses: statuses,
			Limit:    limit,
			Offset:   offset,
		})
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": rows})
	}
}

func getBriefingReplayHandler(svc agent.AgentService, readStore PortfolioReadStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		sid, err := uuid.Parse(strings.TrimSpace(c.Param("session_id")))
		if err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "session_id must be a UUID", nil)
			return
		}
		replay, found, err := svc.GetSessionReplay(c.Request.Context(), sid)
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		if !found {
			respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "agent session not found", nil)
			return
		}
		if user, hasUser := authUserFromContext(c); hasUser {
			if checker, ok := readStore.(portfolioOwnerChecker); ok {
				owned, err := checker.PortfolioOwnedByUser(c.Request.Context(), replay.Session.PortfolioID, user.UserID)
				if err != nil {
					respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
					return
				}
				if !owned {
					respondAPIError(c, http.StatusForbidden, ErrCodeForbidden, "forbidden", nil)
					return
				}
			}
		}
		c.JSON(http.StatusOK, replay)
	}
}

func jsonMarshal(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}
