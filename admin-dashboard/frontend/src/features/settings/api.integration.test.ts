import { contractJSON } from "@/mocks/contract";
import assert from "node:assert/strict";
import { after, afterEach, before, test } from "node:test";
import { http } from "msw";
import { httpClient as apiClient, session, generation } from "@/app/bootstrap";
import { server } from "@/mocks/server";
import { settingsApi } from "./api";

function seedCSRF(token: string): void { generation.ready(); session.state.acceptCSRF(token, session.state.snapshot()); }

const previousBaseURL = apiClient.defaults.baseURL;
const baseURL = "http://localhost:30190";
const oldOnline = Object.getOwnPropertyDescriptor(navigator, "onLine");
before(() => {
	Object.defineProperty(navigator, "onLine", { value: true, configurable: true }); seedCSRF("fixture-csrf"); apiClient.defaults.baseURL = baseURL; server.listen({ onUnhandledRequest: "error" }); });
afterEach(() => { server.resetHandlers(); });
after(() => {
	if (oldOnline) Object.defineProperty(navigator, "onLine", oldOnline); else Reflect.deleteProperty(navigator, "onLine"); server.close(); apiClient.defaults.baseURL = previousBaseURL; });

test("settings GET preserves the server range independently of the editor range", async () => {
 for (const minutes of [0, 1, 15, 60, 120, 1440]) {
  server.use(http.get("*/settings", () => contractJSON({status:"ok",settings:{alarmAdvanceMinutes:minutes}})));
  assert.equal((await settingsApi.get()).settings.alarmAdvanceMinutes, minutes);
 }
});

test("settings GET rejects unknown or malformed values", async () => {
 for (const body of [{}, null, "<html>", {status:"ok",settings:null}, {status:"ok",settings:{alarmAdvanceMinutes:null}}, {status:"ok",settings:{alarmAdvanceMinutes:"15"}}, ...[-1, 1441, 1.5].map(alarmAdvanceMinutes => ({status:"ok",settings:{alarmAdvanceMinutes}}))]) {
  server.use(http.get("*/settings", () => contractJSON(body)));
  await assert.rejects(settingsApi.get(), /응답/);
 }
});

test("settings POST preserves partial runtime results and rejects unconfirmed bodies without replay", async () => {
 for (const runtime of [{alarm_applied:true,config_publish_alarm_advance_minutes:false}, {alarm_applied:false,config_publish_alarm_advance_minutes:true}, {}, null, []]) {
  let posts=0;
  server.use(http.post("*/settings", () => { posts++; return contractJSON({status:"ok",message:"설정 저장",settings:{alarmAdvanceMinutes:15},runtime}); }));
  if (runtime===null || Array.isArray(runtime)) await assert.rejects(settingsApi.update({alarmAdvanceMinutes:15}), /응답/);
  else assert.deepEqual((await settingsApi.update({alarmAdvanceMinutes:15})).runtime,runtime);
  assert.equal(posts,1);
 }
});
