export function createAudio(ctx) {
const document=ctx.document||globalThis.document;
const navigator=ctx.navigator||globalThis.navigator;
let audioOffset=0,audioRenderKey="",detailText="";
async function openMessageDetail(source,audio=false){
 const token=ctx.requests.begin("detail");
 const item=await ctx.api((audio?"/api/audio/":"/api/messages/")+encodeURIComponent(source));
 if(!token.current())return;const m=item.message;
 detailText=audio&&!m.Deleted?m.Transcript?.Text||"":"";
 ctx.bindText(ctx.$("detail-title"),()=>audio?ctx.t("ui.automatic_transcription"):ctx.t("ui.original_message"));
 ctx.bindText(ctx.$("detail-author"),()=>(item.author_code?ctx.t(item.author_code):item.author||ctx.t("ui.unknown_author"))+" · "+new Date(audio?item.time:m.Time).toLocaleString(ctx.translator.language,{timeZone:ctx.model.timezone})+(m.Edited?ctx.t("ui.edited_alternate"):""));
 ctx.$("detail-body").replaceChildren();
 ctx.$("detail-body").append(ctx.el("div",()=>m.Deleted?ctx.t("ui.deleted_message"):audio?(detailText||ctx.t("ui.transcript_not_available_yet")):(m.Text||"["+ctx.kindLabel(m.Kind)+"]"),"message-text"));
 if(!audio&&!m.Deleted&&m.FileName)ctx.$("detail-body").append(ctx.el("p",()=>ctx.t("ui.attachment")+m.FileName));
 if(!audio&&!m.Deleted&&m.Transcript)ctx.$("detail-body").append(ctx.el("small",()=>ctx.t("ui.automatic_transcription_alternate")+m.Transcript.Model),ctx.el("div",()=>m.Transcript.Text,"message-text"));
 if(audio&&!m.Deleted&&m.Transcript)ctx.$("detail-body").prepend(ctx.el("p",()=>ctx.t("ui.speech_recognition")+m.Transcript.Model,"muted"));
 ctx.bindText(ctx.$("detail-identifiers"),()=>ctx.t("ui.source")+item.source_id+"\nMensagem: "+m.ID+"\nConversa: "+m.Chat);
 ctx.bindText(ctx.$("copy-status"),()=>"");ctx.$("copy-transcript").classList.toggle("hidden",!detailText);
 if(!ctx.$("message-detail").open)ctx.$("message-detail").showModal();
}

async function loadAudio() {
 const token=ctx.requests.begin("audio");
 const query=new URLSearchParams({offset:audioOffset,limit:50,chat:ctx.$("audio-chat").value,state:ctx.$("audio-state").value});
 const page=await ctx.api("/api/audio?"+query);if(!token.current())return;
 const key=JSON.stringify([String(query),page]);if(key===audioRenderKey)return;audioRenderKey=key;
 const active=document.activeElement,focusSource=active?.dataset.source,focusAction=active?.dataset.action;
 ctx.bindText(ctx.$("audio-summary"),()=>Object.entries(page.summary).map(([key,count])=>(ctx.model.audioStates[key]||key)+": "+ctx.number(count)).join(" · ")||ctx.t("ui.no_audio_queued"));
 const existing=new Map(Array.from(ctx.$("audio-cards").children).map(n=>[n.dataset.source,n]));
 const cards=[];
 page.items.forEach(job=>{
  const signature=JSON.stringify([job,ctx.chatByID(job.chat)?.Name,!!ctx.model.serviceState?.owner]);
  let card=existing.get(job.source_id);
  if(card?.dataset.signature===signature){cards.push(card);return;}
  card=ctx.el("article",undefined,"audio-card");card.dataset.source=job.source_id;card.dataset.signature=signature;
  const heading=ctx.el("div",undefined,"audio-card-heading"),identity=ctx.el("div");
  identity.append(ctx.el("strong",()=>job.author_code?ctx.t(job.author_code):job.author||ctx.t("ui.unknown_author")),ctx.el("small",()=>(ctx.chatByID(job.chat)||{}).Name||job.chat));
  heading.append(identity,ctx.el("span",()=>job.state==="processing"?(ctx.model.audioPhases[job.phase]||ctx.t("ui.processing")):(ctx.model.audioStates[job.state]||job.state),"pill neutral"));card.append(heading);
  card.append(ctx.el("p",()=>new Date(job.time).toLocaleString(ctx.translator.language,{timeZone:ctx.model.timezone})+" · "+ctx.number(job.attempts)+(job.attempts===1?ctx.t("ui.attempt"):ctx.t("ui.attempts")),"muted"));
  if(job.has_transcript){
   card.append(ctx.el("small",()=>ctx.t("ui.automatic_transcription_preview"),"transcript-label"),ctx.el("div",()=>job.transcript_preview+(job.preview_truncated?"…":""),"transcript-preview"));
   const read=ctx.button(()=>ctx.t("ui.view_transcript"),()=>ctx.safely(()=>openMessageDetail(job.source_id,true)));read.dataset.source=job.source_id;read.dataset.action="read";card.append(read);
  }else if(job.error)card.append(ctx.el("p",()=>job.error_code?ctx.t(job.error_code):ctx.t("error.generic"),"audio-error"));
  if(job.retry_eligible){
   const retry=ctx.button(()=>ctx.t("ui.retry"),()=>ctx.safely(async()=>{
    retry.disabled=true;
    try {const result=await ctx.api("/api/audio/"+encodeURIComponent(job.source_id)+"/retry",{method:"POST"});if(result.status==="pending"&&result.detail){ctx.model.readonlyRetryNotice=result.detail;ctx.model.readonlyRetryCode=result.detail_code||"";}await ctx.loadService();await loadAudio();}finally{retry.disabled=!job.tracked;}
   }));
   retry.dataset.source=job.source_id;retry.dataset.action="retry";retry.disabled=!job.tracked;card.append(retry);
  }
  if(!job.has_transcript&&job.retry_reason)card.append(ctx.el("p",()=>job.retry_code?ctx.t(job.retry_code):job.retry_reason,"helper"));
  cards.push(card);
 });
 ctx.$("audio-cards").replaceChildren(...cards);
 if(!page.items.length)ctx.$("audio-cards").append(ctx.el("div",()=>ctx.t("ui.no_audio_matches_these_filters"),"empty-state"));
 if(focusSource){const target=Array.from(ctx.$("audio-cards").querySelectorAll("button")).find(n=>n.dataset.source===focusSource&&n.dataset.action===focusAction);if(target&&target!==document.activeElement)target.focus({preventScroll:true});}
 ctx.$("audio-prev").disabled=audioOffset===0;ctx.$("audio-next").disabled=!page.more;ctx.bindText(ctx.$("audio-page"),()=>ctx.t("ui.page")+ctx.number(audioOffset/50+1));
}
ctx.$("close-detail").onclick=()=>{ctx.requests.begin("detail");ctx.$("message-detail").close();detailText="";};
ctx.$("message-detail").addEventListener("close",()=>{ctx.requests.begin("detail");detailText="";});
ctx.$("copy-transcript").onclick=()=>ctx.safely(async()=>{try{await navigator.clipboard.writeText(detailText);ctx.bindText(ctx.$("copy-status"),()=>ctx.t("ui.transcript_copied"));}catch(error){ctx.bindText(ctx.$("copy-status"),()=>ctx.t("ui.select_the_transcript_text_and_copy_it_using_your_keyboard"));}});
ctx.$("audio-prev").onclick=()=>{audioOffset=Math.max(0,audioOffset-50);ctx.safely(loadAudio);};
ctx.$("audio-next").onclick=()=>{audioOffset+=50;ctx.safely(loadAudio);};
for(const id of ["audio-chat","audio-state"])ctx.$(id).onchange=()=>{audioOffset=0;ctx.safely(loadAudio);};
return {openMessageDetail,loadAudio};
}
