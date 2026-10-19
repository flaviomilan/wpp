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
 const settings=snapshot.settings||{},connections={connected:"Conectado",connecting:"Conectando…",disconnected:"Desconectado",reconnecting:"Reconectando…",needs_login:"Conectar conta",external:"Captura externa",error:"Conexão interrompida"};
 ctx.$("connection-badge").textContent=connections[snapshot.connection]||"Desconectado";
 ctx.$("connection-badge").classList.toggle("neutral",snapshot.connection!=="connected");
 document.querySelectorAll("[data-control]").forEach(node=>node.disabled=!snapshot.owner);
 ctx.$("capture-toggle").checked=!!settings.capture_enabled;ctx.$("transcription-toggle").checked=!!settings.transcription_enabled;
 ctx.$("capture-caption").textContent=settings.capture_enabled?(snapshot.history_running?"Recuperando histórico":snapshot.connection==="connected"?"Recebendo mensagens":"Aguardando conexão"):"Pausada";
 ctx.$("transcription-caption").textContent=!settings.transcription_enabled?(snapshot.audio_source?"Concluindo áudio antes de pausar":"Pausada"):snapshot.audio_error?"Confira os ajustes":!snapshot.audio_available?"Preparando motor…":snapshot.audio_source?(ctx.model.audioPhases[snapshot.audio_phase]||"Processando áudio"):settings.capture_enabled?"Aguardando áudios":"Aguardando uma sincronização";
 ctx.$("account-status").textContent=snapshot.linked?"Conta vinculada · "+(connections[snapshot.connection]||"Desconectada"):"Vincule sua conta para começar.";
 ctx.$("pair-account").textContent=snapshot.linked?"Reconectar WhatsApp":"Conectar WhatsApp";
 ctx.$("engine-status").textContent=snapshot.audio_error||"Whisper local · "+(settings.language||"pt")+" · "+(settings.model_path?.split("/").pop()||"modelo não configurado");
 ctx.$("audio-current").textContent=snapshot.audio_source?(ctx.model.audioPhases[snapshot.audio_phase]||"Processando áudio")+" · "+snapshot.audio_source:(!settings.transcription_enabled?"Fila pausada.":snapshot.audio_error||"Aguardando áudios elegíveis e conexão.");
 if(snapshot.error||snapshot.export_error||ctx.model.readonlyRetryNotice){ctx.$("notice").textContent=[snapshot.error||snapshot.export_error,ctx.model.readonlyRetryNotice].filter(Boolean).join(" ");ctx.$("notice").classList.remove("hidden");}
 const operations=snapshot.operations||[];ctx.renderActivity(operations);
 const pairing=operations.find(op=>op.kind==="pair"&&["queued","running"].includes(op.state));
 ctx.$("qr-area").classList.toggle("hidden",!pairing);
 if(pairing&&snapshot.qr_available){ctx.$("pair-qr").src="/api/session/qr?t="+Date.now();ctx.$("pair-qr").classList.remove("hidden");}else{ctx.$("pair-qr").classList.add("hidden");ctx.$("pair-qr").removeAttribute("src");}
 if(!settingsDirty&&(!settingsFilled||ctx.model.state.view==="settings")){
  for(const [id,key] of Object.entries({"setting-language":"language","setting-vocabulary":"vocabulary","setting-model":"model_path","setting-whisper":"whisper_path","setting-ffmpeg":"ffmpeg_path","setting-threads":"threads","setting-timeout":"timeout"})){const value=String(settings[key]??"");if(ctx.$(id).value!==value)ctx.$(id).value=value;}
  settingsFilled=true;
 }
 ctx.$("capture-summary").textContent="Captura · "+ctx.$("capture-caption").textContent;
 ctx.$("transcription-summary").textContent="Transcrição · "+ctx.$("transcription-caption").textContent;
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
function discardSettings(){settingsDirty=false;settingsFilled=false;ctx.$("settings-feedback").textContent="";ctx.safely(loadService);}
ctx.$("settings-form").oninput=()=>{settingsDirty=true;ctx.$("settings-feedback").textContent="Alterações não salvas.";};
ctx.$("settings-form").onsubmit=event=>{event.preventDefault();return ctx.safely(async()=>{
 const settings={...ctx.model.serviceState.settings,language:ctx.$("setting-language").value.trim(),vocabulary:ctx.$("setting-vocabulary").value.trim(),model_path:ctx.$("setting-model").value.trim(),whisper_path:ctx.$("setting-whisper").value.trim(),ffmpeg_path:ctx.$("setting-ffmpeg").value.trim(),threads:Number(ctx.$("setting-threads").value),timeout:ctx.$("setting-timeout").value.trim()};
 ctx.$("settings-feedback").textContent="Salvando…";
 try{await control("/api/service/settings","PUT",settings);settingsDirty=false;ctx.$("settings-feedback").textContent="Ajustes salvos. Valem para os próximos áudios.";}
 catch(error){ctx.$("settings-feedback").textContent=error.message;throw error;}
});};
return {control,loadService,settingsChanged:()=>settingsDirty,discardSettings};
}
