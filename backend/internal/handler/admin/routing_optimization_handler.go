package admin

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type RoutingOptimizationHandler struct {
	manager    *service.RoutingArtifactManager
	operations *service.APIKeyRouteOperationsService
}

func NewRoutingOptimizationHandler(manager *service.RoutingArtifactManager, operations *service.APIKeyRouteOperationsService) *RoutingOptimizationHandler {
	return &RoutingOptimizationHandler{manager: manager, operations: operations}
}

type createRoutingArtifactRequest struct {
	ArtifactKind  string          `json:"artifact_kind" binding:"required"`
	Version       string          `json:"version" binding:"required"`
	ParentVersion *string         `json:"parent_version"`
	Platform      string          `json:"platform" binding:"required"`
	ModelFamily   string          `json:"model_family" binding:"required"`
	EndpointKind  string          `json:"endpoint_kind" binding:"required"`
	Preference    *string         `json:"preference"`
	SchemaVersion string          `json:"schema_version" binding:"required"`
	Checksum      string          `json:"checksum" binding:"required"`
	Payload       json.RawMessage `json:"payload" binding:"required"`
	Dependencies  json.RawMessage `json:"dependencies" binding:"required"`
	Lineage       json.RawMessage `json:"lineage" binding:"required"`
}

type promoteRoutingArtifactRequest struct {
	TargetStatus             string `json:"target_status" binding:"required,oneof=shadow canary active"`
	BaselineVersion          string `json:"baseline_version"`
	CanaryAllocationBPS      int    `json:"canary_allocation_bps" binding:"omitempty,min=1,max=10000"`
	CanaryExperimentID       string `json:"canary_experiment_id" binding:"omitempty,max=200"`
	CanaryBucketSaltChecksum string `json:"canary_bucket_salt_checksum" binding:"omitempty,len=64,hexadecimal"`
}

type routingArtifactScopeRequest struct {
	ArtifactKind string  `json:"artifact_kind" binding:"required"`
	Platform     string  `json:"platform" binding:"required"`
	ModelFamily  string  `json:"model_family" binding:"required"`
	EndpointKind string  `json:"endpoint_kind" binding:"required"`
	Preference   *string `json:"preference"`
}

type routingOfflineReplayRequest struct {
	Since *time.Time `json:"since"`
	Until *time.Time `json:"until"`
}

type routingExperimentPauseRequest struct {
	Reason string `json:"reason" binding:"omitempty,max=200"`
}

func (h *RoutingOptimizationHandler) ListArtifacts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.manager.ListArtifacts(c.Request.Context(), c.Query("artifact_kind"), c.Query("status"), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *RoutingOptimizationHandler) GetRuntimeMetrics(c *gin.Context) {
	response.Success(c, service.DefaultRoutingRuntimeMetrics().Snapshot())
}

func (h *RoutingOptimizationHandler) ExplainAPIKeyRoute(c *gin.Context) {
	if h == nil || h.operations == nil {
		response.Error(c, 503, "Routing operations not available")
		return
	}
	apiKeyID, err := strconv.ParseInt(c.Param("api_key_id"), 10, 64)
	if err != nil || apiKeyID <= 0 {
		response.BadRequest(c, "Invalid API key ID")
		return
	}
	explanation, err := h.operations.Explain(c.Request.Context(), apiKeyID, c.Query("model_family"), c.Query("endpoint_kind"), c.Query("session_hash"))
	if err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Success(c, explanation)
}

func (h *RoutingOptimizationHandler) ClearAPIKeyRouteState(c *gin.Context) {
	if h == nil || h.operations == nil {
		response.Error(c, 503, "Routing operations not available")
		return
	}
	var request service.APIKeyRouteClearRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid route-state clear request: "+err.Error())
		return
	}
	result, err := h.operations.ClearState(c.Request.Context(), request)
	if err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *RoutingOptimizationHandler) CreateArtifact(c *gin.Context) {
	var request createRoutingArtifactRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid routing artifact: "+err.Error())
		return
	}
	artifact := &service.RoutingArtifactVersion{
		ArtifactKind: request.ArtifactKind, Version: request.Version, ParentVersion: request.ParentVersion,
		Platform: request.Platform, ModelFamily: request.ModelFamily, EndpointKind: request.EndpointKind,
		Preference: request.Preference, Status: service.RoutingLifecycleDraft, SchemaVersion: request.SchemaVersion,
		Checksum: request.Checksum, Payload: request.Payload, Dependencies: request.Dependencies, Lineage: request.Lineage,
	}
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok && subject.UserID > 0 {
		artifact.CreatedBy = &subject.UserID
	}
	if err := h.manager.CreateArtifact(c.Request.Context(), artifact); err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Created(c, artifact)
}

func (h *RoutingOptimizationHandler) PromoteArtifact(c *gin.Context) {
	var request promoteRoutingArtifactRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid promotion: "+err.Error())
		return
	}
	promotion := service.RoutingArtifactPromotion{
		ArtifactKind: c.Param("kind"), Version: c.Param("version"), TargetStatus: request.TargetStatus,
		BaselineVersion: request.BaselineVersion, CanaryAllocationBPS: request.CanaryAllocationBPS,
		CanaryExperimentID: request.CanaryExperimentID, CanaryBucketSaltChecksum: request.CanaryBucketSaltChecksum,
	}
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok && subject.UserID > 0 {
		promotion.ApprovedBy = &subject.UserID
	}
	pointers, err := h.manager.Promote(c.Request.Context(), promotion)
	if err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Success(c, pointers)
}

func (h *RoutingOptimizationHandler) Rollback(c *gin.Context) {
	var request routingArtifactScopeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid rollback scope: "+err.Error())
		return
	}
	pointers, err := h.manager.RollbackToBaseline(c.Request.Context(), service.RoutingArtifactScope{
		ArtifactKind: request.ArtifactKind, Platform: request.Platform, ModelFamily: request.ModelFamily,
		EndpointKind: request.EndpointKind, Preference: request.Preference,
	})
	if err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Success(c, pointers)
}

func (h *RoutingOptimizationHandler) GetPointers(c *gin.Context) {
	preference := c.Query("preference")
	var preferencePtr *string
	if preference != "" {
		preferencePtr = &preference
	}
	pointers, err := h.manager.LoadPointers(c.Request.Context(), service.RoutingArtifactScope{
		ArtifactKind: c.Query("artifact_kind"), Platform: c.Query("platform"), ModelFamily: c.Query("model_family"),
		EndpointKind: c.Query("endpoint_kind"), Preference: preferencePtr,
	})
	if err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Success(c, pointers)
}

func (h *RoutingOptimizationHandler) ListExperiments(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.manager.ListExperiments(c.Request.Context(), c.Query("status"), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *RoutingOptimizationHandler) CreateExperiment(c *gin.Context) {
	var experiment service.RoutingExperiment
	if err := c.ShouldBindJSON(&experiment); err != nil {
		response.BadRequest(c, "Invalid routing experiment: "+err.Error())
		return
	}
	experiment.ID = 0
	experiment.Status = service.RoutingLifecycleDraft
	experiment.StartedAt, experiment.StoppedAt, experiment.StopReason = nil, nil, nil
	if err := h.manager.CreateExperiment(c.Request.Context(), &experiment); err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Created(c, &experiment)
}

func (h *RoutingOptimizationHandler) RunOfflineReplay(c *gin.Context) {
	var request routingOfflineReplayRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid offline replay window: "+err.Error())
		return
	}
	var since, until time.Time
	if request.Since != nil {
		since = *request.Since
	}
	if request.Until != nil {
		until = *request.Until
	}
	report, err := h.manager.RunOfflineReplay(c.Request.Context(), c.Param("experiment_key"), since, until)
	if err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Success(c, report)
}

func (h *RoutingOptimizationHandler) PauseExperiment(c *gin.Context) {
	var request routingExperimentPauseRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid pause reason: "+err.Error())
		return
	}
	experiment, err := h.manager.PauseExperiment(c.Request.Context(), c.Param("experiment_key"), request.Reason)
	if err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Success(c, experiment)
}

func (h *RoutingOptimizationHandler) ResumeExperiment(c *gin.Context) {
	experiment, err := h.manager.ResumeExperimentShadow(c.Request.Context(), c.Param("experiment_key"))
	if err != nil {
		routingOptimizationError(c, err)
		return
	}
	response.Success(c, experiment)
}

func routingOptimizationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrRoutingArtifactInvalid), errors.Is(err, service.ErrAPIKeyRouteOperationInvalid):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrAPIKeyRouteVersionStale):
		response.Error(c, 409, err.Error())
	case errors.Is(err, service.ErrRoutingArtifactNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrRoutingLifecycleConflict), errors.Is(err, service.ErrRoutingArtifactPointerConflict), errors.Is(err, service.ErrRoutingPromotionEvidence):
		response.Error(c, 409, err.Error())
	default:
		response.ErrorFrom(c, err)
	}
}
