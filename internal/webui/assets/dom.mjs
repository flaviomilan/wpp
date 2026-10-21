export function createDOM(document) {
 const $=id=>document.getElementById(id),bindings=new Set(),current=new WeakMap();
 function bind(node,name,render){
  let slots=current.get(node);if(!slots){slots=new Map();current.set(node,slots);}
  const binding={node:new WeakRef(node),name,render};slots.set(name,binding);bindings.add(binding);
  if(name==='textContent')node.textContent=render();else node.setAttribute(name,render());
  return node;
 }
 const bindText=(node,render)=>bind(node,'textContent',render);
 const bindAttribute=(node,name,render)=>bind(node,name,render);
 function clearTextBinding(node){current.get(node)?.delete("textContent");}
 function refreshTranslations(){
  for(const binding of bindings){
   const node=binding.node.deref();
   if(!node||current.get(node)?.get(binding.name)!==binding){bindings.delete(binding);continue;}
   if(binding.name==='textContent')node.textContent=binding.render();else node.setAttribute(binding.name,binding.render());
  }
 }
 const el=(tag,text,className)=>{
  const node=document.createElement(tag);
  if(typeof text==='function')bindText(node,text);else if(text!==undefined)node.textContent=text;
  if(className)node.className=className;
  return node;
 };
 const button=(text,action)=>{const node=el('button',text,'text-button');node.type='button';node.onclick=action;return node;};
 const showError=error=>{bindText($('notice'),()=>error.message);$('notice').classList.remove('hidden');};
 const safely=action=>Promise.resolve().then(action).catch(showError);
 function options(select,items,placeholder) {
  const key=JSON.stringify([items.map(item=>({...item,label:typeof item.label==='function'?item.label():item.label})),typeof placeholder==='function'?placeholder():placeholder]);if(select.dataset.options===key)return;
  select.dataset.options=key;select.replaceChildren();
  const add=(label,value)=>{const option=el('option',label);option.value=value;select.append(option);};
  if(placeholder)add(placeholder,'');
  items.forEach(item=>add(item.label,item.value));
 }
 function record(value){$('record-content').textContent=JSON.stringify(value,null,2);$('record-dialog').showModal();}
 return {$,el,button,bindText,bindAttribute,clearTextBinding,refreshTranslations,showError,safely,options,record};
}
