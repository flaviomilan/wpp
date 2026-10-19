import test from 'node:test';
import assert from 'node:assert/strict';
import {startApp} from '../assets/app.js';
import {fixture,texts,descendants,flush} from './fixture.mjs';
import {createDOM} from '../assets/dom.mjs';

async function app(handler){const f=fixture(handler);const a=startApp(f.document,f.window,f.navigator);await a.ready;return {...f,...a};}
const event={preventDefault(){}};

test('bootstrap, DOM helpers and cleanup preserve safe text and stop polling',async()=>{
 const f=await app();assert.equal(f.ctx.model.state.chat,'T@g.us');assert.equal(f.timers.size,1);
 assert.equal(f.$('connection-badge').textContent,'Conectado');
 f.$('close-record').onclick();f.listeners.get('visibilitychange')();await flush();
 const dom=createDOM(f.document);dom.record({text:'<script>'});assert.match(f.$('record-content').textContent,/<script>/);assert.ok(f.$('record-dialog').open);
 dom.options(f.$('table-select'),[{label:'Conversas',value:'chats'}],'Selecione');const old=f.$('table-select').children[1];dom.options(f.$('table-select'),[{label:'Conversas',value:'chats'}],'Selecione');assert.equal(old,f.$('table-select').children[1]);
 await dom.safely(()=>{throw new Error('erro controlado');});assert.equal(f.$('notice').textContent,'erro controlado');
 f.stop();assert.equal(f.timers.size,0);assert.equal(f.listeners.has('visibilitychange'),false);
});

test('conversation actions preserve filters and move selection after untracking',async()=>{
 const f=await app(async(path,options,data)=>{if(path.endsWith('/tracking')){const body=JSON.parse(options.body);data.chats.find(c=>path.includes(encodeURIComponent(c.ID))).Tracked=body.tracked;return {};}});
 try{
  await f.ctx.switchChatTab('available');assert.equal(f.ctx.model.state.chat,'A@g.us');
  await f.$('track-chat').onclick();assert.equal(f.ctx.model.chatTab,'tracked');assert.equal(f.ctx.model.state.chat,'A@g.us');
  await f.$('track-chat').onclick();assert.equal(f.ctx.model.state.chat,'T@g.us');
  await f.$('chat-list').children[0].onclick();assert.equal(f.ctx.model.state.chat,'T@g.us');
  f.$('chat-search').value='contato';f.$('chat-filter').value='contacts';f.$('chat-search').oninput();assert.equal(f.ctx.visibleChats()[0].ID,'123@s.whatsapp.net');
  f.$('chat-search').value='';f.$('chat-filter').value='all';f.$('chat-filter').onchange();
  await f.$('sync-chat').onclick();await f.$('export-chat').onclick();assert.ok(f.calls.some(c=>c.path.endsWith('/sync')));assert.ok(f.calls.some(c=>c.path==='/api/exports'));
  f.$('tab-tracked').onkeydown({...event,key:'End'});await flush();assert.equal(f.ctx.model.chatTab,'available');
  f.$('tab-available').onkeydown({...event,key:'Home'});await flush();assert.equal(f.ctx.model.chatTab,'tracked');
  await f.$('tab-available').onclick();await f.$('tab-tracked').onclick();
  f.data.chats=[];await f.ctx.loadChats();f.ctx.clearConversation();f.ctx.renderChats();assert.match(texts(f.$('chat-list')),/Nenhuma conversa/);
  await f.$('chat-list').children[0].children[0].onclick();assert.equal(f.ctx.model.chatTab,'available');
 }finally{f.stop();}
});

test('messages expose quoted text, transcription and safe record actions',async()=>{
 const f=await app();try{
 f.data.messages={more:true,items:[{source_id:'s1',route:'Ana → grupo',preview_truncated:true,reply:{state:'available',author:'Bruno',source_id:'s0',edited:true,preview:'Anterior',truncated:true},message:{ID:'m1',Time:'2026-10-14T12:00:00Z',Sender:'123@lid',Kind:'áudio',Caption:'legenda',FileName:'arquivo',ReplyID:'m0',Edited:true,Transcript:{Text:'Transcrição 👋',Model:'turbo'}}},{source_id:'s2',message:{Time:'2026-10-14T12:00:00Z',Kind:'áudio'}},{source_id:'s3',message:{Time:'2026-10-14T12:00:00Z',Kind:'texto',Deleted:true,ReplyID:'missing'}}]};
 await f.ctx.loadMessages();assert.match(texts(f.$('messages')),/Bruno · editada/);assert.match(texts(f.$('messages')),/Mensagem excluída/);
 const buttons=f.$('messages').querySelectorAll('button');await buttons[0].onclick();assert.ok(f.$('message-detail').open);buttons[1].onclick();assert.ok(f.$('person-dialog').open);buttons[2].onclick();assert.match(f.$('record-content').textContent,/wpp evidence/);
 f.$('messages-next').onclick();await flush();assert.equal(f.ctx.model.state.offset,50);f.$('messages-prev').onclick();await flush();assert.equal(f.ctx.model.state.offset,0);
 f.$('message-search-form').onsubmit(event);f.$('person-filter').onchange();f.$('new-messages').onclick();await flush();
 }finally{f.stop();}
});

test('audio cards validate full text, retain focus, retry selected source and copy',async()=>{
 const f=await app();try{
 f.ctx.changeView('audio');
 f.data.audio={summary:{done:1,pending:1},more:true,items:[{source_id:'ok',chat:'T@g.us',author:'Ana',time:'2026-10-14T12:00:00Z',attempts:1,state:'done',has_transcript:true,transcript_preview:'Olá',preview_truncated:true},{source_id:'failed',chat:'T@g.us',time:'2026-10-14T12:00:00Z',attempts:2,state:'pending',phase:'downloading',error:'falhou',retry_eligible:true,tracked:true,retry_reason:'Tente novamente'}]};
 await f.ctx.loadAudio();assert.match(texts(f.$('audio-cards')),/Olá…/);
 const read=f.$('audio-cards').querySelectorAll('button')[0];read.focus();await read.onclick();assert.match(f.$('detail-body').textContent+texts(f.$('detail-body')),/Olá 👋/);
 await f.$('copy-transcript').onclick();assert.deepEqual(f.copied,['Olá 👋']);
 f.navigator.clipboard.writeText=async()=>{throw new Error('denied');};await f.$('copy-transcript').onclick();assert.match(f.$('copy-status').textContent,/teclado/);
 f.data.audio.items[0].attempts=2;await f.ctx.loadAudio();assert.equal(f.document.activeElement.dataset.source,'ok');
 await f.$('audio-cards').querySelectorAll('button')[1].onclick();assert.match(f.ctx.model.readonlyRetryNotice,/externo/);assert.ok(f.calls.some(c=>c.path==='/api/audio/failed/retry'));
 f.$('close-detail').onclick();assert.equal(f.$('message-detail').open,false);
 f.$('audio-next').onclick();await flush();assert.match(f.$('audio-page').textContent,/2/);f.$('audio-prev').onclick();await flush();
 f.$('audio-chat').onchange();f.$('audio-state').onchange();await flush();
 await f.ctx.openMessageDetail('msg');assert.match(texts(f.$('detail-body')),/texto/);
 f.data.detail.message.Deleted=true;await f.ctx.openMessageDetail('deleted',true);assert.match(texts(f.$('detail-body')),/excluída/);
 f.data.audio.items=[];await f.ctx.loadAudio();assert.match(texts(f.$('audio-cards')),/Nenhum áudio/);
 }finally{f.stop();}
});

test('people registration handles success and error without losing conversation',async()=>{
 let rejected=false;const f=await app((path,options)=>{if(path.startsWith('/api/groups/')&&options){if(rejected)throw new Error('Nome inválido');return {};}});try{
 f.$('open-people').onclick();await flush();assert.equal(f.ctx.model.state.view,'people');assert.match(texts(f.$('people-rows')),/Ana/);
 f.$('people-rows').querySelectorAll('button')[0].onclick();assert.equal(f.$('person-id').value,'123@lid');assert.equal(f.document.activeElement,f.$('person-name'));
 f.$('person-name').value=' Ana nova ';await f.$('person-form').onsubmit(event);assert.ok(!f.$('person-dialog').open);assert.ok(f.calls.some(c=>c.options?.body?.includes('Ana nova')));
 f.$('add-person').onclick();assert.equal(f.document.activeElement,f.$('person-id'));rejected=true;await f.ctx.savePerson(event);assert.equal(f.$('person-error').textContent,'Nome inválido');assert.equal(f.$('save-person').disabled,false);
 f.$('close-person').onclick();f.$('cancel-person').onclick();f.$('people-search').value='inexistente';f.$('people-search').oninput();assert.match(texts(f.$('people-rows')),/Nenhuma pessoa/);
 f.$('people-group').value='';f.$('people-group').onchange();await flush();assert.match(texts(f.$('people-rows')),/Nenhum grupo/);
 f.ctx.model.state.view='groups';f.$('person-form').dataset.group='T@g.us';rejected=false;await f.ctx.savePerson(event);
 }finally{f.stop();}
});

test('settings controls persist choices, cancel operations and preserve dirty fields',async()=>{
 const f=await app();try{
 for(const id of ['discover','pair-account','export-all'])await f.$(id).onclick();
 for(const id of ['capture-toggle','transcription-toggle']){f.$(id).checked=true;await f.$(id).onchange();}
 f.ctx.changeView('settings');await flush();f.$('settings-form').oninput();f.$('setting-vocabulary').value='Ana';await f.ctx.loadService();assert.equal(f.$('setting-vocabulary').value,'Ana');await f.$('settings-form').onsubmit(event);await flush();assert.ok(f.calls.some(c=>c.path==='/api/service/settings'&&c.options.method==='PUT'));
 f.data.snapshot.operations=[{id:'pair',kind:'pair',state:'running',phase:'pairing',detail:'Leia o QR',updated:'2026-10-14T12:00:00Z'}];f.data.snapshot.qr_available=true;await f.ctx.loadService();assert.match(f.$('pair-qr').src,/\/api\/session\/qr/);await f.$('cancel-pair').onclick();
 await f.$('operation-status').querySelectorAll('button')[0].onclick();await f.$('recent-operations').querySelectorAll('button')[0].onclick();
 f.data.snapshot.operations=[];await f.ctx.loadService();await f.$('cancel-pair').onclick();
 f.$('inspector-details').open=true;f.$('table-select').value='chats';await f.$('inspector-details').ontoggle();await flush();assert.match(texts(f.$('table-head')),/name/);f.$('table-rows').querySelectorAll('button')[1].onclick();assert.match(f.$('record-content').textContent,/null/);
 f.$('table-next').onclick();f.$('table-prev').onclick();f.$('table-select').onchange();await flush();assert.equal(f.ctx.model.state.tableOffset,0);
 f.data.table.rows=[];await f.ctx.loadTable();assert.match(texts(f.$('table-rows')),/sem registros/);
 }finally{f.stop();}
});

test('polling does not jump reading, handles errors and avoids hidden-page requests',async()=>{
 let fail=false;const f=await app(path=>{if(fail&&path==='/api/service')throw new Error('offline');});try{
 f.ctx.model.displayedGeneration=-1;f.$('messages').scrollTop=100;await f.ctx.poll();assert.equal(f.$('new-messages').classList.contains('hidden'),false);assert.equal(f.$('messages').scrollTop,100);
 f.$('messages').scrollTop=0;await f.ctx.poll();assert.equal(f.ctx.model.displayedGeneration,1);
 for(const view of ['people','audio','settings','groups']){f.views.find(n=>n.dataset.view===view).onclick();await flush();await f.$('refresh').onclick();}
 f.document.hidden=true;const count=f.calls.length;await f.ctx.poll();assert.equal(f.calls.length,count);f.document.hidden=false;fail=true;await f.ctx.poll();assert.equal(f.$('notice').textContent,'offline');
 assert.ok(descendants(f.$('messages')).length>0);
 }finally{f.stop();}
});

test('navigation routes, mobile menu and dirty settings preserve reading and draft',async()=>{
 const f=await app();try{
  f.$('menu-toggle').onclick();assert.equal(f.$('menu-toggle')['aria-expanded'],'true');
  f.listeners.get('keydown')({key:'Escape'});assert.equal(f.$('menu-toggle')['aria-expanded'],'false');
  f.$('menu-toggle').onclick();f.$('close-menu').onclick();
  f.$('messages').scrollTop=120;f.window.scrollY=60;
  f.ctx.changeView('settings','transcription');await flush();
  assert.equal(f.$('category-transcription')['aria-current'],'page');
  f.$('skip-content').onclick(event);assert.equal(f.document.activeElement,f.$('main-content'));assert.equal(f.ctx.model.state.view,'settings');
  f.$('setting-vocabulary').value='Rascunho';f.$('settings-form').oninput();
  f.ctx.changeView('audio');assert.ok(f.$('unsaved-dialog').open);f.$('keep-editing').onclick();assert.equal(f.ctx.model.state.view,'settings');
  f.$('category-data').onclick();f.$('unsaved-dialog').listeners.get('cancel')[0](event);assert.equal(f.ctx.model.state.view,'settings');
  f.ctx.changeView('audio');f.$('discard-settings').onclick();await flush();assert.equal(f.ctx.model.state.view,'audio');assert.notEqual(f.$('setting-vocabulary').value,'Rascunho');
  f.ctx.changeView('groups');assert.equal(f.$('messages').scrollTop,120);
  f.ctx.changeView('invalid','invalid');assert.equal(f.ctx.model.state.view,'groups');
  f.window.location.hash='#settings/advanced';f.windowListeners.get('hashchange')();await flush();assert.equal(f.$('category-advanced')['aria-current'],'page');
  f.$('settings-form').oninput();f.window.location.hash='#audio';f.windowListeners.get('popstate')();assert.equal(f.window.location.hash,'#settings/advanced');f.$('discard-settings').onclick();await flush();
  f.window.location.hash='#unknown/unknown';f.ctx.restoreNavigation();assert.equal(f.ctx.model.state.view,'groups');
  f.$('open-capture').onclick();assert.equal(f.ctx.model.state.view,'settings');f.$('open-activity').onclick();assert.equal(f.ctx.model.state.view,'activity');f.$('audio-settings').onclick();assert.equal(f.$('category-transcription')['aria-current'],'page');
  f.$('back-to-list').onclick();assert.equal(f.$('workspace').classList.contains('reading'),false);
 }finally{f.stop();}
});

test('onboarding follows account and tracking; invalid settings keep the editable draft',async()=>{
 let invalid=false;const f=await app((path,options)=>{if(invalid&&path==='/api/service/settings'&&options)throw new Error('Modelo inválido');});try{
  f.data.snapshot.linked=false;await f.ctx.loadService();f.$('onboarding-action').onclick();assert.equal(f.$('category-account')['aria-current'],'page');
  f.data.snapshot.linked=true;f.data.chats=[];await f.ctx.loadChats();await f.ctx.loadService();await f.$('onboarding-action').onclick();assert.ok(f.calls.some(c=>c.path==='/api/chats/discover'));
  f.data.chats=[{ID:'x@g.us',Tracked:false}];await f.ctx.loadChats();await f.ctx.loadService();await f.$('onboarding-action').onclick();assert.equal(f.ctx.model.chatTab,'available');
  f.data.chats[0].Tracked=true;await f.ctx.loadChats();await f.ctx.loadService();f.$('onboarding-action').onclick();assert.equal(f.$('category-capture')['aria-current'],'page');
  f.data.snapshot.settings.capture_enabled=true;await f.ctx.loadService();assert.equal(f.$('onboarding').classList.contains('hidden'),true);
  f.data.snapshot.owner=false;await f.ctx.loadService();assert.equal(f.$('owner-notice').classList.contains('hidden'),false);
  f.data.snapshot.owner=true;invalid=true;f.$('setting-model').value='errado';f.$('settings-form').oninput();await f.$('settings-form').onsubmit(event);assert.equal(f.$('settings-feedback').textContent,'Modelo inválido');assert.equal(f.$('setting-model').value,'errado');
 }finally{f.stop();}
});
