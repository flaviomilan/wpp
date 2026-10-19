import {test,expect} from '@playwright/test';
import {intercept} from './demo.mjs';

test('assets reais abrem com dados fictícios nos dois temas',async({page},testInfo)=>{
 const errors=[];page.on('pageerror',error=>errors.push(error.message));
 await intercept(page);await page.goto('/');
 await expect(page.locator('#conversation-title')).toHaveText('Equipe Aurora');
 for(const theme of ['light','dark']){
  await page.emulateMedia({colorScheme:theme});
  await page.screenshot({path:testInfo.outputPath(`painel-${theme}.png`),fullPage:true});
 }
 expect(errors).toEqual([]);
});

async function area(page,name){
 if(await page.locator('#menu-toggle').isVisible())await page.locator('#menu-toggle').click();
 await page.locator(`[data-view="${name}"]`).click();
}
async function fits(page){
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth)).toBe(true);
}
for(const width of [360,768,1440])test(`navegação, leitura e categorias cabem em ${width}px`,async({page},testInfo)=>{
 await page.setViewportSize({width,height:900});await intercept(page);await page.goto('/');
 await expect(page.locator('#conversation-title')).toHaveText('Equipe Aurora');
 await fits(page);
 if(width===360){await page.locator('#back-to-list').click();await expect(page.locator('#chat-list')).toBeVisible();await page.locator('.chat-item').first().click();}
 await area(page,'settings');
 for(const section of ['account','capture','transcription','data','advanced']){
  await page.locator('#category-'+section).click();await expect(page.locator('#settings-'+section)).toBeVisible();await fits(page);
 }
 await page.locator('#category-transcription').click();await page.screenshot({path:testInfo.outputPath(`ajustes-${width}.png`),fullPage:true});
 await area(page,'groups');await expect(page.locator('#conversation-title')).toHaveText('Equipe Aurora');
});

test('rascunho resiste a polling, erro, navegação e histórico',async({page})=>{
 await intercept(page);await page.goto('/#settings/transcription');
 // Initial asynchronous routing focuses the title; interact after that route is mounted.
 await expect(page.locator('#settings-transcription')).toBeVisible();
 await page.locator('#skip-content').focus();await page.keyboard.press('Enter');await expect(page.locator('#main-content')).toBeFocused();
 await page.locator('#setting-vocabulary').fill('Aurora');
 await page.waitForTimeout(2200);await expect(page.locator('#setting-vocabulary')).toHaveValue('Aurora');
 await area(page,'audio');await expect(page.locator('#unsaved-dialog')).toBeVisible();
 await page.locator('#keep-editing').click();await expect(page.locator('#setting-vocabulary')).toHaveValue('Aurora');
 await page.locator('.advanced-settings summary').first().click();await page.locator('#setting-model').fill('invalido');
 await page.locator('#save-settings').click();await expect(page.locator('#settings-feedback')).toContainText('Modelo não encontrado');
 await page.locator('#setting-model').fill('/exemplo/modelo.bin');await page.locator('#save-settings').click();await expect(page.locator('#settings-feedback')).toContainText('Ajustes salvos');
 await area(page,'audio');await page.goBack();await expect(page.locator('#settings-transcription')).toBeVisible();
 await page.locator('#setting-vocabulary').fill('Descartar');await page.locator('#category-data').click();await page.locator('#discard-settings').click();
 await page.locator('#category-transcription').click();await expect(page.locator('#setting-vocabulary')).toHaveValue('Aurora');
});

test('ações de leitura, pessoas, áudio e teclado continuam disponíveis',async({page})=>{
 const {requests}=await intercept(page);await page.goto('/');
 await page.locator('.quoted-message').click();await expect(page.locator('#message-detail')).toBeVisible();
 await page.keyboard.press('Escape');await expect(page.locator('.quoted-message')).toBeFocused();
 await page.getByRole('button',{name:'Ver registro',exact:true}).first().click();await expect(page.locator('#record-content')).toContainText('wpp evidence');await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'Identificar pessoa',exact:true}).first().click();await page.locator('#person-name').fill('Marina');await page.locator('#save-person').click();await expect(page.locator('#person-dialog')).not.toBeVisible();
 await page.locator('#export-chat').click();expect(requests.some(r=>r.path==='/api/exports')).toBe(true);
 await area(page,'audio');await page.getByRole('button',{name:'Ver transcrição',exact:true}).click();await expect(page.locator('#message-detail')).toBeVisible();await expect(page.locator('#detail-body')).toContainText('Vou organizar');await page.keyboard.press('Escape');await expect(page.locator('#message-detail')).not.toBeVisible();
 await page.getByRole('button',{name:'Tentar novamente',exact:true}).click();expect(requests.some(r=>r.path==='/api/audio/failed/retry')).toBe(true);
 await page.locator('#audio-settings').click();await expect(page.locator('#settings-transcription')).toBeVisible();
});

test('consulta externa explica capacidades e mantém nomes e retry',async({page})=>{
 const {data}=await intercept(page);data.snapshot.owner=false;data.snapshot.connection='external';await page.goto('/');
 await expect(page.locator('#owner-notice')).toBeVisible();await expect(page.locator('#sync-chat')).toBeDisabled();
 await area(page,'settings');await expect(page.locator('#pair-account')).toBeDisabled();
 await area(page,'people');await expect(page.locator('#add-person')).toBeEnabled();
 await area(page,'audio');await expect(page.getByRole('button',{name:'Tentar novamente'})).toBeEnabled();
});

test('próximos passos não exigem modelo nem bloqueiam consulta',async({page})=>{
 const {data}=await intercept(page);data.snapshot.linked=false;data.snapshot.audio_available=false;data.snapshot.audio_error='Modelo não instalado';await page.goto('/');
 await expect(page.locator('#onboarding-action')).toHaveText('Conectar conta');await expect(page.locator('#messages')).toContainText('A revisão ficou pronta');
 await page.locator('#onboarding-action').click();await expect(page.locator('#settings-account')).toBeVisible();
 data.snapshot.linked=true;data.chats.forEach(c=>c.Tracked=false);await page.locator('#refresh').click();await expect(page.locator('#onboarding-action')).toHaveText('Escolher conversas');
 await page.locator('#onboarding-action').click();await expect(page.locator('#tab-available')).toHaveAttribute('aria-selected','true');
});

test('polling preserva leitura e atividade mostra estados reais',async({page})=>{
 const {data}=await intercept(page);data.messages.items=Array.from({length:18},(_,i)=>({...data.messages.items[i%3],source_id:'many'+i}));await page.goto('/');
 await expect(page.locator('#messages')).toContainText('A revisão');
 await page.locator('#messages').evaluate(node=>node.scrollTop=140);
 data.chats[0].Generation=2;await expect(page.locator('#new-messages')).toBeVisible();
 expect(await page.locator('#messages').evaluate(node=>node.scrollTop)).toBeGreaterThan(0);
 await area(page,'activity');await expect(page.locator('#recent-operations')).toContainText('Concluída');
 for(const [state,label] of [['failed','Falhou'],['cancelled','Cancelada'],['interrupted','Interrompida'],['queued','Na fila'],['running','Em andamento']]){
  data.snapshot.operations[0].state=state;await page.locator('#refresh').click();await expect(page.locator('#recent-operations')).toContainText(label);
 }
 await page.getByRole('button',{name:'Cancelar',exact:true}).last().click();
});

test('fallback, movimento reduzido e viewport equivalente a zoom 200%',async({page},testInfo)=>{
 await page.setViewportSize({width:720,height:450});await page.emulateMedia({reducedMotion:'reduce',colorScheme:'dark'});
 await intercept(page);await page.goto('/#inexistente');await expect(page.locator('#page-title')).toHaveText('Conversas');await fits(page);
 await area(page,'settings');await page.locator('#category-data').click();await fits(page);
 await page.screenshot({path:testInfo.outputPath('painel-compacto-dark.png'),fullPage:true});
});

test('tokens de texto, ação e controles preservam contraste nos dois temas',async({page})=>{
 await intercept(page);await page.goto('/');
 for(const colorScheme of ['light','dark']){
  await page.emulateMedia({colorScheme});
  const values=await page.evaluate(()=>{
   const style=window.getComputedStyle(document.documentElement);
   const luminance=hex=>{let value=hex.trim().slice(1);if(value.length===3)value=value.split('').map(c=>c+c).join('');const channels=value.match(/../g).map(value=>{const s=parseInt(value,16)/255;return s<=.04045?s/12.92:((s+.055)/1.055)**2.4;});return channels[0]*.2126+channels[1]*.7152+channels[2]*.0722;};
   const ratio=(a,b)=>{const x=luminance(style.getPropertyValue(a)),y=luminance(style.getPropertyValue(b));return (Math.max(x,y)+.05)/(Math.min(x,y)+.05);};
   return {text:ratio('--text','--surface'),muted:ratio('--muted','--surface'),action:ratio('--accent','--surface'),control:ratio('--control-line','--surface')};
  });
  expect(values.text).toBeGreaterThanOrEqual(4.5);expect(values.muted).toBeGreaterThanOrEqual(4.5);expect(values.action).toBeGreaterThanOrEqual(4.5);expect(values.control).toBeGreaterThanOrEqual(3);
 }
});
