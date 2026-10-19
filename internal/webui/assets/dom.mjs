export function createDOM(document) {
 const $=id=>document.getElementById(id);
 const el=(tag,text,className)=>{
  const node=document.createElement(tag);
  if(text!==undefined)node.textContent=text;
  if(className)node.className=className;
  return node;
 };
 const button=(text,action)=>{const node=el("button",text,"text-button");node.type="button";node.onclick=action;return node;};
 const showError=error=>{$("notice").textContent=error.message;$("notice").classList.remove("hidden");};
 const safely=action=>Promise.resolve().then(action).catch(showError);
 function options(select,items,placeholder) {
  const key=JSON.stringify([items,placeholder]);if(select.dataset.options===key)return;
  select.dataset.options=key;select.replaceChildren();
  const add=(label,value)=>{const option=el("option",label);option.value=value;select.append(option);};
  if(placeholder)add(placeholder,"");
  items.forEach(item=>add(item.label,item.value));
 }
 function record(value){$("record-content").textContent=JSON.stringify(value,null,2);$("record-dialog").showModal();}
 return {$,el,button,showError,safely,options,record};
}
