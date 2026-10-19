export function createActivity(ctx) {
let lastOperations="",lastOperationStatus="";
function renderActivity(operations){
 const active=operations.find(op=>["queued","running"].includes(op.state));
 const statusKey=JSON.stringify(active?[active.id,active.phase,active.detail]:[operations[0]?.detail]);
 if(statusKey!==lastOperationStatus){lastOperationStatus=statusKey;ctx.$("operation-status").replaceChildren();
 if(active){ctx.$("operation-status").append(ctx.el("span","","spinner"),ctx.el("span",active.detail),ctx.button("Cancelar",()=>ctx.safely(()=>ctx.control("/api/operations/"+encodeURIComponent(active.id)+"/cancel"))));}
 else ctx.$("operation-status").textContent=operations[0]?.detail||"Pronto para consultar seu arquivo.";}
 const opsJSON=JSON.stringify(operations);
 if(opsJSON!==lastOperations){lastOperations=opsJSON;ctx.$("recent-operations").replaceChildren();operations.slice(0,10).forEach(op=>{
  const node=ctx.el("div",undefined,"operation-item"),states={queued:"Na fila",running:"Em andamento",succeeded:"Concluída",warning:"Com pendências",failed:"Falhou",cancelled:"Cancelada",interrupted:"Interrompida"};
  node.append(ctx.el("strong",states[op.state]||op.state),ctx.el("p",op.detail),ctx.el("small",new Date(op.updated).toLocaleString("pt-BR",{timeZone:ctx.model.timezone})));
  if(["queued","running"].includes(op.state))node.append(ctx.button("Cancelar",()=>ctx.safely(()=>ctx.control("/api/operations/"+encodeURIComponent(op.id)+"/cancel"))));
  ctx.$("recent-operations").append(node);
 });if(!operations.length)ctx.$("recent-operations").append(ctx.el("div","Ainda não há operações neste arquivo.","empty-state"));}
}
return {renderActivity};
}
