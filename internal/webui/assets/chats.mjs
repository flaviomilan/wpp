export function createChats(ctx) {
 const document=ctx.document||globalThis.document;
let chatRenderKey="";
async function loadChats() {
 const token=ctx.requests.begin("chats");const chats=await ctx.api("/api/chats");if(!token.current())return;ctx.model.state.chats=chats||[];
 ctx.bindText(ctx.$("group-count"),()=>ctx.number(ctx.model.state.chats.filter(c=>c.ID.endsWith("@g.us")).length));
 ctx.bindText(ctx.$("message-count"),()=>ctx.number(ctx.model.state.chats.reduce((sum,c)=>sum+c.Count,0)));
 ctx.bindText(ctx.$("tracked-count"),()=>ctx.number(ctx.model.state.chats.filter(c=>c.Tracked).length));
 const selected=ctx.$("people-group").value;
 ctx.options(ctx.$("people-group"),ctx.model.state.chats.filter(c=>c.ID.endsWith("@g.us")).map(c=>({value:c.ID,label:(c.Name||c.ID)+" · "+c.ID})));
 if(ctx.model.state.chats.some(c=>c.ID===selected))ctx.$("people-group").value=selected;
 if(!ctx.model.state.chat)ctx.model.state.chat=visibleChats()[0]?.ID||"";
 if(!ctx.model.state.chat)clearConversation();
 renderChats();
}

function visibleChats(){
 const query=ctx.$("chat-search").value.toLowerCase(),type=ctx.$("chat-filter").value;
 return ctx.model.state.chats.filter(c=>c.Tracked===(ctx.model.chatTab==="tracked")&&((c.Name||"")+" "+c.ID).toLowerCase().includes(query)&&(type==="all"||(type==="groups"&&c.ID.endsWith("@g.us"))||(type==="contacts"&&!c.ID.endsWith("@g.us"))));
}

function clearConversation(){
 ctx.model.state.chat="";ctx.model.state.offset=0;ctx.model.state.version++;ctx.model.displayedGeneration=-1;
 ctx.bindText(ctx.$("conversation-title"),()=>ctx.t("ui.select_a_chat"));ctx.bindText(ctx.$("conversation-id"),()=>"");
 ctx.$("messages").replaceChildren(ctx.el("div",()=>ctx.model.chatTab==="tracked"?ctx.t("ui.choose_a_tracked_chat_or_open_available_to_get_started"):ctx.t("ui.choose_a_chat_to_read_and_track"),"empty-state"));
 ctx.bindText(ctx.$("message-page"),()=>"—");ctx.$("messages-prev").disabled=true;ctx.$("messages-next").disabled=true;
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
 ctx.bindText(ctx.$("chat-count"),()=>ctx.number(chats.length));ctx.$("chat-list").replaceChildren();
 chats.forEach(chat=>{
  const node=ctx.el("button",undefined,"chat-item"+(ctx.model.state.chat===chat.ID?" selected":""));node.type="button";
  node.dataset.chat=chat.ID;
  node.append(ctx.el("span",()=>chat.ID.endsWith("@g.us")?"GR":"CT","avatar"));const info=ctx.el("span",undefined,"chat-info");
  info.append(ctx.el("strong",()=>chat.Name||ctx.t("ui.unnamed")),ctx.el("small",()=>ctx.number(chat.Count)+ctx.t("ui.messages_alternate")+(chat.Tracked?ctx.t("ui.tracked"):ctx.t("ui.available"))));node.append(info);
  node.onclick=()=>ctx.safely(()=>selectChat(chat.ID));ctx.$("chat-list").append(node);
  if(focused===chat.ID)node.focus({preventScroll:true});
 });
 if(!chats.length){
 const empty=ctx.el("div",()=>ctx.model.chatTab==="tracked"?ctx.t("ui.no_tracked_chats_match_these_filters"):ctx.t("ui.no_available_chats_match_these_filters"),"empty-state");
 if(ctx.model.chatTab==="tracked")empty.append(ctx.button(()=>ctx.t("ui.browse_available"),()=>ctx.safely(()=>switchChatTab("available"))));
 ctx.$("chat-list").append(empty);
 }
 ctx.$("chat-list").scrollTop=scroll;
 updateChatControls();
}

async function selectChat(id,transition=null,saved=null) {
 ctx.$("workspace").classList.add("reading");
 if(!transition)ctx.requests.invalidate();const token=transition||ctx.requests.begin("selection");
 ctx.model.state.chat=id;ctx.model.chatTabs[ctx.model.chatTab].chat=id;ctx.model.state.offset=saved?.offset||0;ctx.model.state.people=[];ctx.model.state.version++;renderChats();
 ctx.$("message-search").value=saved?.messageQuery||"";ctx.options(ctx.$("person-filter"),[],()=>ctx.t("ui.all_senders"));
 const chat=ctx.chatByID(id);
 if(chat&&chat.ID.endsWith("@g.us")) {
  const people=(await ctx.api(ctx.groupPath(id))).people||[];
  if(!token.current()||ctx.model.state.chat!==id)return;ctx.model.state.people=people;
  ctx.options(ctx.$("person-filter"),people.map(p=>({value:p.id,label:p.name||p.detected_name||p.id})),()=>ctx.t("ui.all_senders"));
 }
 if(saved)ctx.$("person-filter").value=saved.person;
 if(ctx.model.state.view==="groups")await loadMessages();
 if(token.current()&&saved)ctx.$("messages").scrollTop=saved.scroll;
}

async function loadMessages() {
 const chat=ctx.chatByID(ctx.model.state.chat);if(!chat)return;
 const token=ctx.requests.begin("messages");
 ctx.bindText(ctx.$("conversation-title"),()=>chat.Name||ctx.t("ui.unnamed_chat"));ctx.bindText(ctx.$("conversation-id"),()=>chat.ID);
 ctx.$("open-people").classList.toggle("hidden",!chat.ID.endsWith("@g.us"));
 const query=new URLSearchParams({offset:ctx.model.state.offset,limit:50,q:ctx.$("message-search").value,person:ctx.$("person-filter").value});
 const page=await ctx.api("/api/chats/"+encodeURIComponent(chat.ID)+"/messages?"+query);
 if(!token.current())return;
 ctx.model.displayedGeneration=chat.Generation;ctx.$("new-messages").classList.add("hidden");
 ctx.model.state.more=page.more;ctx.$("messages").replaceChildren();
 page.items.forEach(item=>{
  const m=item.message,node=ctx.el("article",undefined,"message"),heading=ctx.el("div",undefined,"message-heading");
  const date=ctx.el("time",()=>new Date(m.Time).toLocaleString(ctx.translator.language,{timeZone:ctx.model.timezone}));date.dateTime=m.Time;
  heading.append(ctx.el("strong",()=>item.route_presentation?ctx.renderRoute(item.route_presentation):item.route),date);node.append(heading);
  if(m.ReplyID||m.Kind==="reação"){
 const reply=item.reply;
 if(reply?.state==="available"){
 const quote=ctx.button(undefined,()=>ctx.safely(()=>ctx.openMessageDetail(reply.source_id)));
 quote.className="quoted-message";ctx.bindAttribute(quote,"aria-label",()=>ctx.t("ui.view_original_message_from")+(reply.author_code?ctx.t(reply.author_code):reply.author));
 if(m.Kind==="reação")quote.append(ctx.el("span",()=>ctx.t("ui.reacted_to_message"),"helper"));
 quote.append(ctx.el("strong",()=>(reply.author_code?ctx.t(reply.author_code):reply.author)+(reply.edited?ctx.t("ui.edited_alternate"):"")),ctx.el("span",()=>reply.preview+(reply.truncated?"…":"")));
 node.append(quote);
 }else node.append(ctx.el("div",()=>m.Kind==="reação"?ctx.t("ui.reaction_target_unavailable_in_the_archive"):ctx.t("ui.quoted_message_unavailable_in_the_archive"),"reply missing-quote"));
 }
  let text=m.Deleted?ctx.t("ui.deleted_message_alternate"):m.Text||"["+ctx.kindLabel(m.Kind)+"]";
  if(m.Caption)text+="\n"+m.Caption;if(m.FileName)text+="\nAnexo: "+m.FileName;
  node.append(ctx.el("div",()=>text,"message-text"));
  if(!m.Deleted&&m.Transcript){node.append(ctx.el("div",()=>ctx.t("ui.automatic_transcription_alternate")+m.Transcript.Model,"reply"),ctx.el("div",()=>m.Transcript.Text,"message-text"));}
  else if(!m.Deleted&&m.Kind==="áudio")node.append(ctx.el("div",()=>ctx.t("ui.transcription_alternate")+(ctx.model.audioStates[item.audio_state]||ctx.t("ui.unavailable")),"reply"));
  const meta=ctx.el("div",undefined,"message-meta");if(m.Edited)meta.append(ctx.el("span",()=>ctx.t("ui.edited")));
  if(item.preview_truncated)meta.append(ctx.el("span",()=>ctx.t("ui.limited_preview_read_the_full_evidence")));
  if(chat.ID.endsWith("@g.us")&&m.Sender)meta.append(ctx.button(()=>ctx.t("ui.identify_person"),()=>ctx.openPerson(chat.ID,m.Sender)));
  meta.append(ctx.button(()=>ctx.t("ui.view_record"),()=>ctx.record({...item,evidence_command:chat.ID.endsWith("@g.us")?"wpp evidence "+chat.ID+" "+item.source_id:undefined})));
  node.append(meta);ctx.$("messages").append(node);
 });
 if(!page.items.length)ctx.$("messages").append(ctx.el("div",()=>ctx.t("ui.no_messages_match_these_filters"),"empty-state"));
 ctx.$("messages-prev").disabled=ctx.model.state.offset===0;ctx.$("messages-next").disabled=!page.more;
 ctx.bindText(ctx.$("message-page"),()=>ctx.t("ui.page")+ctx.number(ctx.model.state.offset/50+1)+ctx.t("ui.newest_first"));
}

function updateChatControls(){
 const chat=ctx.chatByID(ctx.model.state.chat),owner=!!ctx.model.serviceState?.owner;
 ctx.$("track-chat").disabled=!owner||!chat;ctx.bindText(ctx.$("track-chat"),()=>chat?.Tracked?ctx.t("ui.stop_tracking"):ctx.t("ui.track"));
 const syncing=ctx.model.serviceState?.operations?.some(op=>op.kind==="sync"&&op.chat===ctx.model.state.chat&&["queued","running"].includes(op.state));ctx.$("sync-chat").disabled=!owner||!chat?.Tracked||syncing;ctx.bindText(ctx.$("sync-chat"),()=>syncing?ctx.t("ui.syncing"):ctx.t("ui.sync_now"));ctx.$("export-chat").disabled=!owner||!chat;
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
