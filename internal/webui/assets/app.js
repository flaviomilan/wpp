import {createPreferences} from "./preferences.mjs";
import {createTranslator} from "./i18n.mjs";
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
const translator=createTranslator(),t=translator.text;
const model={
 state: {view:"groups", chats:[], chat:"", people:[], offset:0, tableOffset:0, more:false, tableMore:false, version:0, tables:[]},chatTabs:{tracked:{query:"",type:"all",chat:"",messageQuery:"",person:"",offset:0,scroll:0},available:{query:"",type:"all",chat:"",messageQuery:"",person:"",offset:0,scroll:0}},get labels(){return {groups:[t("ui.chats"),t("ui.your_history_with_context_and_recognizable_people")],people:[t("ui.people"),t("ui.recognize_the_people_in_your_chats")],settings:[t("ui.settings"),t("ui.connection_transcription_and_local_archive_options")],activity:[t("ui.activity"),t("ui.monitor_archive_operations_results_and_outstanding_items")],audio:[t("ui.audio"),t("ui.monitor_processing_and_read_voice_message_transcripts")]};},get audioStates(){return {pending:t("ui.queued"),waiting_metadata:t("ui.waiting_for_metadata"),processing:t("ui.transcribing"),done:t("ui.completed_alternate"),failed:t("ui.failed"),cancelled:t("ui.cancelled_alternate")};},get audioPhases(){return {downloading:t("ui.downloading_audio"),converting:t("ui.preparing_audio"),transcribing:t("ui.recognizing_speech"),saving:t("ui.saving_text")};},
 chatTab:"tracked",readonlyRetryNotice:"",timezone:"America/Sao_Paulo",serviceState:null,displayedGeneration:-1
};
const ctx={document,window,navigator,model,requests:createRequests(),translator,t,...createDOM(document),api:createAPI(window.fetch.bind(window),t)};
ctx.groupPath=group=>"/api/groups/"+encodeURIComponent(group)+"/people";
ctx.number=value=>new Intl.NumberFormat(ctx.translator.language).format(value);
ctx.kindLabel=kind=>t(({texto:"kind.text",áudio:"kind.audio",imagem:"kind.image",vídeo:"kind.video",documento:"kind.document",figurinha:"kind.sticker",localização:"kind.location",contato:"kind.contact",reação:"kind.reaction",indisponível:"kind.unavailable"})[kind]||"kind.unsupported");
ctx.renderRoute=route=>{const from=route.from_self?t("person.self"):route.from;const to=route.group?t("route.group",{name:route.to}):route.to_self?(route.to?t("route.self_named",{name:route.to}):t("person.self")):route.to;return from+" → "+to;};
ctx.chatByID=id=>model.state.chats.find(c=>c.ID===id);
for(const create of [createPreferences,createChats,createPeople,createAudio,createInspector,createActivity,createSettings,createNavigation])Object.assign(ctx,create(ctx));
ctx.$("close-record").onclick=()=>ctx.$("record-dialog").close();
const onVisibility=()=>{if(!document.hidden)ctx.safely(ctx.poll);};
document.addEventListener("visibilitychange",onVisibility);
let interval,stopped=false;
const ready=ctx.safely(async()=>{
 await ctx.loadPreferences();
 model.timezone=(await ctx.api("/api/settings")).timezone;
 await ctx.loadChats();await ctx.loadService();
 if(model.state.chat)await ctx.selectChat(model.state.chat);
 ctx.restoreNavigation();await ctx.poll();if(!stopped)interval=window.setInterval(ctx.poll,2000);
});

return {ctx,ready,stop(){stopped=true;window.clearInterval(interval);document.removeEventListener("visibilitychange",onVisibility);ctx.requests.invalidate();ctx.stopNavigation();}};
}
if (typeof document!=="undefined" && typeof window!=="undefined") startApp(document,window);
