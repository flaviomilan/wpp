import {catalogs} from "./messages.mjs";
export {catalogs};
export function createTranslator(language="en"){
 let current=Object.hasOwn(catalogs,language)?language:"en";
 return {get language(){return current;},setLanguage(value){current=Object.hasOwn(catalogs,value)?value:"en";},text(key,parameters={}){
  let value=catalogs[current][key]||catalogs.en[key]||catalogs.en["error.generic"];
  value=value.replace(/\{([^{}]+)\}/g,(match,name)=>Object.hasOwn(parameters,name)?String(parameters[name]):match);
  return value;
 }};
}
