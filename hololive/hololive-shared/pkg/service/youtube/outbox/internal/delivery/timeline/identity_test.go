package timeline

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
)

func TestNormalizePostTrackingIdentities_EmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	got, err := NormalizePostTrackingIdentities(nil)
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = NormalizePostTrackingIdentities([]PostTrackingIdentity{})
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestNormalizePostTrackingIdentities_KeepsSupportedKindsAndTrimsContentID(t *testing.T) {
	t.Parallel()

	got, err := NormalizePostTrackingIdentities([]PostTrackingIdentity{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "  post-a  "},
		{Kind: domain.OutboxKindNewShort, ContentID: "short-b"},
	})
	require.NoError(t, err)
	require.Equal(t, []PostTrackingIdentity{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "post-a"},
		{Kind: domain.OutboxKindNewShort, ContentID: "short-b"},
	}, got)
}

func TestNormalizePostTrackingIdentities_BlankContentIDSkippedYieldsNonNilEmpty(t *testing.T) {
	t.Parallel()

	got, err := NormalizePostTrackingIdentities([]PostTrackingIdentity{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "   "},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got, 0)
}

func TestNormalizePostTrackingIdentities_DeduplicatesByTrimmedKindAndContentID(t *testing.T) {
	t.Parallel()

	got, err := NormalizePostTrackingIdentities([]PostTrackingIdentity{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "post-a"},
		{Kind: domain.OutboxKindCommunityPost, ContentID: "  post-a  "},
	})
	require.NoError(t, err)
	require.Equal(t, []PostTrackingIdentity{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "post-a"},
	}, got)
}

func TestNormalizePostTrackingIdentities_SameContentIDDifferentKindsBothKept(t *testing.T) {
	t.Parallel()

	got, err := NormalizePostTrackingIdentities([]PostTrackingIdentity{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "id"},
		{Kind: domain.OutboxKindNewShort, ContentID: "id"},
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestNormalizePostTrackingIdentities_UnsupportedKindsError(t *testing.T) {
	t.Parallel()

	for _, kind := range []domain.OutboxKind{
		domain.OutboxKindNewVideo,
		domain.OutboxKindLiveStream,
		domain.OutboxKindMilestone,
		domain.OutboxKind("UNKNOWN_KIND"),
	} {
		got, err := NormalizePostTrackingIdentities([]PostTrackingIdentity{
			{Kind: kind, ContentID: "content"},
		})
		require.Error(t, err, "kind=%s", kind)
		require.Nil(t, got, "kind=%s", kind)
		require.Contains(t, err.Error(), "unsupported tracking identity kind")
	}
}

func TestNormalizePostTrackingIdentities_UnsupportedKindWithBlankContentIDIsSkippedNotErrored(t *testing.T) {
	t.Parallel()

	got, err := NormalizePostTrackingIdentities([]PostTrackingIdentity{
		{Kind: domain.OutboxKindNewVideo, ContentID: "   "},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got, 0)
}

func TestNormalizePostTrackingIdentities_ErrorAbortsWholeBatch(t *testing.T) {
	t.Parallel()

	got, err := NormalizePostTrackingIdentities([]PostTrackingIdentity{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "valid"},
		{Kind: domain.OutboxKindNewVideo, ContentID: "unsupported"},
	})
	require.Error(t, err)
	require.Nil(t, got)
}

func TestPostTrackingIdentityKey_BlankContentIDReturnsEmpty(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", PostTrackingIdentityKey(domain.OutboxKindCommunityPost, ""))
	require.Equal(t, "", PostTrackingIdentityKey(domain.OutboxKindCommunityPost, "   "))
}

func TestPostTrackingIdentityKey_JoinsKindAndTrimmedContentID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "COMMUNITY_POST:abc", PostTrackingIdentityKey(domain.OutboxKindCommunityPost, "abc"))
	require.Equal(t, "COMMUNITY_POST:abc", PostTrackingIdentityKey(domain.OutboxKindCommunityPost, "  abc  "))
}

func TestPostTrackingIdentityKey_DoesNotValidateKind(t *testing.T) {
	t.Parallel()

	require.Equal(t, "NEW_VIDEO:x", PostTrackingIdentityKey(domain.OutboxKindNewVideo, "x"))
	require.Equal(t, "UNKNOWN_KIND:x", PostTrackingIdentityKey(domain.OutboxKind("UNKNOWN_KIND"), "x"))
}
