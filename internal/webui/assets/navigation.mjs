export function createNavigation(ctx) {
 const document=ctx.document||globalThis.document;
 const window=ctx.window||globalThis.window;
 const categories=['account','capture','transcription','data','advanced'];
 let polling=false,category='account',pending=null,returnFocus=null;
 const scrolls={};
 function route(){
  const [view,section]=(window.location.hash.slice(1)||'groups').split('/');
  return {view:Object.hasOwn(ctx.model.labels,view)?view:'groups',section:categories.includes(section)?section:'account'};
 }
 function closeMenu(){ctx.$('sidebar').classList.remove('nav-open');ctx.$('menu-toggle').setAttribute('aria-expanded','false');}
 function navigate(view,section,fromHistory=false){
  const previous=ctx.model.state.view;
  scrolls[previous]={page:window.scrollY,messages:ctx.$('messages').scrollTop};
  ctx.requests.invalidate();ctx.model.state.view=view;ctx.model.state.version++;
  ctx.model.readonlyRetryNotice='';ctx.model.readonlyRetryCode='';ctx.$('notice').classList.add('hidden');
  for(const key of Object.keys(ctx.model.labels))ctx.$(key+'-view').classList.toggle('hidden',key!==view);
  document.querySelectorAll('[data-view]').forEach(button=>{
   button.classList.toggle('active',button.dataset.view===view);
   if(button.dataset.view===view)button.setAttribute('aria-current','page');else button.removeAttribute('aria-current');
  });
  ctx.bindText(ctx.$('page-title'),()=>ctx.model.labels[view][0]);ctx.bindText(ctx.$('page-description'),()=>ctx.model.labels[view][1]);ctx.bindText(ctx.$('breadcrumb'),()=>ctx.t("ui.archive")+ctx.model.labels[view][0]);
  ctx.$('discover').classList.toggle('hidden',view!=='groups');
  if(view==='settings'){
   category=section;
   for(const name of categories){
    ctx.$('settings-'+name).classList.toggle('hidden',name!==category);
    if(name===category)ctx.$('category-'+name).setAttribute('aria-current','page');else ctx.$('category-'+name).removeAttribute('aria-current');
   }
  }
  if(!fromHistory){const hash='#'+view+(view==='settings'?'/'+category:'');if(window.location.hash!==hash)window.history.pushState(null,'',hash);}
  closeMenu();ctx.$("page-title").focus({preventScroll:true});
  // Content stays mounted; changing areas never rebuilds the conversation being read.
  if(view==='people')ctx.safely(ctx.loadPeople);else if(view==='audio')ctx.safely(ctx.loadAudio);else if(view==='settings'||view==='activity')ctx.safely(ctx.loadService);
  const saved=scrolls[view];window.scrollTo(0,saved?.page||0);if(saved)ctx.$('messages').scrollTop=saved.messages;
 }
 function changeView(view,section=category,fromHistory=false){
  if(!Object.hasOwn(ctx.model.labels,view))view='groups';
  if(!categories.includes(section))section='account';
  if(ctx.model.state.view==='settings'&&ctx.settingsChanged()&&(view!=='settings'||section!==category)){
   pending=()=>navigate(view,section,false);returnFocus=document.activeElement;
   if(fromHistory)window.history.replaceState(null,'','#settings/'+category);
   ctx.$('unsaved-dialog').showModal();return;
  }
  navigate(view,section,fromHistory);
 }
 function restoreNavigation(){const next=route();changeView(next.view,next.section,true);}
 function keepEditing(){pending=null;ctx.$('unsaved-dialog').close();returnFocus?.focus();}
 function discard(){ctx.discardSettings();ctx.$('unsaved-dialog').close();const action=pending;pending=null;action();ctx.$('page-title').focus();}
 function onKey(event){if(event.key==='Escape'&&ctx.$('sidebar').classList.contains('nav-open')){closeMenu();ctx.$('menu-toggle').focus();}}
 function renderOnboarding(snapshot){
  let title='',description='',action='',target;
  if(!snapshot.owner){ctx.$('onboarding').classList.add('hidden');return;}
  if(!snapshot.linked){title="ui.your_archive_starts_here";description="ui.link_your_account_to_discover_chats_your_archive_remains_available";action="ui.connect_account";target=()=>changeView('settings','account');}
  else if(!ctx.model.state.chats.some(chat=>chat.Tracked)){
   title="ui.choose_what_to_track";description="ui.discover_chats_and_track_only_those_you_want_to_archive";action=ctx.model.state.chats.length?"ui.choose_chats":"ui.discover_chats";
   target=()=>ctx.safely(async()=>{changeView('groups');if(ctx.model.state.chats.length)await ctx.switchChatTab('available');else await ctx.control('/api/chats/discover');});
  } else if(!snapshot.settings.capture_enabled){title="ui.your_archive_is_ready";description="ui.enable_continuous_capture_or_sync_a_chat_when_needed_transcription";action="ui.configure_capture";target=()=>changeView('settings','capture');}
  ctx.$('onboarding').classList.toggle('hidden',!title);
  ctx.bindText(ctx.$('onboarding-title'),()=>title?ctx.t(title):"");ctx.bindText(ctx.$('onboarding-description'),()=>description?ctx.t(description):"");ctx.bindText(ctx.$('onboarding-action'),()=>action?ctx.t(action):"");ctx.$('onboarding-action').onclick=target;
 }
 async function poll(){
  if(polling||document.hidden)return;polling=true;const token=ctx.requests.begin('poll');
  try{
   await ctx.loadPreferences();if(!token.current())return;await ctx.loadService();if(!token.current())return;await ctx.loadChats();if(!token.current())return;
   const selected=ctx.$('audio-chat').value;ctx.options(ctx.$('audio-chat'),ctx.model.state.chats.map(c=>({value:c.ID,label:c.Name||c.ID})),()=>ctx.t("ui.all_chats"));ctx.$('audio-chat').value=selected;
   if(ctx.model.state.view==='groups'&&ctx.model.state.chat&&ctx.chatByID(ctx.model.state.chat)?.Generation!==ctx.model.displayedGeneration){
    if(ctx.model.state.offset===0&&window.scrollY<30&&ctx.$('messages').scrollTop<30&&!ctx.$('message-search').value&&!ctx.$('person-filter').value)await ctx.loadMessages();
    else ctx.$('new-messages').classList.remove('hidden');
   }
   if(ctx.model.state.view==='audio')await ctx.loadAudio();
  }catch(error){ctx.showError(error);}finally{polling=false;}
 }
 document.querySelectorAll('[data-view]').forEach(button=>button.onclick=()=>changeView(button.dataset.view));
 for(const name of categories)ctx.$('category-'+name).onclick=()=>changeView('settings',name);
 ctx.$('page-title').tabIndex=-1;
 ctx.$('skip-content').onclick=event=>{event.preventDefault();ctx.$('main-content').focus();};
 ctx.$('menu-toggle').onclick=()=>{const open=ctx.$('sidebar').classList.toggle('nav-open');ctx.$('menu-toggle').setAttribute('aria-expanded',String(open));if(open)ctx.$('close-menu').focus();};
 ctx.$('close-menu').onclick=()=>{closeMenu();ctx.$('menu-toggle').focus();};
 ctx.$('open-capture').onclick=()=>changeView('settings','capture');ctx.$('open-activity').onclick=()=>changeView('activity');ctx.$('audio-settings').onclick=()=>changeView('settings','transcription');
 ctx.$('back-to-list').onclick=()=>{ctx.$('workspace').classList.remove('reading');const selected=Array.from(ctx.$('chat-list').children).find(node=>node.classList.contains('selected'));selected?.focus();};
 ctx.$('keep-editing').onclick=keepEditing;ctx.$('discard-settings').onclick=discard;
 ctx.$('unsaved-dialog').addEventListener('cancel',event=>{event.preventDefault();keepEditing();});
 window.addEventListener('popstate',restoreNavigation);window.addEventListener('hashchange',restoreNavigation);document.addEventListener('keydown',onKey);
 ctx.$('refresh').onclick=()=>ctx.safely(async()=>{await ctx.loadChats();await ctx.loadService();if(ctx.model.state.view==='groups')await ctx.loadMessages();else if(ctx.model.state.view==='people')await ctx.loadPeople();else if(ctx.model.state.view==='audio')await ctx.loadAudio();});
 return {changeView,poll,renderOnboarding,restoreNavigation,stopNavigation(){window.removeEventListener('popstate',restoreNavigation);window.removeEventListener('hashchange',restoreNavigation);document.removeEventListener('keydown',onKey);}};
}
