package workerapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommunityOutboxThumbnailURLFallsBackToAuthorPhoto(t *testing.T) {
	data := alarmDispatchKaringCommunityPayload{
		Images: []alarmDispatchKaringImagePayload{
			{URL: " "},
		},
		AuthorPhoto: []alarmDispatchKaringImagePayload{
			{URL: "//yt3.ggpht.com/avatar-small=s88", Width: 88, Height: 88},
			{URL: "https://yt3.ggpht.com/avatar-large=s800", Width: 800, Height: 800},
		},
	}

	got := communityOutboxThumbnailURL(&data)

	assert.Equal(t, "https://yt3.ggpht.com/avatar-large=s800", got)
}

func TestCleanCommunityOutboxTitleKeepsFirstFourContentLines(t *testing.T) {
	got := cleanCommunityOutboxTitle("／\\n첫번째\\n두번째\\n세번째\\n네번째\\n다섯번째\\n＼")

	assert.Equal(t, "첫번째 두번째 세번째 네번째", got)
}
