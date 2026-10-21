export function createPreferences(ctx){
 let saved='en',dirty=false,initialized=false,sessionOverride=false;
 function applyLanguage(language){
  ctx.translator.setLanguage(language);
  if(ctx.document.documentElement)ctx.document.documentElement.lang=ctx.translator.language;
  ctx.refreshTranslations();
 }
 function localizeStatic(){
  for(const node of ctx.document.querySelectorAll('[data-i18n]'))ctx.bindText(node,()=>ctx.t(node.dataset.i18n));
  for(const name of ['aria-label','placeholder','alt','title'])for(const node of ctx.document.querySelectorAll('[data-i18n-'+name+']'))ctx.bindAttribute(node,name,()=>ctx.t(node.getAttribute('data-i18n-'+name)));
 }
 localizeStatic();
 async function loadPreferences(){
  const token=ctx.requests.begin('preferences');const preferences=await ctx.api('/api/preferences');if(!token.current())return;
  saved=preferences.application_language;
  if(!initialized){initialized=true;sessionOverride=preferences.initial_language!==saved;applyLanguage(preferences.initial_language||saved);}
  else if(!dirty&&!sessionOverride)applyLanguage(saved);
  if(!dirty)ctx.$('application-language').value=saved;
 }
 function discardLanguageDraft(){dirty=false;ctx.$('application-language').value=saved;ctx.bindText(ctx.$('language-feedback'),()=>'');}
 ctx.$('application-language').onchange=()=>{dirty=ctx.$('application-language').value!==saved;ctx.bindText(ctx.$('language-feedback'),()=>dirty?ctx.t('ui.unsaved_changes'):'');};
 ctx.$('language-form').onsubmit=event=>{event.preventDefault();return ctx.safely(async()=>{
  const returnFocus=ctx.document.activeElement;ctx.requests.begin('preferences');ctx.$('save-language').disabled=true;
  try{
   const result=await ctx.api('/api/preferences',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({application_language:ctx.$('application-language').value})});
   saved=result.application_language;dirty=false;sessionOverride=false;applyLanguage(saved);ctx.bindText(ctx.$('language-feedback'),()=>ctx.t('language.saved'));
  }catch(error){ctx.bindText(ctx.$('language-feedback'),()=>error.message);throw error;}
  finally{ctx.$('save-language').disabled=false;if(ctx.document.activeElement===ctx.document.body&&returnFocus?.isConnected)returnFocus.focus({preventScroll:true});}
 });};
 return {loadPreferences,applyLanguage,preferencesChanged:()=>dirty,discardLanguageDraft};
}
