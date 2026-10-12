// Real pinned kernel; synthetic bridge responses, no network/model/SQL calls.
import {spawn} from 'node:child_process';
import {createInterface} from 'node:readline';
import assert from 'node:assert/strict';
import path from 'node:path';
const root=path.resolve(process.argv[2]);
for(const protocol of [0,1]){
  await new Promise((resolve,reject)=>{
    const env=Object.fromEntries(['PATH','SystemRoot','WINDIR','TEMP','TMP','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
    env.DSH_TELEMETRY_DISABLED='1';
    const child=spawn(process.execPath,['--max-old-space-size=192',path.join(root,'harness/runner.mjs')],{cwd:root,env,stdio:['pipe','pipe','pipe'],windowsHide:true});
    let models=0,finished=false,seq=0,cursor=0,failure;
    const stop=error=>{failure??=error;child.kill();};
    const timeout=setTimeout(()=>stop(Error('relocated kernel timed out')),15000);
    child.stderr.resume();
    child.on('error',stop);
    child.stdin.on('error',stop);
    createInterface({input:child.stdout}).on('line',line=>{
      try{
        const f=JSON.parse(line);assert.equal(f.id,++seq);
        if(protocol===1){assert.equal(f.event_start,cursor);assert.equal(f.event_end-f.event_start,f.events.length);cursor=f.event_end;}
        if(f.method==='model'){
          assert.equal(++models,1);assert.ok(f.params.messages.length);assert.equal(f.params.tools.length,0);
          child.stdin.write(JSON.stringify({id:f.id,result:{role:'assistant',content:'relocated kernel fixture'}})+'\n');
        }else if(f.method==='finish'){
          assert.equal(models,1);assert.equal(f.params.result,'relocated kernel fixture');
          if(protocol===1)assert.ok(cursor>0);
          finished=true;child.stdin.end(JSON.stringify({id:f.id,result:{done:true}})+'\n');
        }else throw Error('unexpected kernel method');
      }catch(e){stop(e);}
    });
    child.on('close',code=>{clearTimeout(timeout);if(failure)reject(failure);else if(!finished||code!==0)reject(Error('kernel did not finish'));else resolve();});
    child.stdin.write(JSON.stringify({type:'init',run_id:'relocated-fixture',task:'Return fixture answer.',system:'Synthetic packaging probe.',model:'fixture',context_tokens:32000,tools:[],...(protocol===1?{evidence_protocol:1}:{})})+'\n');
  });
}
console.log('Relocated Node and both pinned DSH protocols passed; synthetic bridge, no provider calls.');
