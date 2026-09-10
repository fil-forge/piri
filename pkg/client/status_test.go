package client

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

func TestCalculateUpgradeSafety(t *testing.T) {
	testCases := []struct {
		name  string
		state types.ProofSetState
		want  bool
	}{
		{
			name:  "fault state blocks upgrade",
			state: types.ProofSetState{IsInFaultState: true},
			want:  false,
		},
		{
			name:  "active proving blocks upgrade",
			state: types.ProofSetState{IsProving: true},
			want:  false,
		},
		{
			name:  "unproven challenge window blocks upgrade",
			state: types.ProofSetState{InChallengeWindow: true},
			want:  false,
		},
		{
			name:  "proven challenge window allows upgrade",
			state: types.ProofSetState{InChallengeWindow: true, HasProven: true},
			want:  true,
		},
		{
			name:  "outside challenge window with no blockers allows upgrade",
			state: types.ProofSetState{},
			want:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, calculateUpgradeSafety(&tc.state))
		})
	}
}
