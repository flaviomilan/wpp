export function createAudio(ctx) {
const document=ctx.document||globalThis.document;
const navigator=ctx.navigator||globalThis.navigator;
let audioOffset=0,audioRenderKey="",detailText="";
async function openMessageDetail(source,audio=false){
 const token=ctx.requests.begin("detail");
 const item=await ctx.api((audio?"/api/audio/":"/api/messages/")+encodeURIComponent(source));
 if(!token.current())return;const m=item.message;
 detailText=audio&&!m.Deleted?m.Transcript?.Text||"":"";
 ctx.$("detail-title").textContent=audio?"Transcrição automática":"Mensagem original";
 ctx.$("detail-author").textContent=(item.author||"Autor desconhecido")+" · "+new Date(audio?item.time:m.Time).toLocaleString("pt-BR",{timeZone:ctx.model.timezone})+(m.Edited?" · editada":"");
 ctx.$("detail-body").replaceChildren();
 ctx.$("detail-body").append(ctx.el("div",m.Deleted?"Mensagem excluída":audio?(detailText||"Transcrição ainda não disponível."):(m.Text||"["+m.Kind+"]"),"message-text"));
 if(!audio&&!m.Deleted&&m.FileName)ctx.$("detail-body").append(ctx.el("p","Anexo: "+m.FileName));
 if(!audio&&!m.Deleted&&m.Transcript)ctx.$("detail-body").append(ctx.el("small","Transcrição automática · "+m.Transcript.Model),ctx.el("div",m.Transcript.Text,"message-text"));
 if(audio&&!m.Deleted&&m.Transcript)ctx.$("detail-body").prepend(ctx.el("p","Reconhecimento de fala · "+m.Transcript.Model,"muted"));
 ctx.$("detail-identifiers").textContent="Fonte: "+item.source_id+"\nMensagem: "+m.ID+"\nConversa: "+m.Chat;
 ctx.$("copy-status").textContent="";ctx.$("copy-transcript").classList.toggle("hidden",!detailText);
 if(!ctx.$("message-detail").open)ctx.$("message-detail").showModal();
}

async function loadAudio() {
 const token=ctx.requests.begin("audio");
 const query=new URLSearchParams({offset:audioOffset,limit:50,chat:ctx.$("audio-chat").value,state:ctx.$("audio-state").value});
 const page=await ctx.api("/api/audio?"+query);if(!token.current())return;
 const key=JSON.stringify([String(query),page]);if(key===audioRenderKey)return;audioRenderKey=key;
 const active=document.activeElement,focusSource=active?.dataset.source,focusAction=active?.dataset.action;
 ctx.$("audio-summary").textContent=Object.entries(page.summary).map(([key,count])=>(ctx.model.audioStates[key]||key)+": "+count).join(" · ")||"Nenhum áudio enfileirado";
 const existing=new Map(Array.from(ctx.$("audio-cards").children).map(n=>[n.dataset.source,n]));
 const cards=[];
 page.items.forEach(job=>{
  const signature=JSON.stringify([job,ctx.chatByID(job.chat)?.Name,!!ctx.model.serviceState?.owner]);
  let card=existing.get(job.source_id);
  if(card?.dataset.signature===signature){cards.push(card);return;}
  card=ctx.el("article",undefined,"audio-card");card.dataset.source=job.source_id;card.dataset.signature=signature;
  const heading=ctx.el("div",undefined,"audio-card-heading"),identity=ctx.el("div");
  identity.append(ctx.el("strong",job.author||"Autor desconhecido"),ctx.el("small",(ctx.chatByID(job.chat)||{}).Name||job.chat));
  heading.append(identity,ctx.el("span",job.state==="processing"?(ctx.model.audioPhases[job.phase]||"Em processamento"):(ctx.model.audioStates[job.state]||job.state),"pill neutral"));card.append(heading);
  card.append(ctx.el("p",new Date(job.time).toLocaleString("pt-BR",{timeZone:ctx.model.timezone})+" · "+job.attempts+(job.attempts===1?" tentativa":" tentativas"),"muted"));
  if(job.has_transcript){
   card.append(ctx.el("small","Transcrição automática · Prévia","transcript-label"),ctx.el("div",job.transcript_preview+(job.preview_truncated?"…":""),"transcript-preview"));
   const read=ctx.button("Ver transcrição",()=>ctx.safely(()=>openMessageDetail(job.source_id,true)));read.dataset.source=job.source_id;read.dataset.action="read";card.append(read);
  }else if(job.error)card.append(ctx.el("p",job.error,"audio-error"));
  if(job.retry_eligible){
   const retry=ctx.button("Tentar novamente",()=>ctx.safely(async()=>{
    retry.disabled=true;
    try {const result=await ctx.api("/api/audio/"+encodeURIComponent(job.source_id)+"/retry",{method:"POST"});if(result.status==="pending"&&result.detail){ctx.model.readonlyRetryNotice=result.detail;}await ctx.loadService();await loadAudio();}finally{retry.disabled=!job.tracked;}
   }));
   retry.dataset.source=job.source_id;retry.dataset.action="retry";retry.disabled=!job.tracked;card.append(retry);
  }
  if(!job.has_transcript&&job.retry_reason)card.append(ctx.el("p",job.retry_reason,"helper"));
  cards.push(card);
 });
 ctx.$("audio-cards").replaceChildren(...cards);
 if(!page.items.length)ctx.$("audio-cards").append(ctx.el("div","Nenhum áudio com estes filtros.","empty-state"));
 if(focusSource){const target=Array.from(ctx.$("audio-cards").querySelectorAll("button")).find(n=>n.dataset.source===focusSource&&n.dataset.action===focusAction);if(target&&target!==document.activeElement)target.focus({preventScroll:true});}
 ctx.$("audio-prev").disabled=audioOffset===0;ctx.$("audio-next").disabled=!page.more;ctx.$("audio-page").textContent="Página "+(audioOffset/50+1);
}
ctx.$("close-detail").onclick=()=>{ctx.requests.begin("detail");ctx.$("message-detail").close();detailText="";};
ctx.$("message-detail").addEventListener("close",()=>{ctx.requests.begin("detail");detailText="";});
ctx.$("copy-transcript").onclick=()=>ctx.safely(async()=>{try{await navigator.clipboard.writeText(detailText);ctx.$("copy-status").textContent="Transcrição copiada.";}catch(error){ctx.$("copy-status").textContent="Selecione o texto da transcrição e copie com o teclado.";}});
ctx.$("audio-prev").onclick=()=>{audioOffset=Math.max(0,audioOffset-50);ctx.safely(loadAudio);};
ctx.$("audio-next").onclick=()=>{audioOffset+=50;ctx.safely(loadAudio);};
for(const id of ["audio-chat","audio-state"])ctx.$(id).onchange=()=>{audioOffset=0;ctx.safely(loadAudio);};
return {openMessageDetail,loadAudio};
}
