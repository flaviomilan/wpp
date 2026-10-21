import test from 'node:test';
import assert from 'node:assert/strict';
import {startApp} from '../assets/app.js';
import {fixture,flush} from './fixture.mjs';
import {createAPI} from '../assets/api.mjs';
import {createTranslator} from '../assets/i18n.mjs';

const event={preventDefault(){}};
async function start(handler){const f=fixture(handler);f.data.preferences={application_language:'en',initial_language:'en'};f.document.documentElement={lang:'en'};const app=startApp(f.document,f.window,f.navigator);await app.ready;return {...f,...app};}

test('English first use, explicit saved Portuguese and live text preserve reading and drafts',async()=>{
 const f=await start();try{
  assert.equal(f.ctx.translator.language,'en');assert.equal(f.ctx.number(1234),'1,234');assert.equal(f.$('connection-badge').textContent,'Connected');
  f.ctx.changeView('settings','advanced');await flush();
  f.$('messages').scrollTop=121;f.$('setting-vocabulary').value='Unsubmitted vocabulary';f.$('settings-form').oninput();
  f.$('application-language').value='pt-BR';f.$('application-language').onchange();await f.ctx.poll();
  assert.equal(f.$('application-language').value,'pt-BR');assert.equal(f.ctx.translator.language,'en');
  const selected=f.ctx.model.state.chat;f.$('application-language').focus();await f.$('language-form').onsubmit(event);
  assert.equal(f.ctx.translator.language,'pt-BR');assert.equal(f.ctx.number(1234),'1.234');assert.equal(f.document.documentElement.lang,'pt-BR');
  assert.equal(f.$('connection-badge').textContent,'Conectado');assert.equal(f.ctx.model.state.chat,selected);assert.equal(f.$('messages').scrollTop,121);
  assert.equal(f.$('setting-vocabulary').value,'Unsubmitted vocabulary');assert.equal(f.document.activeElement,f.$('application-language'));
  assert.equal(f.data.snapshot.settings.language,'pt');assert.equal(f.data.preferences.application_language,'pt-BR');
  assert.equal(f.calls.filter(c=>c.path==='/api/service/settings'&&c.options).length,0);
  f.ctx.discardSettings();f.$('application-language').value='en';f.$('application-language').onchange();f.ctx.changeView('audio');assert.ok(f.$('unsaved-dialog').open);
  f.$('keep-editing').onclick();assert.equal(f.$('application-language').value,'en');
  f.ctx.changeView('audio');f.$('discard-settings').onclick();await flush();assert.equal(f.$('application-language').value,'pt-BR');
 }finally{f.stop();}
});

test('save failure retains selection and previous language; consultation mode permits retry',async()=>{
 let fail=true;
 const f=await start((path,options)=>{if(path==='/api/preferences'&&options?.method==='PUT'&&fail)throw new Error('Storage unavailable');});try{
  f.data.snapshot.owner=false;await f.ctx.loadService();f.$('application-language').value='pt-BR';f.$('application-language').onchange();
  await f.$('language-form').onsubmit(event);assert.equal(f.ctx.translator.language,'en');assert.equal(f.$('application-language').value,'pt-BR');assert.match(f.$('language-feedback').textContent,/Storage unavailable/);
  assert.equal(f.$('save-language').disabled,false);fail=false;await f.$('language-form').onsubmit(event);assert.equal(f.ctx.translator.language,'pt-BR');assert.equal(f.$('capture-toggle').disabled,true);
 }finally{f.stop();}
});

test('initial override is temporary and an explicit panel save replaces it',async()=>{
 const f=fixture();f.data.preferences={application_language:'pt-BR',initial_language:'en'};const app=startApp(f.document,f.window,f.navigator);await app.ready;try{
  assert.equal(app.ctx.translator.language,'en');assert.equal(f.data.preferences.application_language,'pt-BR');
  await app.ctx.loadPreferences();assert.equal(app.ctx.translator.language,'en');
  f.$('application-language').value='pt-BR';await f.$('language-form').onsubmit(event);assert.equal(app.ctx.translator.language,'pt-BR');
 }finally{app.stop();}
});

test('coded public errors follow the active language and never expose unknown identifiers',async()=>{
 const translator=createTranslator();const api=createAPI(async()=>({ok:false,json:async()=>({code:'language.invalid',error:'fallback'})}),translator.text);
 let failure;try{await api('/api/preferences');}catch(error){failure=error;}
 assert.match(failure.message,/Use en or pt-BR/);translator.setLanguage('pt-BR');assert.match(failure.message,/Use en ou pt-BR/);
 await assert.rejects(createAPI(async()=>({ok:false,json:async()=>({code:'private.unknown'})}))('/api/preferences'),/The operation failed/);
 await assert.rejects(createAPI(async()=>({ok:false,json:async()=>({})}))('/api/preferences'),/Request failed/);
});

test('localized state maps and participant routing preserve names and native values',async()=>{
 const f=await start();try{
  for(const group of [f.ctx.model.labels,f.ctx.model.audioStates,f.ctx.model.audioPhases])for(const value of Object.values(group))assert.ok(value);
  assert.equal(f.ctx.renderRoute({from:'Marina',to:'Aurora',group:true}),'Marina → group Aurora');
  assert.equal(f.ctx.renderRoute({from_self:true,to:'Marina'}),'You → Marina');
  assert.equal(f.ctx.renderRoute({from:'Marina',to_self:true,to:'Rafael'}),'Marina → Rafael (you)');
  assert.equal(f.ctx.renderRoute({from:'Marina',to_self:true,to:''}),'Marina → You');
  f.ctx.applyLanguage('pt-BR');assert.equal(f.ctx.renderRoute({from_self:true,to:'Aurora',group:true}),'Você → grupo Aurora');
  for(const connection of ['connected','connecting','disconnected','reconnecting','needs_login','external','error']){f.data.snapshot.connection=connection;await f.ctx.loadService();assert.ok(f.$('connection-badge').textContent);}
  const operations=['queued','running','succeeded','warning','failed','cancelled','interrupted'].map((state,index)=>({id:String(index),state,detail:'legacy evidence',updated:'2026-10-20T12:00:00Z'}));
  f.ctx.renderActivity(operations);f.ctx.applyLanguage('en');assert.ok(f.$('recent-operations').children.length);
 }finally{f.stop();}
});

test('static accessible text and content-kind labels change without replacing source content',async()=>{
 const f=fixture(),label=f.document.createElement('span'),input=f.$('chat-search');label.dataset.i18n='ui.settings';input.getAttribute=()=> 'ui.search_chats';
 const query=f.document.querySelectorAll.bind(f.document);
 f.document.querySelectorAll=selector=>selector==='[data-i18n]'?[label]:selector==='[data-i18n-placeholder]'?[input]:query(selector);
 const a=startApp(f.document,f.window,f.navigator);await a.ready;try{
  a.ctx.applyLanguage('en');assert.equal(label.textContent,'Settings');assert.equal(input.placeholder,'Search chats');assert.equal(a.ctx.kindLabel('áudio'),'audio');
  a.ctx.applyLanguage('pt-BR');assert.equal(label.textContent,'Ajustes');assert.equal(input.placeholder,'Buscar conversa');assert.equal(a.ctx.kindLabel('áudio'),'áudio');
  assert.equal(a.ctx.kindLabel('unknown'),'conteúdo não interpretado');
 }finally{a.stop();}
});
