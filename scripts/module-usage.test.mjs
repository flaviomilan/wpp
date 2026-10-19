import test from "node:test";
import assert from "node:assert/strict";
import {moduleInfo,moduleUsage} from "./module-usage.mjs";

test("module graph treats only application imports and local reads as usage",()=>{
  moduleUsage({"app.js":"import {create} from './view.mjs'; create();","view.mjs":"export function create() {}"},"app.js");
  moduleUsage({"app.js":"export function boot() {} boot();"},"app.js");
  moduleUsage({"app.js":"import boot from './view.mjs'; boot();","view.mjs":"export default function boot() {}"},"app.js");
  moduleUsage({"app.js":"import * as view from './view.mjs'; view.boot();","view.mjs":"export function boot() {}"},"app.js");
  moduleUsage({"app.js":"import './view.mjs';","view.mjs":"console.log('ready');"},"app.js");
  moduleUsage({"app.js":"import {boot} from './bridge.mjs'; boot();","bridge.mjs":"export {boot} from './view.mjs';","view.mjs":"export function boot(){}"},"app.js");
});
test("module graph rejects unreachable modules and exports retained only by tests",()=>{
  assert.throws(()=>moduleUsage({"app.js":"console.log('ready');","unused.mjs":"export function unused(){}"},"app.js"),/Módulo sem uso/);
  assert.throws(()=>moduleUsage({"app.js":"export function unused(){}"},"app.js"),/Export sem uso/);
  assert.throws(()=>moduleUsage({"app.js":"export function recursive(){recursive();}"},"app.js"),/Export sem uso/);
  assert.throws(()=>moduleUsage({"app.js":"import './missing.mjs';"},"app.js"),/ausente/);
  assert.throws(()=>moduleUsage({"app.js":"import x from 'npm'; x();"},"app.js"),/imports locais/);
  assert.throws(()=>moduleInfo('export function {'),/inválido/);
});
test("named variable exports and aliases expose only the declared names",()=>{
  const info=moduleInfo('const value=1; export {value as renamed}; export const other=2; console.log(other);');
  assert.deepEqual(info.exports.map(item=>[item.name,item.localRead]),[['renamed',false],['other',true]]);
  moduleUsage({'app.js':'import {renamed} from "./values.mjs"; console.log(renamed);','values.mjs':'const value=1;export {value as renamed};'},'app.js');
});
