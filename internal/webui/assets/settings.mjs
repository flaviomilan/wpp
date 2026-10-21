export function createSettings(ctx) {
const document=ctx.document||globalThis.document;
let settingsDirty=false,settingsFilled=false;
async function control(path,method="POST",body){
 ctx.$("notice").classList.add("hidden");
 const result=await ctx.api(path,{method,headers:body===undefined?undefined:{"Content-Type":"application/json"},body:body===undefined?undefined:JSON.stringify(body)});
 await loadService();return result;
}

async function loadService(){
 const token=ctx.requests.begin("service");const snapshot=await ctx.api("/api/service");if(!token.current())return;ctx.model.serviceState=snapshot;
 const settings=snapshot.settings||{},connections={get connected(){return ctx.t("ui.connected");},get connecting(){return ctx.t("ui.connecting");},get disconnected(){return ctx.t("ui.disconnected_alternate");},get reconnecting(){return ctx.t("ui.reconnecting");},get needs_login(){return ctx.t("ui.connect_account");},get external(){return ctx.t("ui.external_capture");},get error(){return ctx.t("ui.connection_interrupted");}};
 ctx.bindText(ctx.$("connection-badge"),()=>connections[snapshot.connection]||ctx.t("ui.disconnected_alternate"));
 ctx.$("connection-badge").classList.toggle("neutral",snapshot.connection!=="connected");
 document.querySelectorAll("[data-control]").forEach(node=>node.disabled=!snapshot.owner);
 ctx.$("capture-toggle").checked=!!settings.capture_enabled;ctx.$("transcription-toggle").checked=!!settings.transcription_enabled;
 ctx.bindText(ctx.$("capture-caption"),()=>settings.capture_enabled?(snapshot.history_running?ctx.t("ui.retrieving_history"):snapshot.connection==="connected"?ctx.t("ui.receiving_messages"):ctx.t("ui.waiting_for_connection")):ctx.t("ui.paused"));
 ctx.bindText(ctx.$("transcription-caption"),()=>!settings.transcription_enabled?(snapshot.audio_source?ctx.t("ui.finishing_current_audio_before_pausing"):ctx.t("ui.paused")):snapshot.audio_error?ctx.t("ui.check_settings"):!snapshot.audio_available?ctx.t("ui.preparing_engine"):snapshot.audio_source?(ctx.model.audioPhases[snapshot.audio_phase]||ctx.t("ui.processing_audio")):settings.capture_enabled?ctx.t("ui.waiting_for_audio"):ctx.t("ui.waiting_for_a_sync"));
 ctx.bindText(ctx.$("account-status"),()=>snapshot.linked?ctx.t("ui.account_linked")+(connections[snapshot.connection]||ctx.t("ui.disconnected")):ctx.t("ui.link_your_account_to_get_started"));
 ctx.bindText(ctx.$("pair-account"),()=>snapshot.linked?ctx.t("ui.reconnect_whatsapp"):ctx.t("ui.connect_whatsapp"));
 ctx.bindText(ctx.$("engine-status"),()=>(snapshot.audio_error_code?ctx.t(snapshot.audio_error_code):snapshot.audio_error)||ctx.t("ui.local_whisper")+(settings.language||"pt")+" · "+(settings.model_path?.split("/").pop()||ctx.t("ui.model_not_configured")));
 ctx.bindText(ctx.$("audio-current"),()=>snapshot.audio_source?(ctx.model.audioPhases[snapshot.audio_phase]||ctx.t("ui.processing_audio"))+" · "+snapshot.audio_source:(!settings.transcription_enabled?ctx.t("ui.queue_paused"):(snapshot.audio_error_code?ctx.t(snapshot.audio_error_code):snapshot.audio_error)||ctx.t("ui.waiting_for_eligible_audio_and_connection")));
 if(snapshot.error||snapshot.export_error||ctx.model.readonlyRetryNotice){ctx.bindText(ctx.$("notice"),()=>[(snapshot.error_code?ctx.t(snapshot.error_code):snapshot.error)||(snapshot.export_error_code?ctx.t(snapshot.export_error_code):snapshot.export_error),ctx.model.readonlyRetryCode?ctx.t(ctx.model.readonlyRetryCode):ctx.model.readonlyRetryNotice].filter(Boolean).join(" "));ctx.$("notice").classList.remove("hidden");}
 const operations=snapshot.operations||[];ctx.renderActivity(operations);
 const pairing=operations.find(op=>op.kind==="pair"&&["queued","running"].includes(op.state));
 ctx.$("qr-area").classList.toggle("hidden",!pairing);
 if(pairing&&snapshot.qr_available){ctx.$("pair-qr").src="/api/session/qr?t="+Date.now();ctx.$("pair-qr").classList.remove("hidden");}else{ctx.$("pair-qr").classList.add("hidden");ctx.$("pair-qr").removeAttribute("src");}
 if(!settingsDirty&&(!settingsFilled||ctx.model.state.view==="settings")){
  for(const [id,key] of Object.entries({"setting-language":"language","setting-vocabulary":"vocabulary","setting-model":"model_path","setting-whisper":"whisper_path","setting-ffmpeg":"ffmpeg_path","setting-threads":"threads","setting-timeout":"timeout"})){const value=String(settings[key]??"");if(ctx.$(id).value!==value)ctx.$(id).value=value;}
  settingsFilled=true;
 }
 ctx.bindText(ctx.$("capture-summary"),()=>ctx.t("ui.capture")+ctx.$("capture-caption").textContent);
 ctx.bindText(ctx.$("transcription-summary"),()=>ctx.t("ui.transcription")+ctx.$("transcription-caption").textContent);
 ctx.$("owner-notice").classList.toggle("hidden",!!snapshot.owner);
 ctx.renderOnboarding(snapshot);
 ctx.updateChatControls();
 return snapshot;
}
ctx.$("discover").onclick=()=>ctx.safely(()=>control("/api/chats/discover"));
ctx.$("capture-toggle").onchange=()=>ctx.safely(()=>control("/api/service/capture","PUT",{enabled:ctx.$("capture-toggle").checked}));
ctx.$("transcription-toggle").onchange=()=>ctx.safely(()=>control("/api/service/transcription","PUT",{enabled:ctx.$("transcription-toggle").checked}));
ctx.$("pair-account").onclick=()=>ctx.safely(()=>control("/api/session/pair"));
ctx.$("cancel-pair").onclick=()=>ctx.safely(()=>{const op=ctx.model.serviceState?.operations.find(op=>op.kind==="pair"&&["queued","running"].includes(op.state));if(op)return control("/api/operations/"+encodeURIComponent(op.id)+"/cancel");});
ctx.$("export-all").onclick=()=>ctx.safely(()=>control("/api/exports","POST",{}));
function discardSettings(){ctx.discardLanguageDraft();settingsDirty=false;settingsFilled=false;ctx.bindText(ctx.$("settings-feedback"),()=>"");ctx.safely(loadService);}
ctx.$("settings-form").oninput=()=>{settingsDirty=true;ctx.bindText(ctx.$("settings-feedback"),()=>ctx.t("ui.unsaved_changes"));};
ctx.$("settings-form").onsubmit=event=>{event.preventDefault();return ctx.safely(async()=>{
 const settings={...ctx.model.serviceState.settings,language:ctx.$("setting-language").value.trim(),vocabulary:ctx.$("setting-vocabulary").value.trim(),model_path:ctx.$("setting-model").value.trim(),whisper_path:ctx.$("setting-whisper").value.trim(),ffmpeg_path:ctx.$("setting-ffmpeg").value.trim(),threads:Number(ctx.$("setting-threads").value),timeout:ctx.$("setting-timeout").value.trim()};
 ctx.bindText(ctx.$("settings-feedback"),()=>ctx.t("ui.saving"));
 try{await control("/api/service/settings","PUT",settings);settingsDirty=false;ctx.bindText(ctx.$("settings-feedback"),()=>ctx.t("ui.settings_saved_they_apply_to_the_next_audio_files"));}
 catch(error){ctx.bindText(ctx.$("settings-feedback"),()=>error.message);throw error;}
});};
return {control,loadService,settingsChanged:()=>settingsDirty||ctx.preferencesChanged(),discardSettings};
}
