export function createPeople(ctx) {

async function loadPeople() {
 const token=ctx.requests.begin("people");
 const group=ctx.$("people-group").value;ctx.model.state.people=[];ctx.$("people-rows").replaceChildren();ctx.$("add-person").disabled=!group;
 if(!group){ctx.$("people-rows").append(ctx.el("tr",()=>ctx.t("ui.no_known_groups")));return;}
 const people=(await ctx.api(ctx.groupPath(group))).people||[];
 if(!token.current()||ctx.$("people-group").value!==group||ctx.model.state.view!=="people")return;ctx.model.state.people=people;renderPeople();
}

function renderPeople() {
 const query=ctx.$("people-search").value.toLowerCase(),group=ctx.$("people-group").value;ctx.$("people-rows").replaceChildren();
 const people=ctx.model.state.people.filter(p=>(p.id+" "+p.name+" "+(p.detected_name||"")).toLowerCase().includes(query));
 people.forEach(p=>{
  const row=ctx.el("tr"),identity=ctx.el("td");identity.append(ctx.el("strong",()=>p.name||p.detected_name||ctx.t("ui.unnamed")),ctx.el("small",()=>p.id));
  const action=ctx.el("td");action.append(ctx.button(()=>ctx.t("ui.edit_name"),()=>openPerson(group,p.id,p.name)));
  row.append(identity,ctx.el("td",()=>p.detected_name||ctx.t("ui.not_received")),ctx.el("td",()=>p.name||ctx.t("ui.not_registered")),action);ctx.$("people-rows").append(row);
 });
 if(!people.length){const row=ctx.el("tr"),cell=ctx.el("td",()=>ctx.t("ui.no_people_found"));cell.colSpan=4;row.append(cell);ctx.$("people-rows").append(row);}
}

function openPerson(group,id,name) {
 ctx.$("person-form").dataset.group=group;ctx.$("person-id").value=id||"";ctx.$("person-id").readOnly=!!id;
 ctx.$("person-name").value=name===undefined?(ctx.model.state.people.find(p=>p.id===id)||{}).name||"":name;
 ctx.bindText(ctx.$("person-error"),()=>"");ctx.$("person-dialog").showModal();(id?ctx.$("person-name"):ctx.$("person-id")).focus();
}

async function savePerson(event) {
 event.preventDefault();ctx.$("save-person").disabled=true;ctx.bindText(ctx.$("person-error"),()=>"");
 try {
  await ctx.api(ctx.groupPath(ctx.$("person-form").dataset.group),{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({id:ctx.$("person-id").value.trim(),name:ctx.$("person-name").value.trim()})});
  ctx.$("person-dialog").close();
  if(ctx.model.state.view==="people")await loadPeople();else await ctx.selectChat(ctx.model.state.chat);
 }catch(error){ctx.bindText(ctx.$("person-error"),()=>error.message);}finally{ctx.$("save-person").disabled=false;}
}
ctx.$("people-group").onchange=()=>ctx.safely(loadPeople);
ctx.$("open-people").onclick=()=>{ctx.$("people-group").value=ctx.model.state.chat;ctx.changeView("people");};
ctx.$("add-person").onclick=()=>openPerson(ctx.$("people-group").value);
ctx.$("person-form").onsubmit=savePerson;
["close-person","cancel-person"].forEach(id=>ctx.$(id).onclick=()=>ctx.$("person-dialog").close());
ctx.$("people-search").oninput=renderPeople;
return {loadPeople,renderPeople,openPerson,savePerson};
}
