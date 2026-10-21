export function createActivity(ctx) {
let lastOperations="",lastOperationStatus="";
function detail(op){return op.detail_code?ctx.t(op.detail_code,op.detail_parameters||{}):ctx.t(({queued:"ui.queued",running:"ui.in_progress",succeeded:"ui.completed",warning:"ui.outstanding_items",failed:"ui.failed",cancelled:"ui.cancelled",interrupted:"ui.interrupted"})[op.state]||"error.generic");}
function renderActivity(operations){
 const active=operations.find(op=>["queued","running"].includes(op.state));
 const statusKey=JSON.stringify(active?[active.id,active.phase,active.detail]:[operations[0]?.detail]);
 if(statusKey!==lastOperationStatus){lastOperationStatus=statusKey;ctx.clearTextBinding(ctx.$("operation-status"));ctx.$("operation-status").replaceChildren();
 if(active){ctx.$("operation-status").append(ctx.el("span",()=>"","spinner"),ctx.el("span",()=>detail(active)),ctx.button(()=>ctx.t("ui.cancel"),()=>ctx.safely(()=>ctx.control("/api/operations/"+encodeURIComponent(active.id)+"/cancel"))));}
 else ctx.bindText(ctx.$("operation-status"),()=>operations[0]?detail(operations[0]):ctx.t("ui.ready_to_explore_your_archive"));}
 const opsJSON=JSON.stringify(operations);
 if(opsJSON!==lastOperations){lastOperations=opsJSON;ctx.$("recent-operations").replaceChildren();operations.slice(0,10).forEach(op=>{
  const node=ctx.el("div",undefined,"operation-item"),states={get queued(){return ctx.t("ui.queued");},get running(){return ctx.t("ui.in_progress");},get succeeded(){return ctx.t("ui.completed");},get warning(){return ctx.t("ui.outstanding_items");},get failed(){return ctx.t("ui.failed");},get cancelled(){return ctx.t("ui.cancelled");},get interrupted(){return ctx.t("ui.interrupted");}};
  node.append(ctx.el("strong",()=>states[op.state]||op.state),ctx.el("p",()=>detail(op)),ctx.el("small",()=>new Date(op.updated).toLocaleString(ctx.translator.language,{timeZone:ctx.model.timezone})));
  if(["queued","running"].includes(op.state))node.append(ctx.button(()=>ctx.t("ui.cancel"),()=>ctx.safely(()=>ctx.control("/api/operations/"+encodeURIComponent(op.id)+"/cancel"))));
  ctx.$("recent-operations").append(node);
 });if(!operations.length)ctx.$("recent-operations").append(ctx.el("div",()=>ctx.t("ui.no_operations_in_this_archive_yet"),"empty-state"));}
}
return {renderActivity};
}
