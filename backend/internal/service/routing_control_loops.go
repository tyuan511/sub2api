package service

import (
	"errors"
	"time"
)

type RoutingControlLoop string

const (
	RoutingControlLoopRequestSafety RoutingControlLoop = "request_safety"
	RoutingControlLoopDynamicScore  RoutingControlLoop = "dynamic_score"
	RoutingControlLoopCalibration   RoutingControlLoop = "policy_calibration"
	RoutingControlLoopGovernance    RoutingControlLoop = "product_governance"
)

type RoutingControlLoopContract struct {
	Loop                  RoutingControlLoop
	MinimumCadence        time.Duration
	RequestPath           bool
	MayWriteRequestState  bool
	MayPublishScore       bool
	MayCreateDraft        bool
	RequiresPromotionGate bool
}

var ErrRoutingControlLoopBoundary = errors.New("routing control-loop boundary violation")

// RoutingControlLoopContracts is the executable ownership boundary between
// safety, scoring, offline calibration, and human product governance. Slow
// loops never own request-level breaker/sticky state or live score pointers.
func RoutingControlLoopContracts() map[RoutingControlLoop]RoutingControlLoopContract {
	return map[RoutingControlLoop]RoutingControlLoopContract{
		RoutingControlLoopRequestSafety: {
			Loop: RoutingControlLoopRequestSafety, RequestPath: true, MayWriteRequestState: true,
		},
		RoutingControlLoopDynamicScore: {
			Loop: RoutingControlLoopDynamicScore, MinimumCadence: 30 * time.Second, MayPublishScore: true,
		},
		RoutingControlLoopCalibration: {
			Loop: RoutingControlLoopCalibration, MinimumCadence: 24 * time.Hour, MayCreateDraft: true, RequiresPromotionGate: true,
		},
		RoutingControlLoopGovernance: {
			Loop: RoutingControlLoopGovernance, RequiresPromotionGate: true,
		},
	}
}

func ValidateRoutingControlLoopContract(contract RoutingControlLoopContract) error {
	switch contract.Loop {
	case RoutingControlLoopRequestSafety:
		if !contract.RequestPath || !contract.MayWriteRequestState || contract.MayPublishScore || contract.MayCreateDraft {
			return ErrRoutingControlLoopBoundary
		}
	case RoutingControlLoopDynamicScore:
		if contract.RequestPath || contract.MayWriteRequestState || !contract.MayPublishScore || contract.MayCreateDraft || contract.MinimumCadence < 30*time.Second {
			return ErrRoutingControlLoopBoundary
		}
	case RoutingControlLoopCalibration:
		if contract.RequestPath || contract.MayWriteRequestState || contract.MayPublishScore || !contract.MayCreateDraft || !contract.RequiresPromotionGate || contract.MinimumCadence < 24*time.Hour {
			return ErrRoutingControlLoopBoundary
		}
	case RoutingControlLoopGovernance:
		if contract.RequestPath || contract.MayWriteRequestState || contract.MayPublishScore || contract.MayCreateDraft || !contract.RequiresPromotionGate {
			return ErrRoutingControlLoopBoundary
		}
	default:
		return ErrRoutingControlLoopBoundary
	}
	return nil
}
