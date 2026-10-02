import { Innertube } from "youtubei.js";
import { createCollectionClient } from "../collection-client.mjs";

const fixtureOverrides = new WeakMap();
const tabMethods = new Map([
  ["getVideos", "videos"], ["getShorts", "shorts"], ["getCommunity", "posts"], ["getLiveStreams", "streams"],
]);

/** 원시 증거는 실제 adapter로 만들고 매핑 단위 테스트의 getter만 교체합니다. */
export async function channelFixture(overrides = {}, tabs = ["featured", "videos", "shorts", "posts", "streams"], selected = "featured") {
  const innertube = await Innertube.create({
    retrieve_player: false, generate_session_locally: true, enable_session_cache: false,
    retrieve_innertube_config: false,
    fetch: async () => new Response(JSON.stringify({ contents: { twoColumnBrowseResultsRenderer: {
      tabs: tabs.map(tab => ({ tabRenderer: {
        title: tab,
        selected: tab === selected,
        endpoint: {
          commandMetadata: { webCommandMetadata: { url: `/channel/UC_TEST/${tab}`, apiUrl: "/youtubei/v1/browse" } },
          browseEndpoint: { browseId: "UC_TEST" },
        },
        content: { sectionListRenderer: { contents: [] } },
      } })),
    } } })),
  });
  const channel = await createCollectionClient(innertube).getChannel("UC_TEST");
  const descriptors = Object.getOwnPropertyDescriptors(overrides);
  for (const [method, tab] of tabMethods) {
    const load = descriptors[method]?.value;
    if (typeof load !== "function") continue;
    descriptors[method].value = async () => {
      const feed = await load.call(channel);
      const feedOverrides = fixtureOverrides.get(feed);
      return feedOverrides === undefined ? feed : channelFixture(feedOverrides, [tab], tab);
    };
  }
  Object.defineProperties(channel, descriptors);
  fixtureOverrides.set(channel, overrides);
  return channel;
}
