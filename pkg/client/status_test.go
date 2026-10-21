package client

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

func TestCalculateUpgradeSafety(t *testing.T) {
	testCases := []struct {
		name  string
		state types.ProofSetState
		// want is the exact reason; empty means safe.
		want string
	}{
		{
			name: "fault state blocks upgrade",
			state: types.ProofSetState{
				ID:                 7,
				NextChallengeEpoch: 1000,
				ChallengeWindow:    60,
				CurrentEpoch:       1100,
				IsInFaultState:     true,
			},
			want: "proof set 7 is in a fault state: challenge window opened at epoch 1000 (next challenge epoch) and closed at epoch 1060 without a proof; current epoch is 1100",
		},
		{
			name: "fault state takes precedence over proving",
			state: types.ProofSetState{
				ID:                 7,
				NextChallengeEpoch: 1000,
				ChallengeWindow:    60,
				CurrentEpoch:       1100,
				IsInFaultState:     true,
				IsProving:          true,
			},
			want: "proof set 7 is in a fault state: challenge window opened at epoch 1000 (next challenge epoch) and closed at epoch 1060 without a proof; current epoch is 1100",
		},
		{
			name: "active proving blocks upgrade",
			state: types.ProofSetState{
				ID:                 7,
				NextChallengeEpoch: 1000,
				ChallengeWindow:    60,
				CurrentEpoch:       1010,
				InChallengeWindow:  true,
				IsProving:          true,
			},
			want: "node is currently generating a proof for proof set 7 (current epoch 1010); wait for it to finish",
		},
		{
			name: "unproven challenge window blocks upgrade",
			state: types.ProofSetState{
				ID:                 7,
				NextChallengeEpoch: 1000,
				ChallengeWindow:    60,
				CurrentEpoch:       1020,
				InChallengeWindow:  true,
			},
			want: "proof set 7 is in a challenge window and has not submitted a proof yet: window opened at epoch 1000 (next challenge epoch) and closes at epoch 1060; current epoch is 1020",
		},
		{
			name:  "proven challenge window allows upgrade",
			state: types.ProofSetState{InChallengeWindow: true, HasProven: true},
			want:  "",
		},
		{
			name:  "outside challenge window with no blockers allows upgrade",
			state: types.ProofSetState{},
			want:  "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := calculateUpgradeSafety(&tc.state)
			require.Equal(t, tc.want, got)
			// Callers add their own "not safe" framing; the reason must not.
			require.NotContains(t, strings.ToLower(got), "not safe")
			require.NotContains(t, strings.ToLower(got), "busy")
		})
	}
}
