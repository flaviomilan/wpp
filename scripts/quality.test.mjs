import test from "node:test";
import assert from "node:assert/strict";
import {mkdtempSync,writeFileSync,mkdirSync,rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join,resolve} from "node:path";
import {cleanDiagnostics,goCoverage,jsCoverage,run,sourceFiles,check,profileSummary,goPackages,cliCoverage} from "./quality.mjs";

test("diagnostic text fails even after successful exit",()=>{
  cleanDiagnostics("tool"," \n");
  assert.throws(()=>cleanDiagnostics("tool","unreachable func: unused"),/unreachable/);
  assert.equal(run(process.execPath,["-e","process.stdout.write('ok')"]),"ok");
  assert.equal(run(process.execPath,["-e","process.stderr.write('expected');process.exit(1)"],{expectedStatus:1,includeStderr:true}),"expected");
  assert.throws(()=>run(process.execPath,["-e","process.exit(2)"]),/failed/);
  assert.throws(()=>run("/nonexistent-wpp-quality-executable",[]),/ENOENT/);
});
test("Go gate rejects untested functions, low totals and omitted files",()=>{
  const profile="mode: atomic\nwpp/a.go:1.1,2.1 9 1\nwpp/a.go:3.1,4.1 1 0\n";
  const covered="wpp/a.go:1: f 100.0%\ntotal: (statements) 90.0%";
  assert.equal(goCoverage(covered,profile,["wpp/a.go"]),90);
  assert.throws(()=>goCoverage(covered,profile,["wpp/b.go"]),/Omitted/);
  assert.throws(()=>goCoverage(covered.replace("f 100.0","f 0.0"),profile,[]),/Untested/);
  assert.throws(()=>goCoverage(covered.replace("90.0","89.9"),profile,[]),/below/);
  assert.throws(()=>goCoverage("invalid",profile,[]),/below/);
});
test("Go statement inventory merges duplicate blocks and does not round up the threshold",()=>{
 const profile="mode: atomic\nwpp/a.go:1.1,2.1 9 0\nwpp/a.go:1.1,2.1 9 1\nwpp/a.go:3.1,4.1 1 0\n";
 const summary=profileSummary(profile);assert.equal(summary.total,10);assert.equal(summary.covered,9);assert.equal(summary.percent,90);
 assert.throws(()=>profileSummary(profile+"wpp/a.go:1.1,2.1 8 1\n"),/Incompatible/);
 assert.throws(()=>profileSummary("mode: atomic\ninvalid\n"),/Invalid/);
 assert.equal(profileSummary("mode: atomic\n").percent,0);
 assert.throws(()=>goCoverage("total: (statements) 90.0%","mode: atomic\nwpp/a.go:1.1,2.1 8996 1\nwpp/a.go:3.1,4.1 1004 0\n",[]),/below/);
});
test("JS gate checks all modules and every function",()=>{
  const file=resolve("fixture.mjs");
  const result={functions:{covered:2,total:2},lines:{covered:9,total:10,pct:90}};
  const summary={total:result,[file]:result};
  assert.equal(jsCoverage(summary,[file]),90);
  assert.throws(()=>jsCoverage(summary,["missing.mjs"]),/Omitted/);
  assert.throws(()=>jsCoverage({...summary,[file]:{...result,functions:{covered:1,total:2}}},[file]),/Untested/);
  assert.throws(()=>jsCoverage({...summary,[file]:{...result,lines:{covered:0,total:10}}},[file]),/not executed/);
  assert.throws(()=>jsCoverage({...summary,total:{lines:{pct:89.9}}},[file]),/below/);
  assert.throws(()=>jsCoverage({[file]:result},[file]),/below/);
});
test("source inventory includes nested production modules and excludes tests",()=>{
  const dir=mkdtempSync(join(tmpdir(),"wpp-quality-"));
  try{
    mkdirSync(join(dir,"nested"));
    writeFileSync(join(dir,"nested","module.mjs"),"");
    writeFileSync(join(dir,"module.test.mjs"),"");
    writeFileSync(join(dir,"readme.md"),"");
    assert.deepEqual(sourceFiles(dir,[".mjs"]),[join(dir,"nested","module.mjs")]);
  }finally{rmSync(dir,{recursive:true,force:true});}
  assert.throws(()=>check("invalid"),/Unknown/);
});
test("Go package inventory excludes only npm dependencies, never own packages",()=>{
 assert.deepEqual(goPackages('go',()=>"wpp/cmd/wpp\nwpp/internal/archive\nwpp/new\nwpp/node_modules/flatted/golang/pkg/flatted\n"),['wpp/cmd/wpp','wpp/internal/archive','wpp/new']);
 assert.throws(()=>goPackages('go',()=>''),/No first-party Go packages/);
 assert.throws(()=>cliCoverage('go',()=>''),/No first-party Go packages/);
});

test("check commands inventory all Go files and validate complete reports",()=>{
 const dir=mkdtempSync(join(tmpdir(),"wpp-check-")),old=process.cwd();
 try{
  for(const folder of ['cmd','internal','scripts','internal/webui/assets','coverage'])mkdirSync(join(dir,folder),{recursive:true});
  writeFileSync(join(dir,'go.mod'),'module wpp\n');
  writeFileSync(join(dir,'cmd','main.go'),'package main\nfunc main() {}\n');
  writeFileSync(join(dir,'internal','model.go'),'package internal\n');
  writeFileSync(join(dir,'cmd','main_test.go'),'package main\n');
  writeFileSync(join(dir,'scripts','quality.mjs'),'export function check(){}');
  writeFileSync(join(dir,'internal','webui','assets','app.js'),'console.log("app")');
  writeFileSync(join(dir,'coverage','go.out'),'mode: atomic\nwpp/cmd/main.go:2.1,2.15 1 1\n');
  const result={functions:{covered:1,total:1},lines:{total:1,covered:1,pct:100}};
  writeFileSync(join(dir,'coverage','coverage-summary.json'),JSON.stringify({total:result,[join(dir,'scripts','quality.mjs')]:result,[join(dir,'internal','webui','assets','app.js')]:result}));
  process.chdir(dir);
  const calls=[];const runner=(command,args,options)=>{
    calls.push({command,args});
    if(args[0]==='list')return 'wpp/cmd\nwpp/internal\nwpp/node_modules/dependency\n';
    if(options?.expectedStatus===1)return 'Error: unknown command';
    if(args.includes('covdata')){writeFileSync(args.find(arg=>arg.startsWith('-o=')).slice(3),'mode: atomic\nwpp/cmd/main.go:2.1,2.15 1 1\n');return '';}
    return args[0]==='tool'?'wpp/cmd/main.go:2: main 100.0%\ntotal: (statements) 100.0%':' ';
  };
  const reports=[],report=message=>reports.push(message);
  check('format',runner,report);check('diagnostics',runner,report);check('lint',runner,report);check('go',runner,report);check('js');check('modules');
  assert.ok(reports.includes('Consolidated Go: 100.00%'));
  assert.ok(calls.some(call=>call.args.includes('-test=false')));
  assert.ok(calls.some(call=>call.args.includes('-coverpkg=wpp/cmd,wpp/internal')));
  assert.ok(calls.some(call=>call.args.includes(join(dir,'cmd','main_test.go'))));
 }finally{process.chdir(old);rmSync(dir,{recursive:true,force:true});}
 assert.throws(()=>run(process.execPath,['scripts/quality.mjs','invalid']),/Unknown/);
});
