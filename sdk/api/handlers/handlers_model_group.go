package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/modelgroup"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func ginKeyConfigs(ctx context.Context) (*internalconfig.APIKeyConfig, *internalconfig.ModelGroup) {
	if ctx == nil {
		return nil, nil
	}
	ginCtx, _ := ctx.Value("gin").(*gin.Context)
	if ginCtx == nil {
		return nil, nil
	}
	keyConfig, _ := ginCtx.Get("apiKeyConfig")
	group, _ := ginCtx.Get("modelGroup")
	kc, _ := keyConfig.(*internalconfig.APIKeyConfig)
	mg, _ := group.(*internalconfig.ModelGroup)
	return kc, mg
}

func modelGroupForRequest(ctx context.Context, model string, resolved bool) (*internalconfig.ModelGroup, *interfaces.ErrorMessage) {
	if resolved {
		return nil, nil
	}
	keyConfig, group := ginKeyConfigs(ctx)
	if err := modelgroup.CheckModelAccess(keyConfig, model); err != nil {
		return nil, executionErrorMessage(err)
	}
	if modelgroup.IsGroupModel(model, group) {
		return group, nil
	}
	return nil, nil
}

func isQuotaExhausted(err error) bool { return shouldModelGroupFailover(err) }

func shouldModelGroupFailover(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if statusErr, ok := err.(interface{ StatusCode() int }); ok {
		switch statusErr.StatusCode() {
		case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden,
			http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	var authErr *coreauth.Error
	return errors.As(err, &authErr) && authErr != nil && (authErr.Code == "auth_not_found" || authErr.Code == "auth_unavailable")
}

func modelGroupCandidates(group *internalconfig.ModelGroup) []string {
	if group == nil {
		return nil
	}
	tiers := modelgroup.GroupByPriority(group.Models)
	models := make([]string, 0, len(group.Models))
	for _, tier := range tiers {
		models = append(models, tier.Models...)
	}
	return models
}
