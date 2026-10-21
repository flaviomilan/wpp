import {createTranslator} from './i18n.mjs';
export function createAPI(fetcher,t=createTranslator().text) {
 return async function api(path,options) {
  const response=await fetcher(path,options);
  const body=await response.json();
  if(!response.ok){const error=new Error();Object.defineProperty(error,'message',{get:()=>body.code?t(body.code,body.parameters||{}):body.error||t('ui.request_failed')});throw error;}
  return body;
 };
}
