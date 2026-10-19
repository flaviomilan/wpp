import {createAPI} from "./api.mjs";
import {createDOM} from "./dom.mjs";
import {createRequests} from "./requests.mjs";
import {createChats} from "./chats.mjs";
import {createPeople} from "./people.mjs";
import {createAudio} from "./audio.mjs";
import {createSettings} from "./settings.mjs";
import {createInspector} from "./inspector.mjs";
import {createActivity} from "./activity.mjs";
import {createNavigation} from "./navigation.mjs";

export function startApp(document, window, navigator=globalThis.navigator) {
const model={
 state: {view:"groups", chats:[], chat:"", people:[], offset:0, tableOffset:0, more:false, tableMore:false, version:0, tables:[]},chatTabs:{tracked:{query:"",type:"all",chat:"",messageQuery:"",person:"",offset:0,scroll:0},available:{query:"",type:"all",chat:"",messageQuery:"",person:"",offset:0,scroll:0}},labels: {groups:["Conversas","Seu histórico, com contexto e pessoas identificadas."],people:["Pessoas","Reconheça os autores das suas conversas."],settings:["Ajustes","Conexão, transcrição e opções do arquivo local."]},audioStates:{pending:"Na fila",waiting_metadata:"Aguardando metadados",processing:"Transcrevendo",done:"Concluído",failed:"Falhou",cancelled:"Cancelado"},audioPhases:{downloading:"Baixando áudio",converting:"Preparando áudio",transcribing:"Reconhecendo fala",saving:"Salvando texto"},
 chatTab:"tracked",readonlyRetryNotice:"",timezone:"America/Sao_Paulo",serviceState:null,displayedGeneration:-1
};
model.labels.activity=["Atividade","Acompanhe operações, resultados e pendências do arquivo."];
model.labels.audio=["Áudios","Acompanhe o processamento e as transcrições das mensagens de voz."];
const ctx={document,window,navigator,model,requests:createRequests(),...createDOM(document),api:createAPI(window.fetch.bind(window))};
ctx.groupPath=group=>"/api/groups/"+encodeURIComponent(group)+"/people";
ctx.chatByID=id=>model.state.chats.find(c=>c.ID===id);
for(const create of [createChats,createPeople,createAudio,createInspector,createActivity,createSettings,createNavigation])Object.assign(ctx,create(ctx));
ctx.$("close-record").onclick=()=>ctx.$("record-dialog").close();
const onVisibility=()=>{if(!document.hidden)ctx.safely(ctx.poll);};
document.addEventListener("visibilitychange",onVisibility);
let interval,stopped=false;
const ready=ctx.safely(async()=>{
 model.timezone=(await ctx.api("/api/settings")).timezone;
 await ctx.loadChats();await ctx.loadService();
 if(model.state.chat)await ctx.selectChat(model.state.chat);
 ctx.restoreNavigation();await ctx.poll();if(!stopped)interval=window.setInterval(ctx.poll,2000);
});

return {ctx,ready,stop(){stopped=true;window.clearInterval(interval);document.removeEventListener("visibilitychange",onVisibility);ctx.requests.invalidate();ctx.stopNavigation();}};
}
if (typeof document!=="undefined" && typeof window!=="undefined") startApp(document,window);
