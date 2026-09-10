import { createHash } from "node:crypto";

const firstID = 9007199254740993n;
const members = Array.from({ length: 1000 }, (_, index) => ({ id: firstID + BigInt(index), channelId: `UC${String(index).padStart(22, "0")}`, name: `한글 멤버 ${String(index).padStart(4, "0")}`, aliases: { ko: [`별명 ${index}`], ja: [] }, isGraduated: false }));
const rooms = Array.from({ length: 100 }, (_, index) => String(firstID + BigInt(index)));
const alarms = Array.from({ length: 2000 }, (_, index) => ({ roomId: rooms[index % 100], roomName: `한글 방 ${index % 100}`, channelId: members[index % 1000].channelId, memberName: members[index % 1000].name }));
const streams = (count, status) => Array.from({ length: count }, (_, index) => ({ id: `fixture-video-${index}`, title: `한글 방송 ${index}`, status, channel_id: members[index].channelId, channel_name: members[index].name, thumbnail: null }));
const serialize = value => JSON.stringify(value, (_key, item) => typeof item === "bigint" ? JSON.rawJSON(String(item)) : item);
const bodies = new Map(Object.entries({
  "/health": { status: "ok", goroutines: 30 },
  "/api/holo/members": { status: "ok", members },
  "/api/holo/rooms": { status: "ok", rooms, aclEnabled: true, aclMode: "blacklist" },
  "/api/holo/alarms": { status: "ok", alarms },
  "/api/holo/settings": { status: "ok", settings: { alarmAdvanceMinutes: 15 } },
  "/api/holo/streams/live": { status: "ok", org: "hololive", streams: streams(50, "live") },
  "/api/holo/streams/upcoming": { status: "ok", org: "hololive", streams: streams(100, "upcoming") },
  "/api/holo/stats": { status: "ok", members: 1000, alarms: 2000, rooms: 100, version: "fixture", uptime: "1h" },
  "/api/holo/rooms/joined": { status: "ok", rooms: rooms.map((chatId, index) => ({ chatId, name: `한글 방 ${index}`, type: "MultiChat", memberCount: 10 })) },
  "/api/holo/members/calendar": { status: "ok", year: 2026, month: 9, entries: [] },
}).map(([route, value]) => [route, serialize(value)]));

export const dataFixtureHash = createHash("sha256").update(serialize([...bodies])).digest("hex");
/** attachDataFixture는 동결된 corpus와 10ms 지연을 소유한 fake Holo/Docker에 적용합니다. */
export function attachDataFixture(fixture) {
  fixture.dockerDelay = 10;
  fixture.holoReads = new Map();
  fixture.holoHandler = (req, res) => {
    const route = new URL(req.url, "http://fixture.invalid").pathname;
    const key = `${req.method} ${route}`;
    fixture.holoReads.set(key, (fixture.holoReads.get(key) ?? 0) + 1);
    const body = bodies.get(route);
    setTimeout(() => { res.statusCode = req.method === "GET" && body !== undefined ? 200 : 404; res.end(body ?? '{"error":"unmapped fixture route"}'); }, 10);
  };
}
