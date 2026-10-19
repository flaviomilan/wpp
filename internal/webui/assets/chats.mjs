export function createChats(ctx) {
 const document=ctx.document||globalThis.document;
let chatRenderKey="";
async function loadChats() {
 const token=ctx.requests.begin("chats");const chats=await ctx.api("/api/chats");if(!token.current())return;ctx.model.state.chats=chats||[];
 ctx.$("group-count").textContent=ctx.model.state.chats.filter(c=>c.ID.endsWith("@g.us")).length;
 ctx.$("message-count").textContent=ctx.model.state.chats.reduce((sum,c)=>sum+c.Count,0);
 ctx.$("tracked-count").textContent=ctx.model.state.chats.filter(c=>c.Tracked).length;
 const selected=ctx.$("people-group").value;
 ctx.options(ctx.$("people-group"),ctx.model.state.chats.filter(c=>c.ID.endsWith("@g.us")).map(c=>({value:c.ID,label:(c.Name||c.ID)+" · "+c.ID})));
 if(ctx.model.state.chats.some(c=>c.ID===selected))ctx.$("people-group").value=selected;
 if(!ctx.model.state.chat)ctx.model.state.chat=visibleChats()[0]?.ID||"";
 if(!ctx.model.state.chat)clearConversation();
 renderChats();
}

function visibleChats(){
 const query=ctx.$("chat-search").value.toLocaleLowerCase("pt-BR"),type=ctx.$("chat-filter").value;
 return ctx.model.state.chats.filter(c=>c.Tracked===(ctx.model.chatTab==="tracked")&&((c.Name||"")+" "+c.ID).toLocaleLowerCase("pt-BR").includes(query)&&(type==="all"||(type==="groups"&&c.ID.endsWith("@g.us"))||(type==="contacts"&&!c.ID.endsWith("@g.us"))));
}

function clearConversation(){
 ctx.model.state.chat="";ctx.model.state.offset=0;ctx.model.state.version++;ctx.model.displayedGeneration=-1;
 ctx.$("conversation-title").textContent="Selecione uma conversa";ctx.$("conversation-id").textContent="";
 ctx.$("messages").replaceChildren(ctx.el("div",ctx.model.chatTab==="tracked"?"Escolha uma conversa acompanhada ou abra Disponíveis para começar.":"Escolha uma conversa para consultar e acompanhar.","empty-state"));
 ctx.$("message-page").textContent="—";ctx.$("messages-prev").disabled=true;ctx.$("messages-next").disabled=true;
 ctx.$("open-people").classList.add("hidden");ctx.$("new-messages").classList.add("hidden");updateChatControls();
}

async function switchChatTab(next){
 ctx.requests.invalidate();const token=ctx.requests.begin("navigation");
 const previous=ctx.model.chatTabs[ctx.model.chatTab];
 Object.assign(previous,{query:ctx.$("chat-search").value,type:ctx.$("chat-filter").value,chat:ctx.model.state.chat,messageQuery:ctx.$("message-search").value,person:ctx.$("person-filter").value,offset:ctx.model.state.offset,scroll:ctx.$("messages").scrollTop});
 ctx.model.chatTab=next;
 for(const name of ["tracked","available"]){ctx.$("tab-"+name).setAttribute("aria-selected",String(name===next));ctx.$("tab-"+name).tabIndex=name===next?0:-1;}
 ctx.$("chat-list").setAttribute("aria-labelledby","tab-"+next);
 const saved={...ctx.model.chatTabs[next]};ctx.$("chat-search").value=saved.query;ctx.$("chat-filter").value=saved.type;
 const chat=visibleChats().find(c=>c.ID===saved.chat)||visibleChats()[0];
 if(!chat){clearConversation();renderChats();return;}
 await selectChat(chat.ID,token,chat.ID===saved.chat?saved:null);
}

function renderChats() {
 const chats=visibleChats();
 const key=JSON.stringify([ctx.model.chatTab,ctx.model.state.chat,chats.map(c=>[c.ID,c.Name,c.Count,c.Tracked])]);
 if(chatRenderKey===key){updateChatControls();return;}chatRenderKey=key;
 const focused=document.activeElement?.dataset.chat,scroll=ctx.$("chat-list").scrollTop;
 ctx.$("chat-count").textContent=chats.length;ctx.$("chat-list").replaceChildren();
 chats.forEach(chat=>{
  const node=ctx.el("button",undefined,"chat-item"+(ctx.model.state.chat===chat.ID?" selected":""));node.type="button";
  node.dataset.chat=chat.ID;
  node.append(ctx.el("span",chat.ID.endsWith("@g.us")?"GR":"CT","avatar"));const info=ctx.el("span",undefined,"chat-info");
  info.append(ctx.el("strong",chat.Name||"Sem nome"),ctx.el("small",chat.Count+" mensagens · "+(chat.Tracked?"acompanhada":"disponível")));node.append(info);
  node.onclick=()=>ctx.safely(()=>selectChat(chat.ID));ctx.$("chat-list").append(node);
  if(focused===chat.ID)node.focus({preventScroll:true});
 });
 if(!chats.length){
 const empty=ctx.el("div",ctx.model.chatTab==="tracked"?"Nenhuma conversa acompanhada com estes filtros.":"Nenhuma conversa disponível com estes filtros.","empty-state");
 if(ctx.model.chatTab==="tracked")empty.append(ctx.button("Explorar disponíveis",()=>ctx.safely(()=>switchChatTab("available"))));
 ctx.$("chat-list").append(empty);
 }
 ctx.$("chat-list").scrollTop=scroll;
 updateChatControls();
}

async function selectChat(id,transition=null,saved=null) {
 ctx.$("workspace").classList.add("reading");
 if(!transition)ctx.requests.invalidate();const token=transition||ctx.requests.begin("selection");
 ctx.model.state.chat=id;ctx.model.chatTabs[ctx.model.chatTab].chat=id;ctx.model.state.offset=saved?.offset||0;ctx.model.state.people=[];ctx.model.state.version++;renderChats();
 ctx.$("message-search").value=saved?.messageQuery||"";ctx.options(ctx.$("person-filter"),[],"Todos os remetentes");
 const chat=ctx.chatByID(id);
 if(chat&&chat.ID.endsWith("@g.us")) {
  const people=(await ctx.api(ctx.groupPath(id))).people||[];
  if(!token.current()||ctx.model.state.chat!==id)return;ctx.model.state.people=people;
  ctx.options(ctx.$("person-filter"),people.map(p=>({value:p.id,label:p.name||p.detected_name||p.id})),"Todos os remetentes");
 }
 if(saved)ctx.$("person-filter").value=saved.person;
 if(ctx.model.state.view==="groups")await loadMessages();
 if(token.current()&&saved)ctx.$("messages").scrollTop=saved.scroll;
}

async function loadMessages() {
 const chat=ctx.chatByID(ctx.model.state.chat);if(!chat)return;
 const token=ctx.requests.begin("messages");
 ctx.$("conversation-title").textContent=chat.Name||"Conversa sem nome";ctx.$("conversation-id").textContent=chat.ID;
 ctx.$("open-people").classList.toggle("hidden",!chat.ID.endsWith("@g.us"));
 const query=new URLSearchParams({offset:ctx.model.state.offset,limit:50,q:ctx.$("message-search").value,person:ctx.$("person-filter").value});
 const page=await ctx.api("/api/chats/"+encodeURIComponent(chat.ID)+"/messages?"+query);
 if(!token.current())return;
 ctx.model.displayedGeneration=chat.Generation;ctx.$("new-messages").classList.add("hidden");
 ctx.model.state.more=page.more;ctx.$("messages").replaceChildren();
 page.items.forEach(item=>{
  const m=item.message,node=ctx.el("article",undefined,"message"),heading=ctx.el("div",undefined,"message-heading");
  const date=ctx.el("time",new Date(m.Time).toLocaleString("pt-BR",{timeZone:ctx.model.timezone}));date.dateTime=m.Time;
  heading.append(ctx.el("strong",item.route),date);node.append(heading);
  if(m.ReplyID||m.Kind==="reação"){
 const reply=item.reply;
 if(reply?.state==="available"){
 const quote=ctx.button("",()=>ctx.safely(()=>ctx.openMessageDetail(reply.source_id)));
 quote.className="quoted-message";quote.setAttribute("aria-label","Ver mensagem original de "+reply.author);
 if(m.Kind==="reação")quote.append(ctx.el("span","Reagiu à mensagem","helper"));
 quote.append(ctx.el("strong",reply.author+(reply.edited?" · editada":"")),ctx.el("span",reply.preview+(reply.truncated?"…":"")));
 node.append(quote);
 }else node.append(ctx.el("div",m.Kind==="reação"?"Mensagem reagida indisponível no arquivo":"Mensagem citada indisponível no arquivo","reply missing-quote"));
 }
  let text=m.Deleted?"[Mensagem excluída]":m.Text||"["+m.Kind+"]";
  if(m.Caption)text+="\n"+m.Caption;if(m.FileName)text+="\nAnexo: "+m.FileName;
  node.append(ctx.el("div",text,"message-text"));
  if(!m.Deleted&&m.Transcript){node.append(ctx.el("div","Transcrição automática · "+m.Transcript.Model,"reply"),ctx.el("div",m.Transcript.Text,"message-text"));}
  else if(!m.Deleted&&m.Kind==="áudio")node.append(ctx.el("div","Transcrição: "+(ctx.model.audioStates[item.audio_state]||"não disponível"),"reply"));
  const meta=ctx.el("div",undefined,"message-meta");if(m.Edited)meta.append(ctx.el("span","Editada"));
  if(item.preview_truncated)meta.append(ctx.el("span","Prévia limitada. Consulte a evidência completa."));
  if(chat.ID.endsWith("@g.us")&&m.Sender)meta.append(ctx.button("Identificar pessoa",()=>ctx.openPerson(chat.ID,m.Sender)));
  meta.append(ctx.button("Ver registro",()=>ctx.record({...item,evidence_command:chat.ID.endsWith("@g.us")?"wpp evidence "+chat.ID+" "+item.source_id:undefined})));
  node.append(meta);ctx.$("messages").append(node);
 });
 if(!page.items.length)ctx.$("messages").append(ctx.el("div","Nenhuma mensagem para estes filtros.","empty-state"));
 ctx.$("messages-prev").disabled=ctx.model.state.offset===0;ctx.$("messages-next").disabled=!page.more;
 ctx.$("message-page").textContent="Página "+(ctx.model.state.offset/50+1)+" · mais recentes primeiro";
}

function updateChatControls(){
 const chat=ctx.chatByID(ctx.model.state.chat),owner=!!ctx.model.serviceState?.owner;
 ctx.$("track-chat").disabled=!owner||!chat;ctx.$("track-chat").textContent=chat?.Tracked?"Parar acompanhamento":"Acompanhar";
 const syncing=ctx.model.serviceState?.operations?.some(op=>op.kind==="sync"&&op.chat===ctx.model.state.chat&&["queued","running"].includes(op.state));ctx.$("sync-chat").disabled=!owner||!chat?.Tracked||syncing;ctx.$("sync-chat").textContent=syncing?"Sincronizando…":"Sincronizar agora";ctx.$("export-chat").disabled=!owner||!chat;
}
ctx.$("tab-tracked").onclick=()=>ctx.safely(()=>switchChatTab("tracked"));
ctx.$("tab-available").onclick=()=>ctx.safely(()=>switchChatTab("available"));
for(const name of ["tracked","available"])ctx.$("tab-"+name).onkeydown=event=>{if(["ArrowLeft","ArrowRight","Home","End"].includes(event.key)){event.preventDefault();const next=event.key==="Home"?"tracked":event.key==="End"?"available":ctx.model.chatTab==="tracked"?"available":"tracked";ctx.$("tab-"+next).focus();ctx.safely(()=>switchChatTab(next));}};
ctx.$("message-search-form").onsubmit=event=>{event.preventDefault();ctx.model.state.offset=0;ctx.safely(loadMessages);};
ctx.$("person-filter").onchange=()=>{ctx.model.state.offset=0;ctx.safely(loadMessages);};
ctx.$("messages-prev").onclick=()=>{ctx.model.state.offset=Math.max(0,ctx.model.state.offset-50);ctx.safely(loadMessages);};
ctx.$("messages-next").onclick=()=>{ctx.model.state.offset+=50;ctx.safely(loadMessages);};
ctx.$("chat-filter").onchange=renderChats;
ctx.$("track-chat").onclick=()=>ctx.safely(async()=>{const token=ctx.requests.begin("tracking");const chat=ctx.chatByID(ctx.model.state.chat);if(!chat)return;await ctx.control("/api/chats/"+encodeURIComponent(chat.ID)+"/tracking","PUT",{tracked:!chat.Tracked});if(!token.current())return;await loadChats();if(!token.current())return;
 if(chat.Tracked){const next=visibleChats()[0];if(next)await selectChat(next.ID);else{clearConversation();renderChats();}}
 else {ctx.model.chatTabs.tracked.chat=chat.ID;await switchChatTab("tracked");}
});
ctx.$("sync-chat").onclick=()=>ctx.safely(()=>ctx.control("/api/chats/"+encodeURIComponent(ctx.model.state.chat)+"/sync"));
ctx.$("export-chat").onclick=()=>ctx.safely(()=>ctx.control("/api/exports","POST",{chat_id:ctx.model.state.chat}));
ctx.$("new-messages").onclick=()=>{ctx.model.state.offset=0;ctx.safely(loadMessages);};
ctx.$("chat-search").oninput=renderChats;
ctx.$("chat-filter").onchange=renderChats;
return {loadChats,visibleChats,clearConversation,switchChatTab,renderChats,selectChat,loadMessages,updateChatControls};
}
