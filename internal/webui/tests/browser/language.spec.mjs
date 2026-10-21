import {test,expect} from '@playwright/test';
import {demo,intercept} from './demo.mjs';

for(const language of ['en','pt-BR'])for(const width of [360,768,1440])test(`language ${language} remains accessible at ${width}px`,async({page})=>{
 const data=demo();data.application_language=language;await intercept(page,data);await page.setViewportSize({width,height:900});await page.goto('/#settings/advanced');
 await expect(page.locator('html')).toHaveAttribute('lang',language);
 await expect(page.locator('#page-title')).toHaveText(language==='en'?'Settings':'Ajustes');
 await expect(page.locator('#application-language')).toHaveValue(language);
 for(const colorScheme of ['light','dark']){
  await page.emulateMedia({colorScheme});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth)).toBe(true);
  await page.locator('#application-language').focus();await expect(page.locator('#application-language')).toBeFocused();
 }
});

test('English default and remembered Portuguese preserve content, focus, reading, and processing settings',async({page})=>{
 const {data,requests}=await intercept(page);await page.goto('/');await expect(page.locator('html')).toHaveAttribute('lang','en');
 await expect(page.locator('#conversation-title')).toHaveText('Aurora Team');
 const originalMessages=JSON.stringify(data.messages),recognition=data.snapshot.settings.language;
 await page.goto('/#settings/advanced');await expect(page.locator('#application-language')).toHaveValue('en');
 await page.locator('#application-language').selectOption('pt-BR');await page.locator('#save-language').click();
 await expect(page.locator('html')).toHaveAttribute('lang','pt-BR');await expect(page.locator('#language-feedback')).toHaveText('Idioma da aplicação salvo.');
 await expect(page.locator('#save-language')).toBeFocused();
 await expect(page.locator('.quoted-message').first()).toContainText('Marina');
 await expect(page.locator('.quoted-message').first()).toContainText('The review is ready');
 expect(data.snapshot.settings.language).toBe(recognition);expect(JSON.stringify(data.messages)).toBe(originalMessages);
 expect(requests.filter(r=>r.method!=='GET'&&r.path!=='/api/preferences')).toHaveLength(0);
 await page.reload();await expect(page.locator('html')).toHaveAttribute('lang','pt-BR');await expect(page.locator('#application-language')).toHaveValue('pt-BR');
 await page.locator('#application-language').selectOption('en');await page.locator('#save-language').click();await expect(page.locator('html')).toHaveAttribute('lang','en');
});

test('consultation mode, initial override, and 200 percent zoom keep language controls available',async({page})=>{
 const data=demo();data.application_language='pt-BR';data.initial_language='en';data.snapshot.owner=false;await intercept(page,data);await page.goto('/#settings/advanced');
 await expect(page.locator('html')).toHaveAttribute('lang','en');await expect(page.locator('#application-language')).toHaveValue('pt-BR');
 await page.evaluate(()=>document.documentElement.style.zoom='2');await page.setViewportSize({width:1440,height:900});
 await page.locator('#save-language').click();await expect(page.locator('html')).toHaveAttribute('lang','pt-BR');
 await expect(page.locator('#save-language')).toBeEnabled();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth)).toBe(true);
});
