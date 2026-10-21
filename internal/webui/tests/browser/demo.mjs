export function demo() {
  const time = '2026-10-18T14:30:00Z';
  const settings = {capture_enabled:true,transcription_enabled:true,language:'pt',vocabulary:'',model_path:'/example/model.bin',whisper_path:'whisper-cli',ffmpeg_path:'ffmpeg',threads:4,timeout:'30m'};
  const message = {ID:'m1',Chat:'equipe@g.us',Time:time,Text:'The review is ready. Can we check the details at our next meeting?',Kind:'texto',Sender:'pessoa@lid'};
  return {
    chats:[{ID:'equipe@g.us',Name:'Aurora Team',Tracked:true,Count:128,Generation:1},{ID:'ideias@g.us',Name:'Ideas and references',Tracked:true,Count:42,Generation:1},{ID:'familia@g.us',Name:'Family',Tracked:false,Count:0}],
    snapshot:{owner:true,linked:true,connection:'connected',audio_available:true,settings,operations:[{id:'demo',kind:'sync',state:'succeeded',detail_code:'backend.operation_completed_history_coverage_remains_unconfirmed',detail:'Sincronização concluída. Cobertura do histórico não confirmada.',updated:time}]},
    messages:{more:false,items:[{source_id:'demo1',route:'Marina → Aurora Team',message},{source_id:'demo2',route:'Rafael → Aurora Team',reply:{state:'available',source_id:'demo1',author:'Marina',preview:message.Text},message:{...message,ID:'m2',Time:'2026-10-18T14:28:00Z',Text:'Agreed! I collected the references for our discussion.',ReplyID:'m1'}},{source_id:'demo3',route:'Marina → Aurora Team',message:{...message,ID:'m3',Time:'2026-10-18T14:25:00Z',Kind:'áudio',Text:'',Transcript:{Text:'I will organize the notes and share the next steps with the team.',Model:'Local Whisper'}}}]},
    people:[{id:'pessoa@lid',name:'Marina',detected_name:'Marina'},{id:'outra@lid',name:'Rafael',detected_name:'Rafael'}],
    audio:{summary:{done:1,failed:1},more:false,items:[{source_id:'demo3',chat:'equipe@g.us',author:'Marina',time,attempts:1,state:'done',has_transcript:true,transcript_preview:'I will organize the notes and share the next steps with the team.'},{source_id:'failed',chat:'equipe@g.us',author:'Rafael',time,attempts:1,state:'failed',error:'Local engine unavailable.',retry_eligible:true,tracked:true}]},
    detail:{source_id:'demo3',author:'Marina',time,message:{...message,Transcript:{Text:'I will organize the notes and share the next steps with the team.',Model:'Local Whisper'}}},
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
      if(path==='/api/preferences')body={application_language:data.application_language||'en',initial_language:data.initial_language||data.application_language||'en'};
      else if(path==='/api/settings')body={timezone:'UTC'};
      else if(path==='/api/service')body=data.snapshot;
      else if(path==='/api/chats')body=data.chats;
      else if(path.endsWith('/messages'))body=data.messages;
      else if(path.startsWith('/api/groups/'))body={people:data.people};
      else if(path==='/api/audio')body=data.audio;
      else if(path.startsWith('/api/audio/')||path.startsWith('/api/messages/'))body=data.detail;
      else if(path==='/api/tables')body=[{name:'chats',count:3,description:'Demo chats'}];
      else if(path.startsWith('/api/tables/'))body={columns:['id','name'],rows:[[1,'Aurora Team']],more:false};
      else throw new Error('Unmocked API '+path);
    } else if(path==='/api/preferences'){data.application_language=request.postDataJSON().application_language;body={application_language:data.application_language};
    } else if(path==='/api/service/settings') {
      const input=request.postDataJSON();
      if(input.model_path==='invalido')return route.fulfill({status:400,json:{error:'Model not found. Check the path.'}});
      Object.assign(data.snapshot.settings,input);
    } else if(path==='/api/service/capture')data.snapshot.settings.capture_enabled=request.postDataJSON().enabled;
    else if(path==='/api/service/transcription')data.snapshot.settings.transcription_enabled=request.postDataJSON().enabled;
    else if(path.endsWith('/tracking'))data.chats.find(c=>path.includes(encodeURIComponent(c.ID))).Tracked=request.postDataJSON().tracked;
    else if(path.startsWith('/api/audio/'))body={status:'pending',detail:'Requeued for the external controller'};
    await route.fulfill({json:body});
  });
  return {data,requests};
}
