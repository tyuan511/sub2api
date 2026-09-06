package service

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// Recovery admission deliberately uses a small, bounded sample before a
// candidate can bypass a stale long-window health gate. The traffic ramp is
// deterministic per session, so retries in one logical session do not flap
// between groups while different new sessions gradually test the route.
const (
	APIKeyRoutingRecoveryMinimumSamples int64   = 3
	APIKeyRoutingRecoverySuccessRate    float64 = 0.90
	APIKeyRoutingRecoveryProbeBPS       int     = 500
	APIKeyRoutingRecoveryEarlyBPS       int     = 2000
	APIKeyRoutingRecoveryMiddleBPS      int     = 5000
	APIKeyRoutingRecoveryFullBPS        int     = 10000
)

func APIKeyRoutingRecoveryTrafficBPS(successes, failures int64) int {
	total := successes + failures
	if total < APIKeyRoutingRecoveryMinimumSamples ||
		float64(successes)/float64(total) < APIKeyRoutingRecoverySuccessRate {
		return 0
	}
	traffic := APIKeyRoutingRecoveryProbeBPS
	switch {
	case successes >= 20:
		traffic = APIKeyRoutingRecoveryFullBPS
	case successes >= 10:
		traffic = APIKeyRoutingRecoveryMiddleBPS
	case successes >= 5:
		traffic = APIKeyRoutingRecoveryEarlyBPS
	}
	// A recent failure keeps recovery in probe mode until the next clean
	// window, even when the aggregate rate still clears the threshold.
	if failures > 0 && traffic > APIKeyRoutingRecoveryEarlyBPS {
		traffic = APIKeyRoutingRecoveryEarlyBPS
	}
	return traffic
}

func APIKeyRoutingRecoverySignal(successes, failures int64) (eligible bool, trafficBPS int, rate float64) {
	total := successes + failures
	if total > 0 {
		rate = float64(successes) / float64(total)
	}
	trafficBPS = APIKeyRoutingRecoveryTrafficBPS(successes, failures)
	return trafficBPS > 0, trafficBPS, rate
}

// APIKeyRoutingRecoveryTrafficAllowed assigns a stable subset of new session
// hashes to a recovering group. Empty session hashes are intentionally denied:
// without a stable identity, admitting the recovery candidate would make
// consecutive requests flap and destroy cache locality.
func APIKeyRoutingRecoveryTrafficAllowed(apiKeyID, groupID int64, sessionHash string, trafficBPS int) bool {
	if apiKeyID <= 0 || groupID <= 0 || sessionHash == "" || trafficBPS <= 0 {
		return false
	}
	if trafficBPS >= APIKeyRoutingRecoveryFullBPS {
		return true
	}
	if trafficBPS > APIKeyRoutingRecoveryFullBPS {
		trafficBPS = APIKeyRoutingRecoveryFullBPS
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", apiKeyID, groupID, sessionHash)))
	value := int(binary.BigEndian.Uint16(digest[:2])) % APIKeyRoutingRecoveryFullBPS
	return value < trafficBPS
}
