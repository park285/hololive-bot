import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { createServer } from "vite";

const run = promisify(execFile);
const root = fileURLToPath(new URL("../../../", import.meta.url));
const entry = String.raw`
import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SettingsForm } from "/src/components/settings/SettingsForm.tsx";
import { adminClient } from "/src/api/adminClient.ts";
import { queryKeys } from "/src/api/queryKeys.ts";
import toast, { getToastItems } from "/src/lib/toast-api.ts";
adminClient.instance.defaults.baseURL = location.origin;
const client = new QueryClient({defaultOptions:{queries:{retry:false,refetchOnWindowFocus:false},mutations:{retry:2,retryDelay:0}}});
const root = createRoot(document.getElementById("root"));
const wait = async (fn, label) => { for(let i=0;i<300;i++){if(fn())return;await new Promise(r=>setTimeout(r,10));}throw new Error("timeout: "+label); };
const expect = (value, label) => { if(!value)throw new Error(label); };
const control = action => fetch("/__settings_control/"+action,{method:"POST"});
const input = () => document.querySelector("input");
const button = () => document.querySelector('button[type="submit"]');
const edit = value => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(input(),value); input().dispatchEvent(new Event("input",{bubbles:true})); };
const refetch = () => client.invalidateQueries({queryKey:queryKeys.settings.all});
(async()=>{
 try {
  root.render(React.createElement(QueryClientProvider,{client},React.createElement(SettingsForm)));
  await wait(()=>input(),"initial pending");
  expect(input().value==="" && input().disabled && button().disabled,"unknown value editable");
  expect(!document.body.textContent.includes("저장됨"),"unknown claimed saved");
  await control("fail-get");
  await wait(()=>document.body.textContent.includes("설정을 조회하지 못했습니다"),"initial error");
  expect(input().value==="" && button().disabled,"failed read displayed fallback");
  await control("malformed-get"); await refetch();
  await wait(()=>document.body.textContent.includes("설정을 조회하지 못했습니다"),"malformed initial query error");
  expect(input().value==="" && button().disabled && document.body.textContent.includes("설정을 조회하지 못했습니다"),"malformed initial response accepted");
  await control("ok-get"); await refetch();
  await wait(()=>input().value==="15","loaded baseline");
  await control("malformed-get"); await refetch();
  await wait(()=>document.body.textContent.includes("마지막으로 확인한 값"),"stale error");
  expect(input().value==="15","stale baseline lost");
  edit("20"); await wait(()=>!button().disabled,"dirty edit");
  await control("baseline-25"); await refetch();
  expect(input().value==="20","refetch overwrote edit");
  for (const [mode,value,fragment] of [["publish-failed","21","전파하지 못했습니다"],["apply-failed","22","적용에 실패했습니다"],["confirmed","23","적용·전파했습니다"],["save-failed","24","저장 결과를 확인하지 못했습니다"],["bad-request","25","거절되어 변경되지 않았습니다"],["forbidden","26","거절되어 변경되지 않았습니다"]]) {
   toast.dismiss(); await control(mode); edit(value); await wait(()=>!button().disabled,"edit "+mode); button().click();
   await wait(()=>getToastItems().some(t=>String(t.message).includes(fragment)),"result "+mode);
   await wait(()=>!input().disabled,"settled "+mode);
   expect(getToastItems().length===1,"mutation retried "+mode);
   if(["publish-failed","apply-failed","confirmed"].includes(mode)) {
    await wait(()=>document.body.textContent.includes("마지막으로 확인한 값"),"failed refetch after save");
    expect(input().value===value && button().disabled,"confirmed saved value lost after read failure");
   }
  }
  document.documentElement.dataset.testStatus="passed";
 } catch(error) {document.documentElement.dataset.testStatus="failed";document.getElementById("result").textContent=error?.stack??String(error);}
 finally {
  root.unmount();client.clear();toast.dismiss();
  await fetch("/__settings_result",{method:"POST",body:document.documentElement.outerHTML});
 }
})();
`;

test("settings preserve unknown, stale, edited, saved and partially applied results", {timeout:150000}, async()=>{
 const profile=await mkdtemp(path.join(tmpdir(),"settings-contract-browser-"));
 const state={get:"hold",value:15,mode:"confirmed",posts:0};
 const completion=Promise.withResolvers();
 const unit=`iris-settings-browser-${process.pid}-${Date.now()}`;
 let server;
 let completionTimeout;
 try {
  server=await createServer({root,configFile:path.join(root,"vite.config.ts"),cacheDir:path.join(profile,"vite-cache"),logLevel:"error",server:{host:"127.0.0.1",port:0},plugins:[{
   name:"settings-contract",
   configureServer(server){server.middlewares.use(async(req,res,next)=>{
    const pathname=new URL(req.url,"http://localhost").pathname;
    if(pathname==="/__settings_result" && req.method==="POST") {
     let body="";for await(const chunk of req)body+=chunk;
     res.end("ok");completion.resolve(body);return;
    }
    if(pathname==="/__settings_test") {res.setHeader("Content-Type","text/html");res.end(await server.transformIndexHtml(pathname,'<div id="root"></div><pre id="result"></pre><script type="module" src="/__settings_entry.jsx"></script>'));return;}
    if(pathname.startsWith("/__settings_control/")) {
     const action=pathname.split("/").at(-1);
     if(action==="fail-get") {state.get="failed";state.release?.();state.release=null;}
     else if(action==="ok-get")state.get="ok";
     else if(action==="malformed-get")state.get="malformed";
     else if(action==="baseline-25"){state.get="ok";state.value=25;}
     else state.mode=action;
     res.end("ok");return;
    }
    if(!pathname.endsWith("/settings"))return next();
    res.setHeader("Content-Type","application/json");
    if(req.method==="GET") {
     if(state.get==="malformed"){res.end(JSON.stringify({status:"ok",settings:null}));return;}
     const respond=()=>{if(state.get==="failed"){res.statusCode=503;res.end(JSON.stringify({error:"unavailable"}));}else res.end(JSON.stringify({status:"ok",settings:{alarmAdvanceMinutes:state.value}}));};
     if(state.get==="hold")state.release=respond;else respond();return;
    }
    state.posts++;
    let body="";for await (const chunk of req)body+=chunk;
    if(state.mode==="save-failed"){res.statusCode=500;res.end(JSON.stringify({error:"save unavailable"}));return;}
    if(state.mode==="bad-request" || state.mode==="forbidden"){res.statusCode=state.mode==="bad-request"?400:403;res.end(JSON.stringify({error:"refused"}));return;}
    state.value=JSON.parse(body).alarmAdvanceMinutes;state.get="failed";
    res.end(JSON.stringify({status:"ok",message:"Settings updated",settings:{alarmAdvanceMinutes:state.value},runtime:{alarm_applied:state.mode!=="apply-failed",config_publish_alarm_advance_minutes:state.mode!=="publish-failed"}}));
   });},
   resolveId(id){if(id==="/__settings_entry.jsx")return "\0settings-contract.jsx";},
   load(id){if(id==="\0settings-contract.jsx")return entry;},
  }]});
  await server.listen();
  const address=server.httpServer.address();
  // 가상 시간 DOM 덤프는 Vite 모듈 로드 전에 끝날 수 있어 실제 완료 보고를 기다린다.
  await run("systemd-run",["--user","--quiet","--collect",`--unit=${unit}`,"--property=RuntimeMaxSec=120","--property=KillMode=control-group","--property=TimeoutStopSec=5",process.env.CHROME_BIN??"/usr/bin/google-chrome","--headless=new","--no-sandbox","--disable-gpu","--disable-dev-shm-usage",`--user-data-dir=${profile}`,`http://127.0.0.1:${address.port}/__settings_test`],{timeout:10000});
  completionTimeout=setTimeout(()=>completion.reject(new Error("browser contract did not report completion")),110000);
  assert.match(await completion.promise,/data-test-status="passed"/);
  assert.equal(state.posts,6,"settings mutations must not inherit automatic retry");
 }finally{
  clearTimeout(completionTimeout);
  state.release?.();
  await run("systemctl",["--user","stop",unit],{timeout:10000}).catch(()=>{});
  await server?.close();await rm(profile,{recursive:true,force:true});
 }
});
