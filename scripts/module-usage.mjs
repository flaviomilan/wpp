import {Linter} from "eslint";
import {resolve, dirname} from "node:path";

// Analyze only the browser entry graph: test imports must not keep public APIs alive.
export function moduleInfo(source) {
  const info={imports:[],exports:[]};
  const linter=new Linter();
  const messages=linter.verify(source,[{
    languageOptions:{ecmaVersion:"latest",sourceType:"module"},
    plugins:{inventory:{rules:{collect:{create(context){return {
      ImportDeclaration(node){
        info.imports.push({path:node.source.value,names:node.specifiers.map(s=>s.type==="ImportSpecifier"?s.imported.name:s.type==="ImportDefaultSpecifier"?"default":"*")});
      },
      ExportNamedDeclaration(node){
        if(node.source){info.imports.push({path:node.source.value,names:node.specifiers.map(s=>s.local.name)});}
        const variables=node.declaration?context.sourceCode.getDeclaredVariables(node.declaration):[];
        const names=node.declaration?variables.filter(v=>!node.declaration.id||v.name===node.declaration.id.name).map(v=>({name:v.name,local:v.name})):node.specifiers.map(s=>({name:s.exported.name,local:s.local.name}));
        info.exports.push(...names.map(name=>({...name,range:node.range})));
      },
      ExportDefaultDeclaration(node){info.exports.push({name:"default",local:node.declaration.id?.name,range:node.range});},
      "Program:exit"(){
        const variables=context.sourceCode.scopeManager.scopes.filter(scope=>scope.type==="module").flatMap(scope=>scope.variables);
        for(const item of info.exports){item.localRead=variables.some(v=>v.name===item.local&&v.references.some(ref=>ref.isRead()&&(ref.identifier.range[0]<item.range[0]||ref.identifier.range[0]>=item.range[1])));}
      }
    };}}}}},
    rules:{"inventory/collect":"error"}
  }]);
  if(messages.length)throw new Error("JavaScript inválido: "+messages.map(message=>message.message).join("; "));
  return info;
}

export function moduleUsage(sources, entry) {
  const modules=new Map(Object.entries(sources).map(([path,source])=>[resolve(path),moduleInfo(source)]));
  const reachable=new Set(),used=new Map();
  function visit(path){
    if(reachable.has(path))return;
    const module=modules.get(path);if(!module)throw new Error("Módulo importado ausente: "+path);
    reachable.add(path);
    for(const item of module.imports){
      if(!item.path.startsWith("."))throw new Error("O painel deve usar imports locais: "+item.path);
      const target=resolve(dirname(path),item.path);
      used.set(target,new Set([...(used.get(target)||[]),...item.names]));
      visit(target);
    }
  }
  visit(resolve(entry));
  for(const [path,module] of modules){
    if(!reachable.has(path))throw new Error("Módulo sem uso pelo painel: "+path);
    for(const item of module.exports){
      if(!item.localRead&&!used.get(path)?.has(item.name)&&!used.get(path)?.has("*"))throw new Error("Export sem uso pelo painel: "+path+" · "+item.name);
    }
  }
}
