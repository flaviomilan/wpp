import {readFileSync} from 'node:fs';

class Element {
 constructor(document,tag='div'){this.document=document;this.tagName=tag.toUpperCase();this.value='';this.textContent='';this.dataset={};this.children=[];this.scrollTop=0;this.open=false;this.listeners=new Map();this.classes=new Set();this.classList={add:(...names)=>names.forEach(n=>this.classes.add(n)),remove:(...names)=>names.forEach(n=>this.classes.delete(n)),toggle:(name,force)=>{const active=force??!this.classes.has(name);if(active)this.classes.add(name);else this.classes.delete(name);return active;},contains:name=>this.classes.has(name)};}
 append(...nodes){this.children.push(...nodes);}
 prepend(...nodes){this.children.unshift(...nodes);}
 replaceChildren(...nodes){this.children=[...nodes];}
 setAttribute(key,value){this[key]=value;}
 removeAttribute(key){delete this[key];}
 focus(){this.document.activeElement=this;}
 showModal(){this.open=true;}
 close(){this.open=false;for(const fn of this.listeners.get('close')||[])fn();}
 addEventListener(name,fn){this.listeners.set(name,[...(this.listeners.get(name)||[]),fn]);}
 removeEventListener(name,fn){this.listeners.set(name,(this.listeners.get(name)||[]).filter(f=>f!==fn));}
 querySelectorAll(selector){return this.children.flatMap(n=>[...(selector==='button'&&n.tagName==='BUTTON'?[n]:[]),...n.querySelectorAll(selector)]);}
}
export function fixture(handler){
 const nodes=new Map(),listeners=new Map();
 const document={hidden:false,activeElement:null,getElementById:id=>{if(!nodes.has(id))throw new Error('Unknown element '+id);return nodes.get(id);},createElement:tag=>new Element(document,tag),querySelectorAll:selector=>selector==='[data-view]'?views:selector==='[data-control]'?controls:[],addEventListener:(name,fn)=>listeners.set(name,fn),removeEventListener:(name,fn)=>{if(listeners.get(name)===fn)listeners.delete(name);}};
 const html=readFileSync(new URL('../assets/index.html',import.meta.url),'utf8');
 for(const match of html.matchAll(/<([a-z]+)[^>]*\bid="([^"]+)"[^>]*>/g)){
  const node=new Element(document,match[1]);node.id=match[2];nodes.set(match[2],node);
 }
 const views=['groups','audio','people','activity','settings'].map(view=>{const n=new Element(document,'button');n.dataset.view=view;return n;});
 const controls=[document.getElementById('capture-toggle'),document.getElementById('transcription-toggle')];
 const $=id=>document.getElementById(id);
 $('chat-filter').value='all';$('audio-state').value='';$('setting-language').value='pt';
 const calls=[],timers=new Map(),copied=[];
 const settings={capture_enabled:false,transcription_enabled:true,language:'pt',threads:2,timeout:'30m',model_path:'/tmp/model',whisper_path:'whisper',ffmpeg_path:'ffmpeg'};
 const data={preferences:{application_language:"pt-BR",initial_language:"pt-BR"},chats:[{ID:'T@g.us',Name:'Trabalho',Tracked:true,Count:1,Generation:1},{ID:'A@g.us',Name:'Disponível',Tracked:false,Count:0},{ID:'123@s.whatsapp.net',Name:'Contato',Tracked:true,Count:0}],snapshot:{owner:true,linked:true,connection:'connected',audio_available:true,settings,operations:[]},people:[{id:'123@lid',name:'Ana',detected_name:'Ana antiga'}],messages:{items:[],more:false},audio:{summary:{done:1},items:[],more:false},detail:{source_id:'source',author:'Ana',time:'2026-10-14T12:00:00Z',message:{ID:'msg',Chat:'T@g.us',Time:'2026-10-14T12:00:00Z',Text:'texto',Kind:'texto',Transcript:{Text:'Olá 👋',Model:'turbo'}}},table:{columns:['id','name'],rows:[[1,null]],more:false}};
 const api=async(path,options)=>{calls.push({path,options});if(handler){const result=await handler(path,options,data);if(result!==undefined)return result;}if(path==='/api/preferences'){if(options?.method==='PUT'){data.preferences.application_language=JSON.parse(options.body).application_language;data.preferences.initial_language=data.preferences.application_language;}return data.preferences;}if(path==='/api/settings')return {timezone:'UTC'};if(path==='/api/chats')return data.chats;if(path==='/api/service')return data.snapshot;if(path.startsWith('/api/groups/'))return {people:data.people};if(path.includes('/messages?'))return data.messages;if(path.startsWith('/api/audio?'))return data.audio;if(path.startsWith('/api/audio/')||path.startsWith('/api/messages/'))return options?{status:'pending',detail:'Controlador externo'}:data.detail;if(path==='/api/tables')return [{name:'chats',count:1,description:'Conversas'}];if(path.startsWith('/api/tables/'))return data.table;return {id:'operation'};};
 const windowListeners=new Map();
 const window={location:{hash:''},history:{pushState(_a,_b,hash){window.location.hash=hash;},replaceState(_a,_b,hash){window.location.hash=hash;}},scrollTo(_x,y){window.scrollY=y;},addEventListener:(name,fn)=>windowListeners.set(name,fn),removeEventListener:name=>windowListeners.delete(name),scrollY:0,setInterval:fn=>{timers.set(1,fn);return 1;},clearInterval:id=>timers.delete(id),fetch:async(path,options)=>({ok:true,json:async()=>structuredClone(await api(path,options))})};
 const navigator={clipboard:{writeText:async text=>{copied.push(text);}}};
 return {document,window,navigator,$,api,calls,data,timers,listeners,views,copied,windowListeners};
}
export function texts(node){return [node.textContent,...node.children.flatMap(texts)].join('\n');}
export function descendants(node){return [node,...node.children.flatMap(descendants)];}
export async function flush(){for(let n=0;n<20;n++)await Promise.resolve();}
