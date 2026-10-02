package template

import (
	"maps"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

// YouTube outbox seed template을 예시 값으로 렌더링한 최종 본문을 고정한다. 이 패키지가 template 본문을 소유하므로
// 렌더러(alarm-worker) 쪽 테스트는 본문을 복사해 렌더링 결과를 검증하지 않는다.
func TestSeedTemplates_OutboxRenderGoldens(t *testing.T) {
	pool := dbtest.NewPool(t)
	zwsp := util.KakaoZeroWidthSpace
	groupSeparator := "──────────"

	cases := []struct {
		name string
		key  domain.TemplateKey
		kind string
		want string
	}{
		{
			name: "single/new_video", key: domain.TemplateKeyOutboxVideo, kind: "NEW_VIDEO",
			want: "🔔 사쿠라 미코 새 영상\n" + zwsp + "마인크래프트 건축 배틀 #" + zwsp + "미코라이브\nhttps://youtu.be/video123xyz",
		},
		{
			name: "single/live_stream", key: domain.TemplateKeyOutboxVideo, kind: "LIVE_STREAM",
			want: "🔴 사쿠라 미코 방송 시작\n" + zwsp + "마인크래프트 건축 배틀 #" + zwsp + "미코라이브\nhttps://youtu.be/video123xyz",
		},
		{
			name: "single/shorts", key: domain.TemplateKeyOutboxShorts, kind: "NEW_SHORT",
			want: "🔔 사쿠라 미코 새 쇼츠\n" + zwsp + "새 쇼츠 제목 - 귀여운 미코치\nhttps://www.youtube.com/shorts/abc123xyz",
		},
		{
			name: "single/community", key: domain.TemplateKeyOutboxCommunity, kind: "COMMUNITY_POST",
			want: "🔔 사쿠라 미코 커뮤니티 글\n" + zwsp + "오늘 밤 10시에 방송합니다! 많이 놀러오세요~" + zwsp + "\nhttps://www.youtube.com/post/Ugkxyz123",
		},
		{
			name: "group/new_video", key: domain.TemplateKeyOutboxVideoGroup, kind: "NEW_VIDEO",
			want: "🔔 사쿠라 미코 새 영상 · 2개\n\n1 · 마인크래프트 건축 배틀 #" + zwsp + "1\nhttps://youtu.be/group-video-1\n" + groupSeparator + "\n2 · 마인크래프트 건축 배틀 #" + zwsp + "2\nhttps://youtu.be/group-video-2",
		},
		{
			name: "group/live_stream", key: domain.TemplateKeyOutboxVideoGroup, kind: "LIVE_STREAM",
			want: "🔴 사쿠라 미코 방송 시작 · 2개\n\n1 · 마인크래프트 건축 배틀 #" + zwsp + "1\nhttps://youtu.be/group-video-1\n" + groupSeparator + "\n2 · 마인크래프트 건축 배틀 #" + zwsp + "2\nhttps://youtu.be/group-video-2",
		},
		{
			name: "group/shorts", key: domain.TemplateKeyOutboxShortsGroup, kind: "NEW_SHORT",
			want: "🔔 사쿠라 미코 새 쇼츠 · 2개\n\n1 · 오늘의 쇼츠 #" + zwsp + "1\nhttps://www.youtube.com/shorts/group-1\n" + groupSeparator + "\n2 · 오늘의 쇼츠 #" + zwsp + "2\nhttps://www.youtube.com/shorts/group-2",
		},
		{
			name: "group/community", key: domain.TemplateKeyOutboxCommunityGroup, kind: "COMMUNITY_POST",
			want: "🔔 사쿠라 미코 커뮤니티 글 · 2개\n\n1 · 오늘 밤 10시 방송 공지\nhttps://www.youtube.com/post/group-community-1\n" + groupSeparator + "\n2 · 굿즈 판매 시작 안내\nhttps://www.youtube.com/post/group-community-2",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, ok := sampledata.GetTemplateSampleData(c.key).(map[string]any)
			if !ok {
				t.Fatalf("sample data for %s is not map[string]any", c.key)
			}

			data := maps.Clone(src)

			data["Kind"] = c.kind

			if got := renderSeedBody(t, c.key, seedBody(t, pool, c.key), data); got != c.want {
				t.Fatalf("render mismatch\n got=%q\nwant=%q", got, c.want)
			}
		})
	}
}
