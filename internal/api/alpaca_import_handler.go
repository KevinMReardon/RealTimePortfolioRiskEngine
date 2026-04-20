package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// AlpacaImportStore persists and reads Alpaca fill import jobs.
type AlpacaImportStore interface {
	PortfolioExists(ctx context.Context, portfolioID uuid.UUID) (bool, error)
	CreateAlpacaImportJob(ctx context.Context, portfolioID uuid.UUID, requestedBy *uuid.UUID, since, until *time.Time, fullHistory bool) (uuid.UUID, error)
	GetAlpacaImportJob(ctx context.Context, jobID uuid.UUID) (events.AlpacaImportJob, bool, error)
}

type postAlpacaImportBody struct {
	Since       *time.Time `json:"since"`
	Until       *time.Time `json:"until"`
	FullHistory bool       `json:"full_history"`
}

type alpacaImportJobResponse struct {
	JobID               uuid.UUID  `json:"job_id"`
	PortfolioID         uuid.UUID  `json:"portfolio_id"`
	RequestedByUserID   *uuid.UUID `json:"requested_by_user_id,omitempty"`
	Status              string     `json:"status"`
	Since               *time.Time `json:"since,omitempty"`
	Until               *time.Time `json:"until,omitempty"`
	FullHistory         bool       `json:"full_history"`
	InsertedCount       int        `json:"inserted_count"`
	DuplicateCount      int        `json:"duplicate_count"`
	SkippedInvalidCount int        `json:"skipped_invalid_count"`
	PagesFetched        int        `json:"pages_fetched"`
	ActivitiesSeen      int        `json:"activities_seen"`
	FillsConsidered     int        `json:"fills_considered"`
	ErrorMessage        *string    `json:"error_message,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
}

func postAlpacaImportHandler(
	store AlpacaImportStore,
	log *zap.Logger,
	priceStreamPartitions []uuid.UUID,
	ownership PortfolioOwnershipChecker,
	importEnabled bool,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !importEnabled {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "alpaca import is not configured", map[string]any{
				"reason": "ALPACA_NOT_CONFIGURED",
			})
			return
		}

		pid, ok := validatePortfolioPathID(c, priceStreamPartitions)
		if !ok {
			return
		}
		if user, hasUser := authUserFromContext(c); hasUser && ownership != nil {
			owned, err := ownership.PortfolioOwnedByUser(c.Request.Context(), pid, user.UserID)
			if err != nil {
				respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
				return
			}
			if !owned {
				respondAPIError(c, http.StatusForbidden, ErrCodeForbidden, "forbidden", nil)
				return
			}
		}

		var body postAlpacaImportBody
		dec := json.NewDecoder(c.Request.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			if !errors.Is(err, io.EOF) {
				respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid JSON body", nil)
				return
			}
			body = postAlpacaImportBody{}
		}

		if body.FullHistory && (body.Since != nil || body.Until != nil) {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "full_history cannot be combined with since or until", nil)
			return
		}

		exists, err := store.PortfolioExists(c.Request.Context(), pid)
		if err != nil {
			log.Warn("alpaca_import_portfolio_exists_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		if !exists {
			respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "portfolio not found", nil)
			return
		}

		var requestedBy *uuid.UUID
		if user, ok := authUserFromContext(c); ok {
			requestedBy = &user.UserID
		}

		jobID, err := store.CreateAlpacaImportJob(c.Request.Context(), pid, requestedBy, body.Since, body.Until, body.FullHistory)
		if errors.Is(err, events.ErrAlpacaImportJobConflict) {
			respondAPIError(c, http.StatusConflict, ErrCodeConflict, "an alpaca import job is already queued or running for this portfolio", nil)
			return
		}
		if err != nil {
			log.Warn("alpaca_import_create_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}

		job, found, err := store.GetAlpacaImportJob(c.Request.Context(), jobID)
		if err != nil || !found {
			if err != nil {
				log.Warn("alpaca_import_load_after_create_failed", zap.Error(err))
			}
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}

		c.JSON(http.StatusAccepted, alpacaImportJobFromEvent(job))
	}
}

func getAlpacaImportJobHandler(
	store AlpacaImportStore,
	log *zap.Logger,
	ownership PortfolioOwnershipChecker,
	importEnabled bool,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !importEnabled {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "alpaca import is not configured", map[string]any{
				"reason": "ALPACA_NOT_CONFIGURED",
			})
			return
		}

		raw := c.Param("job_id")
		jobID, err := uuid.Parse(raw)
		if err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "job_id must be a UUID", nil)
			return
		}

		job, found, err := store.GetAlpacaImportJob(c.Request.Context(), jobID)
		if err != nil {
			log.Warn("alpaca_import_get_failed", zap.String("job_id", jobID.String()), zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		if !found {
			respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "job not found", nil)
			return
		}

		if user, hasUser := authUserFromContext(c); hasUser && ownership != nil {
			owned, err := ownership.PortfolioOwnedByUser(c.Request.Context(), job.PortfolioID, user.UserID)
			if err != nil {
				respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
				return
			}
			if !owned {
				respondAPIError(c, http.StatusForbidden, ErrCodeForbidden, "forbidden", nil)
				return
			}
		}

		c.JSON(http.StatusOK, alpacaImportJobFromEvent(job))
	}
}

func alpacaImportJobFromEvent(j events.AlpacaImportJob) alpacaImportJobResponse {
	return alpacaImportJobResponse{
		JobID:               j.JobID,
		PortfolioID:         j.PortfolioID,
		RequestedByUserID:   j.RequestedByUserID,
		Status:              j.Status,
		Since:               j.Since,
		Until:               j.Until,
		FullHistory:         j.FullHistory,
		InsertedCount:       j.InsertedCount,
		DuplicateCount:      j.DuplicateCount,
		SkippedInvalidCount: j.SkippedInvalidCount,
		PagesFetched:        j.PagesFetched,
		ActivitiesSeen:      j.ActivitiesSeen,
		FillsConsidered:     j.FillsConsidered,
		ErrorMessage:        j.ErrorMessage,
		CreatedAt:           j.CreatedAt.UTC(),
		UpdatedAt:           j.UpdatedAt.UTC(),
		StartedAt:           j.StartedAt,
		FinishedAt:          j.FinishedAt,
	}
}
