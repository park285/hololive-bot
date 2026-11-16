package scheduleposter

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	collabVideo  = "Kpe1dOPOntI"
	melaPoster   = 8
	irohaPoster  = 9
	collabStart  = "Start: 2026-11-12 19:00 JST"
	collabTitle  = "Title: 【オフコラボ】クッキーバトル 🔥めらVSそぴあ"
	guestEntry   = "Official schedule entry (this streamer is a guest):\n" + collabStart + "\n" + collabTitle
	hostEntry    = "Official schedule entry (this streamer is the host):\n" + collabStart + "\n" + collabTitle
	collabAnswer = "明日のコラボはこちら！"
)

var iroha = &domain.Member{ChannelID: "UCiroha", Name: "Kazama Iroha", NameJa: "風真いろは", NameKo: "카자마 이로하"}

func collabStream() posterstore.OfficialStream {
	return posterstore.OfficialStream{
		VideoID: collabVideo, Title: "【オフコラボ】クッキーバトル\n🔥めらVSそぴあ", StartsAt: at("2026-11-12 19:00"),
		Guests: []string{"宙科そぴあ", "熱千めら", "外部ゲスト"}, ChannelID: iroha.ChannelID,
	}
}

func newCollabFixture(pages ...status) *changeFixture {
	f := newChangeFixture(pages...)

	f.members.members = []*domain.Member{sophia, mela, iroha}
	f.service.config.Targets = append(f.service.config.Targets, Target{ChannelID: mela.ChannelID}, Target{ChannelID: iroha.ChannelID})
	f.store.posters = map[string]int64{mela.ChannelID: melaPoster, iroha.ChannelID: irohaPoster}
	f.store.streams = map[string]posterstore.OfficialStream{collabVideo: collabStream()}

	return f
}

func (f *changeFixture) storedBody(t *testing.T, posterID, id int64) string {
	t.Helper()

	post := f.store.findFor(posterID, id)
	require.NotNil(t, post)

	return post.Body
}

func collabAnnouncement() status {
	announce := xStatus(301, at("2026-11-11 10:00"), collabAnswer)

	announce.Author.Name = " 宙科そぴあ\n🪐 "
	announce.Quote = quotedPost("3003", "風真いろは🍃", at("2026-11-11 09:30"), "📢明日19時\nhttps://youtube.com/live/"+collabVideo+"?feature=share")

	return announce
}

func TestLinkedVideosReadsTheStreamLinksOfAPost(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{collabVideo, "abcdefghi_-", "ZYXWVUTSRQP"}, linkedVideos(
		"https://youtube.com/live/"+collabVideo+"?feature=share と https://youtu.be/abcdefghi_-\nhttps://youtube.com/live/"+collabVideo,
		"https://www.youtube.com/watch?app=desktop&v=ZYXWVUTSRQP&t=10s https://youtu.be/11111111111",
	), "같은 영상은 한 번만, 글 하나에서 세 개까지 읽는다")
	require.Empty(t, linkedVideos(
		"https://youtube.com/shorts/"+collabVideo+" https://youtube.com/@kazamairohach https://youtu.be/short https://youtube.com/live/"+collabVideo+"x"))
}

func TestDetectChangesGivesALinkedCollabToEveryGuestTarget(t *testing.T) {
	t.Parallel()

	f := newCollabFixture(collabAnnouncement())

	f.tick(t)

	own, guest := f.store.findFor(changeSchedule().PosterID, 301), f.store.findFor(melaPoster, 301)
	require.NotNil(t, own)
	require.NotNil(t, guest)
	require.Equal(t, collabAnswer+"\n\nQuoted post by 風真いろは🍃 at 2026-11-11 09:30 JST:\n📢明日19時\nhttps://youtube.com/live/"+collabVideo+
		"?feature=share\n\n"+guestEntry+"\nChannel: 風真いろは\nWith: 風真いろは, 熱千めら, 外部ゲスト", own.Body)
	require.Equal(t, "Post by 宙科そぴあ 🪐 about a stream this streamer joins.\n\n"+guestEntry+
		"\nChannel: 風真いろは\nWith: 風真いろは, 宙科そぴあ, 外部ゲスト", guest.Body)
	require.True(t, guest.PostedAt.Equal(own.PostedAt))
	require.Nil(t, f.store.findFor(irohaPoster, 301), "방송을 여는 멤버 앞으로는 다른 사람의 글을 넣지 않는다")
	require.Equal(t, [][]string{{collabVideo}}, f.store.streamCalls)
	require.ElementsMatch(t, []string{own.Body, guest.Body}, f.classifier.calls, "다른 출연자 앞으로 넣은 글도 따로 판별한다")
	require.Equal(t, posterstore.PostMatched, own.Status)
	require.Equal(t, posterstore.PostMatched, guest.Status)
}

func TestDetectChangesTellsTheGuestsOfAHostsOwnAnnouncement(t *testing.T) {
	t.Parallel()

	f := newCollabFixture(xStatus(302, at("2026-11-11 10:00"), "明日19時からコラボ！ https://youtu.be/"+collabVideo))
	stream := collabStream()

	stream.ChannelID, stream.Guests = sophiaChannel, []string{"熱千めら", "風真いろは", "宙科そぴあ"}
	f.store.streams[collabVideo] = stream

	f.tick(t)

	require.Equal(t, "明日19時からコラボ！ https://youtu.be/"+collabVideo+"\n\n"+hostEntry+"\nWith: 熱千めら, 風真いろは",
		f.storedBody(t, changeSchedule().PosterID, 302))
	require.Equal(t, "Post by another streamer about a stream this streamer joins.\n\n"+guestEntry+"\nChannel: 宙科そぴあ\nWith: 宙科そぴあ, 風真いろは",
		f.storedBody(t, melaPoster, 302))
	require.Equal(t, "Post by another streamer about a stream this streamer joins.\n\n"+guestEntry+"\nChannel: 宙科そぴあ\nWith: 宙科そぴあ, 熱千めら",
		f.storedBody(t, irohaPoster, 302))
}

func TestDetectChangesLeavesOutStreamsThePostDoesNotAnnounce(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]func(f *changeFixture){
		"이미 시작한 방송": func(f *changeFixture) {
			stream := collabStream()

			stream.StartsAt = at("2026-11-11 09:00")
			f.store.streams[collabVideo] = stream
		},
		"글쓴이가 출연하지 않는 방송": func(f *changeFixture) {
			stream := collabStream()

			stream.Guests = []string{"熱千めら"}
			f.store.streams[collabVideo] = stream
		},
		"공식 일정에 없는 방송": func(f *changeFixture) { f.store.streams = nil },
	} {
		f := newCollabFixture(collabAnnouncement())

		edit(f)
		f.tick(t)

		own := f.store.findFor(changeSchedule().PosterID, 301)
		require.NotNil(t, own, name)
		require.NotContains(t, own.Body, "Official schedule entry", name)
		require.Nil(t, f.store.findFor(melaPoster, 301), name)
	}

	f := newCollabFixture(postStatus(postA))

	f.tick(t)
	require.Empty(t, f.store.streamCalls, "영상 주소가 없는 글은 공식 일정을 찾지 않는다")
}

func TestDetectChangesListsALargeCastOnlyInPart(t *testing.T) {
	t.Parallel()

	f := newCollabFixture(collabAnnouncement())
	stream := collabStream()

	stream.Guests = []string{"宙科そぴあ", "A", "B", "C", "D", "E", "F", "G", "H", "I"}
	f.store.streams[collabVideo] = stream

	f.tick(t)

	require.True(t, strings.HasSuffix(f.storedBody(t, changeSchedule().PosterID, 301), "\nWith: 風真いろは, A, B, C, D, E, F, G, and 2 others"))
}

func TestDetectChangesStoresNothingWhenTheOfficialScheduleCannotBeRead(t *testing.T) {
	t.Parallel()

	f := newCollabFixture(collabAnnouncement())

	f.store.streamErr = errors.New("connection refused")
	f.tickStoreError(t)
	require.Empty(t, f.store.posts, "공식 일정 없이 넣은 글은 나중에 본문이 달라진다")

	f.store.streamErr = nil
	f.tick(t)
	require.NotNil(t, f.store.findFor(melaPoster, 301))
}

func TestDetectChangesKeepsAnotherMembersPostUntilThatMemberIsProcessed(t *testing.T) {
	t.Parallel()

	f := newCollabFixture(collabAnnouncement())

	f.service.config.Targets = []Target{{ChannelID: mela.ChannelID}, {XUserID: changeUser, ChannelID: sophiaChannel}}

	f.tick(t)
	require.Nil(t, f.store.findFor(melaPoster, 301))

	f.sources.pages = map[string]statusPage{"": statusesPage("")}
	f.tick(t)
	require.NotNil(t, f.store.findFor(melaPoster, 301), "글쓴이보다 먼저 처리한 멤버는 다음 주기에 받는다")

	f.clock = f.clock.Add(lookback + 1)
	f.tick(t)
	require.Empty(t, f.service.changeLoop.mentions)
}

func TestDetectChangesMatchesARejudgedPostFromItsNewBody(t *testing.T) {
	t.Parallel()

	f := newChangeFixture(postStatus(postA))

	f.store.matchErrs = 1
	f.tickStoreError(t)
	require.Len(t, f.matcher.prompts, 1)

	stored := f.store.find(postA.ID)
	require.NotNil(t, stored)

	stored.Status, stored.Body = posterstore.PostUnjudged, postA.Body+"\n\nQuoted post by 霧島なぎ at 2026-11-11 09:00 JST:\n22時から"

	f.tick(t)

	require.Len(t, f.matcher.prompts, 2, "본문이 늘어 다시 판별한 글은 앞서 계산해 둔 대응을 쓰지 않는다")
	require.True(t, strings.HasSuffix(f.matcher.prompts[1], stored.Body))
}
