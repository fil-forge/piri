package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

func TestSetChallengeStatus(t *testing.T) {
	// Epoch values observed in the FIL-1199 staging incident: 64 epochs
	// before the next challenge, the node reported InChallengeWindow=true
	// and falsely blocked `piri status upgrade-check`.
	const (
		nextChallengeEpoch = int64(4057142)
		challengeWindow    = int64(20)
	)
	pendingHash := "0xchallenge-request"

	testCases := []struct {
		name             string
		next             int64
		window           int64
		currentEpoch     int64
		challengeMsgHash *string
		want             types.ProofSetState
	}{
		{
			name:             "before window start (incident regression)",
			next:             nextChallengeEpoch,
			window:           challengeWindow,
			currentEpoch:     4057078,
			challengeMsgHash: &pendingHash,
			want:             types.ProofSetState{},
		},
		{
			name:             "exactly at window start, challenge pending",
			next:             nextChallengeEpoch,
			window:           challengeWindow,
			currentEpoch:     4057142,
			challengeMsgHash: &pendingHash,
			want:             types.ProofSetState{ChallengedIssued: true, InChallengeWindow: true},
		},
		{
			name:             "inside window, challenge pending",
			next:             nextChallengeEpoch,
			window:           challengeWindow,
			currentEpoch:     4057150,
			challengeMsgHash: &pendingHash,
			want:             types.ProofSetState{ChallengedIssued: true, InChallengeWindow: true},
		},
		{
			name:             "inside window, proven",
			next:             nextChallengeEpoch,
			window:           challengeWindow,
			currentEpoch:     4057150,
			challengeMsgHash: nil,
			want:             types.ProofSetState{ChallengedIssued: true, InChallengeWindow: true, HasProven: true},
		},
		{
			name:             "exactly at window end is neither in window nor fault",
			next:             nextChallengeEpoch,
			window:           challengeWindow,
			currentEpoch:     4057162,
			challengeMsgHash: &pendingHash,
			want:             types.ProofSetState{ChallengedIssued: true},
		},
		{
			name:             "past window end is a fault",
			next:             nextChallengeEpoch,
			window:           challengeWindow,
			currentEpoch:     4057163,
			challengeMsgHash: &pendingHash,
			want:             types.ProofSetState{ChallengedIssued: true, IsInFaultState: true},
		},
		{
			name:             "no challenge scheduled",
			next:             0,
			window:           challengeWindow,
			currentEpoch:     4057150,
			challengeMsgHash: nil,
			want:             types.ProofSetState{},
		},
		{
			name:             "zero challenge window",
			next:             nextChallengeEpoch,
			window:           0,
			currentEpoch:     4057142,
			challengeMsgHash: nil,
			want:             types.ProofSetState{ChallengedIssued: true},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := types.ProofSetState{
				NextChallengeEpoch: tc.next,
				ChallengeWindow:    tc.window,
			}
			tc.want.NextChallengeEpoch = tc.next
			tc.want.ChallengeWindow = tc.window

			setChallengeStatus(&state, tc.currentEpoch, tc.challengeMsgHash)

			require.Equal(t, tc.want, state)
		})
	}
}
