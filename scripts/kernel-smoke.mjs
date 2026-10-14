// Real pinned kernel; synthetic bridge responses, no network/model/SQL calls.
import {spawn} from 'node:child_process';
import {createInterface} from 'node:readline';
import assert from 'node:assert/strict';
import path from 'node:path';
import fs from 'node:fs';
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
const manifest=JSON.parse(fs.readFileSync(path.join(root,'package.json'),'utf8'));
const sources={
  'dsh-source-639ed015-state-v1':'source-harness',
  'dsh-source-639ed015-resident-v2':'resident-harness',
  'dsh-source-639ed015-budget-v3':'budget-harness',
};
for(const engine of manifest.source_engines??[]){
  assert.ok(sources[engine],'unknown packaged source engine');
  const entry=path.join(root,sources[engine],'source-runner.mjs');
  const budget=engine==='dsh-source-639ed015-budget-v3';
  await new Promise((resolve,reject)=>{
    const env=Object.fromEntries(['PATH','SystemRoot','WINDIR','TEMP','TMP','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
    const child=spawn(process.execPath,['--no-global-search-paths','--max-old-space-size=192',entry],{cwd:path.dirname(entry),env,stdio:['pipe','pipe','pipe'],windowsHide:true});
    let models=0,finished=false,seq=0,cursor=0,failure;
    const stop=error=>{failure??=error;child.kill();};
    const timeout=setTimeout(()=>stop(Error('relocated source timed out')),15000);
    child.stderr.resume();child.on('error',stop);child.stdin.on('error',stop);
    createInterface({input:child.stdout}).on('line',line=>{
      try{
        const f=JSON.parse(line);assert.equal(f.id,++seq);assert.equal(f.event_start,cursor);assert.equal(f.event_end-f.event_start,f.events.length);cursor=f.event_end;
        let result;
        if(f.method==='checkpoint')result={persisted:true,manifest_hash:'a'.repeat(64)};
        else if(f.method==='working_state_read')result={operation_id:'relocated-source/working-state/'+f.id,view:{run_id:'relocated-source',task:'Return fixture answer.',constraints:[],revision:0,rule_revision:1,source_rule_revision:0,stale:false,state:null,evidence:[]}};
        else if(f.method==='model'){
          assert.equal(++models,1);assert.equal(f.params.max_tokens,budget?6144:4096);
          const message={role:'assistant',content:'relocated source fixture'};
          result=budget?{format:1,kind:'completion',message,usage:{prompt_tokens:100,completion_tokens:10,cached_prompt_tokens:0}}:message;
        }else if(f.method==='finish'){
          assert.equal(models,1);assert.equal(f.params.result,'relocated source fixture');assert.ok(cursor>0);
          finished=true;result={done:true};
        }else throw Error('unexpected source method '+f.method);
        child.stdin.write(JSON.stringify({id:f.id,result})+'\n');
      }catch(e){stop(e);}
    });
    child.on('close',code=>{clearTimeout(timeout);if(failure)reject(failure);else if(!finished||code!==0)reject(Error('source did not finish'));else resolve();});
    child.stdin.write(JSON.stringify({type:'init',engine,evidence_protocol:1,run_id:'relocated-source',task:'Return fixture answer.',system:'Synthetic packaging probe.',model:'fixture',context_tokens:32000,tools:[],...(budget?{model_max_tokens:6144}:{})})+'\n');
  });
}
console.log('Relocated Node, both historical protocols and every packaged source engine passed; synthetic bridge, no provider calls.');
