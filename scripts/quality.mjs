import {spawnSync} from "node:child_process";
import {readdirSync, readFileSync, mkdirSync, mkdtempSync, rmSync, appendFileSync, writeFileSync} from "node:fs";
import {resolve, relative, join, dirname} from "node:path";
import {tmpdir} from "node:os";
import {fileURLToPath} from "node:url";
import {moduleUsage} from "./module-usage.mjs";

export function run(command, args, options={}) {
  const {expectedStatus=0,includeStderr=false,...spawnOptions}=options;
  const result=spawnSync(command,args,{encoding:"utf8",maxBuffer:32*1024*1024,...spawnOptions});
  if(result.error)throw result.error;
  if(result.status!==expectedStatus)throw new Error(`${command} falhou (${result.status}):\n${result.stdout}${result.stderr}`);
  return result.stdout+(includeStderr?result.stderr:"");
}

export function goPackages(go, runner=run) {
  const packages=runner(go,["list","-buildvcs=false","./..."]).trim().split(/\s+/).filter(name=>name&&!name.includes("/node_modules/"));
  if(!packages.length)throw new Error("Nenhum pacote Go próprio encontrado");
  return packages;
}

export function cliCoverage(go, runner=run, packages=goPackages(go,runner)) {
  const dir=mkdtempSync(join(tmpdir(),"wpp-cli-coverage-"));
  try{
    const binary=join(dir,"wpp"),counters=join(dir,"counters"),profile=join(dir,"cli.out");
    mkdirSync(counters);
    runner(go,["build","-buildvcs=false","-coverpkg="+packages.join(","),"-covermode=atomic","-o",binary,"./cmd/wpp"]);
    const options={env:{...process.env,GOCOVERDIR:counters}};
    runner(binary,["--data-dir",join(dir,"data"),"audio","status"],options);
    const failure=runner(binary,["unknown"],{...options,expectedStatus:1,includeStderr:true});
    if(!failure.includes("Erro: comando desconhecido"))throw new Error("A CLI não informou o comando inválido");
    runner(go,["tool","covdata","textfmt","-i="+counters,"-o="+profile]);
    // Go's profile parser merges identical blocks; CLI subprocesses therefore
    // contribute without double-counting the statement denominator.
    appendFileSync("coverage/go.out",readFileSync(profile,"utf8").split("\n").slice(1).join("\n"));
  }finally{rmSync(dir,{recursive:true,force:true});}
}

export function cleanDiagnostics(label, output) {
  if(output.trim())throw new Error(`${label}:\n${output}`);
}

export function profileSummary(profile) {
  const blocks=new Map();
  for(const line of profile.trim().split("\n").slice(1)){
    const match=line.match(/^(.+):(\d+\.\d+,\d+\.\d+) (\d+) (\d+)$/);
    if(!match)throw new Error("Perfil Go inválido: "+line);
    const [,file,range,statements,hits]=match,key=file+":"+range;
    const previous=blocks.get(key);
    if(previous&&previous.total!==Number(statements))throw new Error("Bloco Go incompatível: "+key);
    blocks.set(key,{file,total:Number(statements),covered:previous?.covered||Number(hits)>0});
  }
  const packages={};let covered=0,total=0;
  for(const block of blocks.values()){
    const name=dirname(block.file),item=packages[name]??={covered:0,total:0};
    item.total+=block.total;total+=block.total;
    if(block.covered){item.covered+=block.total;covered+=block.total;}
  }
  return {covered,total,percent:total?100*covered/total:0,packages};
}

export function goCoverage(functions, profile, expectedFiles, minimum=90) {
  const missing=expectedFiles.filter(file=>!profile.includes(file+":"));
  if(missing.length)throw new Error("Arquivos Go omitidos: "+missing.join(", "));
  const rows=functions.trim().split("\n");
  const zero=rows.filter(row=>!row.startsWith("total:")&&/\s0\.0%$/.test(row));
  if(zero.length)throw new Error("Funções Go sem teste:\n"+zero.join("\n"));
  const total=Number(rows.find(row=>row.startsWith("total:"))?.match(/([\d.]+)%$/)?.[1]);
  const actual=profileSummary(profile).percent;
  if(!Number.isFinite(total)||total<minimum||actual<minimum)throw new Error(`Cobertura Go ${actual}% (relatório ${total}%) abaixo de ${minimum}%`);
  return actual;
}

export function jsCoverage(summary, expectedFiles, minimum=90) {
  const files=Object.keys(summary).filter(file=>file!=="total");
  const missing=expectedFiles.filter(file=>!files.some(entry=>resolve(entry)===resolve(file)));
  if(missing.length)throw new Error("Arquivos JavaScript omitidos: "+missing.join(", "));
  for(const file of files){
    const result=summary[file];
    if(result.functions.covered!==result.functions.total)throw new Error("Funções JavaScript sem teste: "+file);
    if(result.lines.total>0&&result.lines.covered===0)throw new Error("JavaScript não executado: "+file);
  }
  const total=100*summary.total?.lines.covered/summary.total?.lines.total;
  if(!Number.isFinite(total)||total<minimum)throw new Error(`Cobertura JavaScript ${total}% abaixo de ${minimum}%`);
  return total;
}

export function sourceFiles(directory, extensions) {
  return readdirSync(directory,{withFileTypes:true}).flatMap(entry=>{
    const path=resolve(directory,entry.name);
    if(entry.isDirectory())return sourceFiles(path,extensions);
    return extensions.some(extension=>entry.name.endsWith(extension))&&!entry.name.endsWith(".test.mjs")?[path]:[];
  });
}

export function check(mode, runner=run, report=console.log) {
  const go=process.env.GO||"go";
  const files=sourceFiles("cmd",[".go"]).concat(sourceFiles("internal",[".go"])).filter(file=>!file.endsWith("_test.go"));
  if(mode==="format"){
    cleanDiagnostics("gofmt",runner(process.env.GOFMT||go.replace(/go$/, "gofmt"),["-l",...files,...sourceFiles("cmd",["_test.go"]),...sourceFiles("internal",["_test.go"])]));
  }else if(mode==="diagnostics"){
    cleanDiagnostics("gopls",runner(process.env.GOPLS||".tools/bin/gopls",["check",...files,...sourceFiles("cmd",["_test.go"]),...sourceFiles("internal",["_test.go"])],{includeStderr:true}));
    cleanDiagnostics("deadcode",runner(process.env.DEADCODE||".tools/bin/deadcode",["-test=false","./cmd/wpp"],{includeStderr:true}));
  }else if(mode==="lint"){
    const module=readFileSync("go.mod","utf8").match(/^module\s+(\S+)/m)[1];
    const paths=goPackages(go,runner).map(name=>name===module?".":"./"+name.slice(module.length+1));
    report(runner(process.env.GOLANGCI_LINT||".tools/bin/golangci-lint",["run",...paths]));
  }else if(mode==="go"){
    mkdirSync("coverage",{recursive:true});
    const packages=goPackages(go,runner);
    report(runner(go,["test","-race","-coverpkg="+packages.join(","),"-covermode=atomic","-coverprofile=coverage/go.out",...packages]));
    cliCoverage(go,runner,packages);
    const functions=runner(go,["tool","cover","-func=coverage/go.out"]);
    writeFileSync("coverage/go-functions.txt",functions);
    const profile=readFileSync("coverage/go.out","utf8"),summary=profileSummary(profile);
    writeFileSync("coverage/go-summary.json",JSON.stringify(summary,null,2));
    for(const [name,result] of Object.entries(summary.packages))report(`${name}: ${(100*result.covered/result.total).toFixed(1)}% (${result.covered}/${result.total})`);
    report(`Go consolidado: ${summary.percent.toFixed(2)}%`);
    const expected=files.filter(file=>/^func\s/m.test(readFileSync(file,"utf8"))).map(file=>"wpp/"+relative(process.cwd(),file));
    goCoverage(functions,profile,expected);
  }else if(mode==="modules"){
    const sources=Object.fromEntries(sourceFiles("internal/webui/assets",[".js",".mjs"]).map(file=>[file,readFileSync(file,"utf8")]));
    moduleUsage(sources,resolve("internal/webui/assets/app.js"));
  }else if(mode==="js"){
    const expected=sourceFiles("internal/webui/assets",[".js",".mjs"]).concat(sourceFiles("scripts",[".mjs"]));
    jsCoverage(JSON.parse(readFileSync("coverage/coverage-summary.json","utf8")),expected);
  }else throw new Error("Verificação desconhecida: "+mode);
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){
  try{check(process.argv[2]);}catch(error){console.error(error.message);process.exitCode=1;}
}
