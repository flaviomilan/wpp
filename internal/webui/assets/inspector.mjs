export function createInspector(ctx) {
async function loadTables() {
 const token=ctx.requests.begin("tables");const selected=ctx.$("table-select").value;
 const tables=await ctx.api("/api/tables");if(!token.current())return;ctx.model.state.tables=tables;
 ctx.options(ctx.$("table-select"),ctx.model.state.tables.map(t=>({value:t.name,label:t.name+" · "+t.count+" registros"})));
 if(ctx.model.state.tables.some(t=>t.name===selected))ctx.$("table-select").value=selected;
 await loadTable();
}

async function loadTable() {
 const name=ctx.$("table-select").value;if(!name)return;const token=ctx.requests.begin("table");
 const table=ctx.model.state.tables.find(t=>t.name===name);ctx.$("table-description").textContent=table?table.description:"";
 const page=await ctx.api("/api/tables/"+encodeURIComponent(name)+"?offset="+ctx.model.state.tableOffset+"&limit=50");
 if(!token.current())return;ctx.model.state.tableMore=page.more;
 const heading=ctx.el("tr");page.columns.forEach(c=>heading.append(ctx.el("th",c)));ctx.$("table-head").replaceChildren(heading);ctx.$("table-rows").replaceChildren();
 page.rows.forEach(values=>{
  const row=ctx.el("tr");values.forEach(value=>{const cell=ctx.el("td"),preview=ctx.el("button",value===null?"NULL":String(value),"text-button cell-preview");preview.onclick=()=>ctx.record(Object.fromEntries(page.columns.map((c,i)=>[c,values[i]])));cell.append(preview);row.append(cell);});ctx.$("table-rows").append(row);
 });
 if(!page.rows.length){const row=ctx.el("tr"),cell=ctx.el("td","Tabela sem registros.");cell.colSpan=page.columns.length;row.append(cell);ctx.$("table-rows").append(row);}
 ctx.$("table-prev").disabled=ctx.model.state.tableOffset===0;ctx.$("table-next").disabled=!page.more;ctx.$("table-page").textContent="Página "+(ctx.model.state.tableOffset/50+1);
}

ctx.$("table-select").onchange=()=>{ctx.model.state.tableOffset=0;ctx.safely(loadTable);};
ctx.$("table-prev").onclick=()=>{ctx.model.state.tableOffset=Math.max(0,ctx.model.state.tableOffset-50);ctx.safely(loadTable);};
ctx.$("table-next").onclick=()=>{ctx.model.state.tableOffset+=50;ctx.safely(loadTable);};
ctx.$("inspector-details").ontoggle=()=>{if(ctx.$("inspector-details").open)ctx.safely(loadTables);};
return {loadTables,loadTable};
}
