import assert from "node:assert/strict";
import { after, afterEach, before, test } from "node:test";
import { http, HttpResponse } from "msw";
import { adminClient } from "@/api/adminClient";
import { server } from "@/mocks/server";
import { settingsApi } from "./api";

const previousBaseURL = adminClient.instance.defaults.baseURL;
const baseURL = "http://localhost:30190";
before(() => { adminClient.instance.defaults.baseURL = baseURL; server.listen({ onUnhandledRequest: "error" }); });
afterEach(() => { server.resetHandlers(); });
after(() => { server.close(); adminClient.instance.defaults.baseURL = previousBaseURL; });

test("settings GET preserves the server range independently of the editor range", async () => {
 for (const minutes of [0, 1, 15, 60, 120, 1440]) {
  server.use(http.get("*/settings", () => HttpResponse.json({status:"ok",settings:{alarmAdvanceMinutes:minutes}})));
  assert.equal((await settingsApi.get()).settings.alarmAdvanceMinutes, minutes);
 }
});

test("settings GET rejects unknown or malformed values", async () => {
 for (const body of [{}, null, "<html>", {status:"ok",settings:null}, {status:"ok",settings:{alarmAdvanceMinutes:null}}, {status:"ok",settings:{alarmAdvanceMinutes:"15"}}, ...[-1, 1441, 1.5].map(alarmAdvanceMinutes => ({status:"ok",settings:{alarmAdvanceMinutes}}))]) {
  server.use(http.get("*/settings", () => HttpResponse.json(body)));
  await assert.rejects(settingsApi.get(), /조회 응답/);
 }
});

test("settings POST preserves partial runtime results and rejects unconfirmed bodies without replay", async () => {
 for (const runtime of [{alarm_applied:true,config_publish_alarm_advance_minutes:false}, {alarm_applied:false,config_publish_alarm_advance_minutes:true}, {}, null, []]) {
  let posts=0;
  server.use(http.post("*/settings", () => { posts++; return HttpResponse.json({status:"ok",settings:{alarmAdvanceMinutes:15},runtime}); }));
  if (runtime===null || Array.isArray(runtime)) await assert.rejects(settingsApi.update({alarmAdvanceMinutes:15}), /저장 응답/);
  else assert.deepEqual((await settingsApi.update({alarmAdvanceMinutes:15})).runtime,runtime);
  assert.equal(posts,1);
 }
});
