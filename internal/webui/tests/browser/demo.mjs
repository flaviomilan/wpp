export function demo() {
  const time = '2026-10-18T14:30:00Z';
  const settings = {capture_enabled:true,transcription_enabled:true,language:'pt',vocabulary:'',model_path:'/exemplo/modelo.bin',whisper_path:'whisper-cli',ffmpeg_path:'ffmpeg',threads:4,timeout:'30m'};
  const message = {ID:'m1',Chat:'equipe@g.us',Time:time,Text:'A revisão ficou pronta. Podemos conferir os detalhes na próxima reunião?',Kind:'texto',Sender:'pessoa@lid'};
  return {
    chats:[{ID:'equipe@g.us',Name:'Equipe Aurora',Tracked:true,Count:128,Generation:1},{ID:'ideias@g.us',Name:'Ideias e referências',Tracked:true,Count:42,Generation:1},{ID:'familia@g.us',Name:'Família',Tracked:false,Count:0}],
    snapshot:{owner:true,linked:true,connection:'connected',audio_available:true,settings,operations:[{id:'demo',kind:'sync',state:'succeeded',detail:'Sincronização concluída. Cobertura do histórico não confirmada.',updated:time}]},
    messages:{more:false,items:[{source_id:'demo1',route:'Marina → Equipe Aurora',message},{source_id:'demo2',route:'Rafael → Equipe Aurora',reply:{state:'available',source_id:'demo1',author:'Marina',preview:message.Text},message:{...message,ID:'m2',Time:'2026-10-18T14:28:00Z',Text:'Combinado! Separei as referências para a conversa.',ReplyID:'m1'}},{source_id:'demo3',route:'Marina → Equipe Aurora',message:{...message,ID:'m3',Time:'2026-10-18T14:25:00Z',Kind:'áudio',Text:'',Transcript:{Text:'Vou organizar as anotações e compartilhar os próximos passos com a equipe.',Model:'Whisper local'}}}]},
    people:[{id:'pessoa@lid',name:'Marina',detected_name:'Marina'},{id:'outra@lid',name:'Rafael',detected_name:'Rafael'}],
    audio:{summary:{done:1,failed:1},more:false,items:[{source_id:'demo3',chat:'equipe@g.us',author:'Marina',time,attempts:1,state:'done',has_transcript:true,transcript_preview:'Vou organizar as anotações e compartilhar os próximos passos com a equipe.'},{source_id:'failed',chat:'equipe@g.us',author:'Rafael',time,attempts:1,state:'failed',error:'Motor local indisponível.',retry_eligible:true,tracked:true}]},
    detail:{source_id:'demo3',author:'Marina',time,message:{...message,Transcript:{Text:'Vou organizar as anotações e compartilhar os próximos passos com a equipe.',Model:'Whisper local'}}},
  };
}
export async function intercept(page, data=demo()) {
  const requests=[];
  await page.route('**/*', async route => {
    const request=route.request(),url=new URL(request.url());
    if(url.origin!=='http://127.0.0.1:4199')throw new Error('External request blocked: '+url.origin);
    if(!url.pathname.startsWith('/api/'))return route.continue();
    requests.push({path:url.pathname,method:request.method(),body:request.postDataJSON()});
    let body={id:'demo-operation'};
    const path=url.pathname;
    if(request.method()==='GET') {
      if(path==='/api/settings')body={timezone:'UTC'};
      else if(path==='/api/service')body=data.snapshot;
      else if(path==='/api/chats')body=data.chats;
      else if(path.endsWith('/messages'))body=data.messages;
      else if(path.startsWith('/api/groups/'))body={people:data.people};
      else if(path==='/api/audio')body=data.audio;
      else if(path.startsWith('/api/audio/')||path.startsWith('/api/messages/'))body=data.detail;
      else if(path==='/api/tables')body=[{name:'chats',count:3,description:'Conversas de demonstração'}];
      else if(path.startsWith('/api/tables/'))body={columns:['id','name'],rows:[[1,'Equipe Aurora']],more:false};
      else throw new Error('Unmocked API '+path);
    } else if(path==='/api/service/settings') {
      const input=request.postDataJSON();
      if(input.model_path==='invalido')return route.fulfill({status:400,json:{error:'Modelo não encontrado. Confira o caminho.'}});
      Object.assign(data.snapshot.settings,input);
    } else if(path==='/api/service/capture')data.snapshot.settings.capture_enabled=request.postDataJSON().enabled;
    else if(path==='/api/service/transcription')data.snapshot.settings.transcription_enabled=request.postDataJSON().enabled;
    else if(path.endsWith('/tracking'))data.chats.find(c=>path.includes(encodeURIComponent(c.ID))).Tracked=request.postDataJSON().tracked;
    else if(path.startsWith('/api/audio/'))body={status:'pending',detail:'Reenfileirado para controlador externo'};
    await route.fulfill({json:body});
  });
  return {data,requests};
}
