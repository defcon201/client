package ephemeral

import (
	"testing"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func TestTeamEKBoxStorage(t *testing.T) {
	tc := ephemeralKeyTestSetup(t)
	defer tc.Cleanup()

	merkleRootPtr, err := tc.G.GetMerkleClient().FetchRootFromServer(context.Background(), libkb.EphemeralKeyMerkleFreshness)
	require.NoError(t, err)
	merkleRoot := *merkleRootPtr

	deviceEKMetadata, err := publishNewDeviceEK(context.Background(), tc.G, merkleRoot)
	require.NoError(t, err)

	userEKMetadata, err := publishNewUserEK(context.Background(), tc.G, merkleRoot)
	require.NoError(t, err)

	teamID := createTeam(tc)
	invalidID := teamID + keybase1.TeamID("foo")

	teamEKMetadata, err := publishNewTeamEK(context.Background(), tc.G, teamID, merkleRoot)
	require.NoError(t, err)

	s := tc.G.GetTeamEKBoxStorage()

	// Test Get nonexistent
	nonexistent, err := s.Get(context.Background(), teamID, teamEKMetadata.Generation+1)
	require.Error(t, err)
	require.Equal(t, keybase1.TeamEk{}, nonexistent)

	// Test invalid teamID
	nonexistent2, err := s.Get(context.Background(), invalidID, teamEKMetadata.Generation+1)
	require.Error(t, err)
	require.Equal(t, keybase1.TeamEk{}, nonexistent2)

	// Test get valid & unbox
	teamEK, err := s.Get(context.Background(), teamID, teamEKMetadata.Generation)
	require.NoError(t, err)

	verifyTeamEK(t, teamEKMetadata, teamEK)

	// Test MaxGeneration
	maxGeneration, err := s.MaxGeneration(context.Background(), teamID)
	require.NoError(t, err)
	require.EqualValues(t, 1, maxGeneration)

	// Invalid id
	maxGeneration2, err := s.MaxGeneration(context.Background(), invalidID)
	require.NoError(t, err)
	require.EqualValues(t, -1, maxGeneration2)

	//	NOTE: We don't expose Delete on the interface put on the GlobalContext
	//	since they should never be called, only DeleteExpired should be used.
	//	GetAll is also not exposed since it' only needed for tests.
	rawTeamEKBoxStorage := NewTeamEKBoxStorage(tc.G)
	teamEKs, err := rawTeamEKBoxStorage.GetAll(context.Background(), teamID)
	require.NoError(t, err)
	require.Equal(t, 1, len(teamEKs))

	teamEK, ok := teamEKs[teamEKMetadata.Generation]
	require.True(t, ok)

	verifyTeamEK(t, teamEKMetadata, teamEK)

	// Test invalid
	teamEKs2, err := rawTeamEKBoxStorage.GetAll(context.Background(), invalidID)
	require.NoError(t, err)
	require.Equal(t, 0, len(teamEKs2))

	// Let's delete our userEK and verify we will refetch and unbox properly
	rawUserEKBoxStorage := NewUserEKBoxStorage(tc.G)
	err = rawUserEKBoxStorage.Delete(context.Background(), userEKMetadata.Generation)
	require.NoError(t, err)

	userStorage := tc.G.GetUserEKBoxStorage()
	userStorage.ClearCache()

	teamEK, err = s.Get(context.Background(), teamID, teamEKMetadata.Generation)
	require.NoError(t, err)
	verifyTeamEK(t, teamEKMetadata, teamEK)

	// No let's the deviceEK which we can't recover from
	rawDeviceEKStorage := NewDeviceEKStorage(tc.G)
	err = rawDeviceEKStorage.Delete(context.Background(), deviceEKMetadata.Generation)
	require.NoError(t, err)

	deviceStorage := tc.G.GetDeviceEKStorage()
	deviceStorage.ClearCache()
	deviceEK, err := deviceStorage.Get(context.Background(), deviceEKMetadata.Generation)
	require.Error(t, err)
	require.Equal(t, keybase1.DeviceEk{}, deviceEK)

	bad, err := s.Get(context.Background(), teamID, teamEKMetadata.Generation)
	require.Error(t, err)
	require.Equal(t, keybase1.TeamEk{}, bad)

	// test delete
	err = rawTeamEKBoxStorage.Delete(context.Background(), teamID, teamEKMetadata.Generation)
	require.NoError(t, err)
	// delete invalid
	err = rawTeamEKBoxStorage.Delete(context.Background(), invalidID, teamEKMetadata.Generation)
	require.NoError(t, err)

	teamEKs, err = rawTeamEKBoxStorage.GetAll(context.Background(), teamID)
	require.NoError(t, err)
	require.Equal(t, 0, len(teamEKs))

	s.ClearCache()

	maxGeneration3, err := s.MaxGeneration(context.Background(), teamID)
	require.NoError(t, err)
	require.EqualValues(t, -1, maxGeneration3)

	expired, err := s.DeleteExpired(context.Background(), teamID, merkleRoot)
	expected := []keybase1.EkGeneration(nil)
	require.NoError(t, err)
	require.Equal(t, expected, expired)
}
