import test from 'node:test';
import assert from 'node:assert/strict';
import {catalogs,createTranslator} from '../assets/i18n.mjs';

test('catalog keys, parameters, explicit languages, and safe fallbacks',()=>{
 assert.deepEqual(Object.keys(catalogs.en).sort(),Object.keys(catalogs['pt-BR']).sort());
 for(const key of Object.keys(catalogs.en))assert.deepEqual((catalogs.en[key].match(/\{[^}]+\}/g)||[]).sort(),(catalogs['pt-BR'][key].match(/\{[^}]+\}/g)||[]).sort());
 const t=createTranslator();assert.equal(t.language,'en');assert.equal(t.text('count.other',{count:2}),'2 items');
 t.setLanguage('pt-BR');assert.equal(t.text('count.other',{count:2}),'2 itens');assert.equal(t.text('unknown'),catalogs.en['error.generic']);
 const saved=catalogs['pt-BR']['language.saved'];delete catalogs['pt-BR']['language.saved'];
 try{assert.equal(t.text('language.saved'),catalogs.en['language.saved']);}finally{catalogs['pt-BR']['language.saved']=saved;}
 t.setLanguage('invalid');assert.equal(t.language,'en');assert.equal(createTranslator('invalid').language,'en');
});

test('browser and Go public message catalogs have identical shared translations',async()=>{
 const {readdir,readFile}=await import('node:fs/promises');
 const directory=new URL('../../localization/',import.meta.url);
 for(const file of (await readdir(directory)).filter(name=>name.endsWith('.json'))){
  const messages=JSON.parse(await readFile(new URL(file,directory),'utf8'));
  for(const [key,languages] of Object.entries(messages))for(const [language,value] of Object.entries(languages))assert.equal(catalogs[language][key],value,key);
 }
});
