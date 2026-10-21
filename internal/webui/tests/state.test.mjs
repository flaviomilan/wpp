import {createTranslator} from "../assets/i18n.mjs";
import test from "node:test";
import assert from "node:assert/strict";
import {createRequests} from "../assets/requests.mjs";
import {createChats} from "../assets/chats.mjs";
import {createAPI} from "../assets/api.mjs";

function deferred(){let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};}
class Node {
 constructor(){this.value="";this.textContent="";this.children=[];this.dataset={};this.scrollTop=0;this.classList={add(){},remove(){},toggle(){}};}
 append(...children){this.children.push(...children);}
 replaceChildren(...children){this.children=children;}
 setAttribute(){}
 focus(){}
}
function fixture(api){
 const nodes=new Map();
 const $=id=>{if(!nodes.has(id))nodes.set(id,new Node());return nodes.get(id);};
 const translator=createTranslator("pt-BR");
 const ctx={number:value=>String(value),kindLabel:kind=>kind,translator,t:translator.text,bindText:(node,fn)=>{node.textContent=fn();},bindAttribute:(node,name,fn)=>node.setAttribute(name,fn()),
  model:{state:{view:"groups",chats:[{ID:"T@g.us",Name:"Tracked",Tracked:true,Count:0},{ID:"A@g.us",Name:"Available",Tracked:false,Count:0}],chat:"T@g.us",offset:0,version:0,people:[]},
   chatTab:"tracked",chatTabs:{tracked:{query:"",type:"all",chat:"T@g.us",messageQuery:"filter T",person:"",offset:0,scroll:20},available:{query:"",type:"all",chat:"A@g.us",messageQuery:"filter A",person:"",offset:0,scroll:100}},serviceState:{owner:true},timezone:"UTC"},
  document:{activeElement:null},requests:createRequests(),$,api,
  el:(_tag,text)=>{const node=new Node();node.textContent=typeof text==="function"?text():text;return node;},
  button:()=>new Node(),safely:fn=>Promise.resolve().then(fn),options:()=>{},
  groupPath:id=>"/people/"+id,openPerson(){},record(){},openMessageDetail(){}
 };
 ctx.chatByID=id=>ctx.model.state.chats.find(c=>c.ID===id);
 $("chat-filter").value="all";$("message-search").value="filter T";$("messages").scrollTop=20;
 Object.assign(ctx,createChats(ctx));
 return ctx;
}

test("rapid tab changes discard the old transition, preserving search and scroll",async()=>{
 const people=[];
 const ctx=fixture(path=>{
  if(path.startsWith("/people/")){const d=deferred();people.push(d);return d.promise;}
  return Promise.resolve({items:[],more:false});
 });
 const first=ctx.switchChatTab("available"),second=ctx.switchChatTab("tracked");
 people[1].resolve({people:[]});await second;
 people[0].resolve({people:[]});await first;
 assert.equal(ctx.model.chatTab,"tracked");
 assert.equal(ctx.model.state.chat,"T@g.us");
 assert.equal(ctx.$("message-search").value,"filter T");
 assert.equal(ctx.$("messages").scrollTop,20);
});
test("an older chat-list response cannot replace the newer one",async()=>{
 const first=deferred(),second=deferred();let calls=0;
 const ctx=fixture(()=>++calls===1?first.promise:second.promise);
 const a=ctx.loadChats(),b=ctx.loadChats();
 const latest=[{ID:"T@g.us",Name:"New",Tracked:true,Count:4}];
 second.resolve(latest);await b;
 first.resolve([{ID:"A@g.us",Name:"Old",Tracked:false,Count:0}]);await a;
 assert.deepEqual(ctx.model.state.chats,latest);
});
test("navigation invalidates independent request streams",()=>{
 const requests=createRequests(),messages=requests.begin("messages"),audio=requests.begin("audio");
 assert.ok(messages.current()&&audio.current());
 requests.begin("messages");
 assert.equal(messages.current(),false);assert.equal(audio.current(),true);
 requests.invalidate();assert.equal(audio.current(),false);
});
test("HTTP client returns safe server errors and preserves success data",async()=>{
 const api=createAPI(async()=>({ok:false,json:async()=>({error:"Storage unavailable"})}));
 await assert.rejects(()=>api("/api/service"),/Storage unavailable/);
 const read=createAPI(async()=>({ok:true,json:async()=>({owner:true})}));
 assert.deepEqual(await read("/api/service"),{owner:true});
});

test("reaction displays the emoji and original author's text together",async()=>{
 const ctx=fixture(async()=>({items:[{route:"Bruno → grupo",reply:{state:"available",source_id:"original",author:"Ana",preview:"Entrega confirmada para amanhã."},message:{Kind:"reação",Text:"👍",ReplyID:"original",Time:"2026-10-13T12:00:00Z"}}],more:false}));
 await ctx.loadMessages();
 const texts=[];function collect(node){if(node.textContent)texts.push(node.textContent);for(const child of node.children)collect(child);}
 collect(ctx.$("messages"));
 for(const text of ["Reagiu à mensagem","Ana","Entrega confirmada para amanhã.","👍"])assert.ok(texts.includes(text),"missing "+text);
});
test("a missing reaction target is explained instead of inventing original text",async()=>{
 const ctx=fixture(async()=>({items:[{route:"Bruno → grupo",reply:{state:"missing"},message:{Kind:"reação",Text:"👍",ReplyID:"missing",Time:"2026-10-13T12:00:00Z"}}],more:false}));
 await ctx.loadMessages();
 const texts=ctx.$("messages").children[0].children.map(node=>node.textContent);
 assert.ok(texts.includes("Mensagem reagida indisponível no arquivo"));assert.ok(texts.includes("👍"));
});
