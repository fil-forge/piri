package client

import (
	"context"
	"fmt"
	"time"

	"github.com/fil-forge/piri/pkg/config"
	"github.com/fil-forge/piri/pkg/pdp/httpapi/client"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

// NodeStatus represents the current state of a piri node
type NodeStatus struct {
	Healthy           bool `json:"healthy"`
	IsProving         bool `json:"is_proving"`
	InChallengeWindow bool `json:"in_challenge_window"`
	HasProven         bool `json:"has_proven"`
	InFaultState      bool `json:"in_fault_state"`
	UpgradeSafe       bool `json:"upgrade_safe"`
	// UnsafeReason explains why an upgrade is not safe. Empty when UpgradeSafe is true.
	UnsafeReason  string     `json:"unsafe_reason,omitempty"`
	NextChallenge *time.Time `json:"next_challenge,omitempty"`
}

// GetNodeStatus connects to a running piri node and checks its status
func GetNodeStatus(ctx context.Context) (*NodeStatus, error) {
	cfg, err := config.Load[config.Client]()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	// Create API client to connect to local node
	api, err := client.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating api client: %w", err)
	}

	// Get proof set state
	psState, err := api.GetProofSetState(ctx, cfg.UCAN.ProofSetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get proof set state: %w", err)
	}

	// Calculate if it's safe to upgrade
	unsafeReason := calculateUpgradeSafety(psState)

	return &NodeStatus{
		Healthy:           true, // If we got this far, node is responding
		IsProving:         psState.IsProving,
		InChallengeWindow: psState.InChallengeWindow,
		HasProven:         psState.HasProven,
		InFaultState:      psState.IsInFaultState,
		UpgradeSafe:       unsafeReason == "",
		UnsafeReason:      unsafeReason,
		// NextChallenge could be calculated from psState if needed
	}, nil
}

// calculateUpgradeSafety determines if it's safe to update based on proof set
// state. It returns an empty string when an upgrade is safe, and otherwise a
// human-readable reason naming the condition that blocks the upgrade. The
// reason does not carry a "not safe" prefix; callers add their own framing.
func calculateUpgradeSafety(psState *types.ProofSetState) string {
	// If in fault state, we cannot update
	// TODO/REVIEW: I don't know if we want to allow updates to nodes in fault.
	if psState.IsInFaultState {
		return fmt.Sprintf(
			"proof set %d is in a fault state: challenge window opened at epoch %d (next challenge epoch) and closed at epoch %d without a proof; current epoch is %d",
			psState.ID,
			psState.NextChallengeEpoch,
			psState.NextChallengeEpoch+psState.ChallengeWindow,
			psState.CurrentEpoch,
		)
	}

	// Don't update while actively proving
	if psState.IsProving {
		return fmt.Sprintf(
			"node is currently generating a proof for proof set %d (current epoch %d); wait for it to finish",
			psState.ID,
			psState.CurrentEpoch,
		)
	}

	// Don't update if in challenge window but haven't proven yet
	if psState.InChallengeWindow && !psState.HasProven {
		return fmt.Sprintf(
			"proof set %d is in a challenge window and has not submitted a proof yet: window opened at epoch %d (next challenge epoch) and closes at epoch %d; current epoch is %d",
			psState.ID,
			psState.NextChallengeEpoch,
			psState.NextChallengeEpoch+psState.ChallengeWindow,
			psState.CurrentEpoch,
		)
	}

	// Otherwise it's safe
	return ""
}
