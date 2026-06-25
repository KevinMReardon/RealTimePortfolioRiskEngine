package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/runtime"
)

type agentSchedulerStatusResponse struct {
	Enabled         bool   `json:"enabled"`
	Running         bool   `json:"running"`
	Cron            string `json:"cron,omitempty"`
	Timezone        string `json:"timezone,omitempty"`
	LastOutcome     string `json:"last_outcome,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	StartGeneration int64  `json:"start_generation,omitempty"`

	LastTickAt         *string `json:"last_tick_at,omitempty"`
	LastTickFinishedAt *string `json:"last_tick_finished_at,omitempty"`
	NextTickAt         *string `json:"next_tick_at,omitempty"`
	LastSuccessAt      *string `json:"last_successful_scheduled_briefing_at,omitempty"`
	CooldownUntil      *string `json:"cooldown_until,omitempty"`
	NextEligibleAt     *string `json:"next_eligible_at,omitempty"`
	LastRestartAt      *string `json:"last_watchdog_restart_at,omitempty"`
}

func getAgentSchedulerStatusHandler(scheduler *runtime.SchedulerManager, cfgHolder *config.ConfigHolder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if scheduler == nil || cfgHolder == nil {
			respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "agent scheduler not configured", nil)
			return
		}
		cfg := cfgHolder.Get()
		status := scheduler.Status(cfg.AgentBriefingCooldown)
		c.JSON(http.StatusOK, agentSchedulerStatusResponse{
			Enabled:            status.Enabled,
			Running:            status.Running,
			Cron:               status.Cron,
			Timezone:           status.Timezone,
			LastOutcome:        status.LastOutcome,
			LastError:          status.LastError,
			StartGeneration:    status.StartGeneration,
			LastTickAt:         ptrTimeRFC3339(status.LastTickAt),
			LastTickFinishedAt: ptrTimeRFC3339(status.LastTickFinishedAt),
			NextTickAt:         ptrTimeRFC3339(status.NextTickAt),
			LastSuccessAt:      ptrTimeRFC3339(status.LastSuccessAt),
			CooldownUntil:      ptrTimeRFC3339(status.CooldownUntil),
			NextEligibleAt:     ptrTimeRFC3339(status.NextEligibleAt),
			LastRestartAt:      ptrTimeRFC3339(status.LastRestartAt),
		})
	}
}
