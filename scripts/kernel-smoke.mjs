// Real pinned kernel; synthetic bridge responses, no network/model/SQL calls.
import {spawn} from 'node:child_process';
import {createInterface} from 'node:readline';
import assert from 'node:assert/strict';
import path from 'node:path';
import fs from 'node:fs';
import {createHash} from 'node:crypto';
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
  'dsh-source-639ed015-native-v4':'native-harness',
  'dsh-source-639ed015-final-v5':'final-harness',
  'dsh-source-639ed015-guarded-v7':'guarded-harness',
};
for(const engine of manifest.source_engines??[]){
  assert.ok(sources[engine],'unknown packaged source engine');
  const entry=path.join(root,sources[engine],'source-runner.mjs');
  const budget=engine==='dsh-source-639ed015-budget-v3';
  const final=['dsh-source-639ed015-final-v5','dsh-source-639ed015-guarded-v7'].includes(engine);
  const native=engine==='dsh-source-639ed015-native-v4'||final;
  await new Promise((resolve,reject)=>{
    const env=Object.fromEntries(['PATH','SystemRoot','WINDIR','TEMP','TMP','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
    const child=spawn(process.execPath,['--no-global-search-paths',native?'--max-old-space-size=512':'--max-old-space-size=192',entry],{cwd:path.dirname(entry),env,stdio:['pipe','pipe','pipe'],windowsHide:true});
    let models=0,finished=false,seq=0,cursor=0,failure;
    const draft='迁移后检查原文🙂e\u0301\r\n尾部空白  \n',digest=createHash('sha256').update(draft).digest('hex');
    let candidate,selection;
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
          ++models;assert.ok(models<=(final?2:1));assert.equal(f.params.max_tokens,native?393216:budget?6144:4096);
          const message=final?{role:'assistant',tool_calls:[{id:'relocated-call-'+models,type:'function',function:{name:models===1?'mcp_fixture_check':'final_output_commit',arguments:JSON.stringify(models===1?{draft}:{operation_id:candidate,sha256:digest})}}]}:{role:'assistant',content:'relocated source fixture'};
          result=(budget||native)?{format:native?2:1,kind:'completion',message,usage:{prompt_tokens:100,completion_tokens:10,cached_prompt_tokens:0}}:message;
        }else if(final&&f.method==='tool'){
          assert.equal(f.params.name,'mcp_fixture_check');assert.equal(f.params.arguments.draft,draft);
          candidate='relocated-source/dsh/'+f.id;
          result=JSON.stringify({result:JSON.stringify({structuredContent:{ready:true}}),receipt:{operation_id:candidate,committed:true,final_output:{protocol:'receipt-field-v1',sha256:digest,ready:true}}});
        }else if(final&&f.method==='final_output_select'){
          assert.equal(f.params.operation_id,candidate);assert.equal(f.params.sha256,digest);assert.equal(f.params.call_id,'relocated-call-2');
          selection='relocated-source/dsh/'+f.id;
          result={selected:true,selection_id:selection,operation_id:candidate,sha256:digest,policy_sha256:'b'.repeat(64),rule_revision:1,call_id:f.params.call_id};
        }else if(final&&f.method==='finish_ref'){
          assert.equal(models,2);assert.equal(f.params.selection_id,selection);assert.ok(cursor>0);
          finished=true;result={done:true,selection_id:selection,sha256:digest};
        }else if(f.method==='finish'){
          assert.equal(final,false,'final policy must not fall back to ordinary text');
          assert.equal(models,1);assert.equal(f.params.result,'relocated source fixture');assert.ok(cursor>0);
          finished=true;result={done:true};
        }else throw Error('unexpected source method '+engine+' '+f.method+' '+JSON.stringify(f.params));
        child.stdin.write(JSON.stringify({id:f.id,result})+'\n');
      }catch(e){stop(e);}
    });
    child.on('close',code=>{clearTimeout(timeout);if(failure)reject(failure);else if(!finished||code!==0)reject(Error('source did not finish'));else resolve();});
    child.stdin.write(JSON.stringify({type:'init',engine,evidence_protocol:1,run_id:'relocated-source',task:'Return fixture answer.',system:'Synthetic packaging probe.',model:'fixture',context_tokens:native?1048576:32000,tools:final?[{name:'mcp_fixture_check',description:'Synthetic checker',inputSchema:{type:'object',properties:{draft:{type:'string'}},required:['draft']}}]:[],...(final?{final_output:{protocol:'receipt-field-v1',tool:'mcp_fixture_check',input_field:'draft',ready_path:['structuredContent','ready']}}:{}),...(budget?{model_max_tokens:6144}:{}),...(native?{model_max_tokens:393216,model_limits:{model:'fixture',base_url:'https://fixture.invalid',protocol:'openai-chat-completions',context_window:1048576,max_output_tokens:393216,source:'https://fixture.invalid/models',observed_at:'2026-10-14T00:00:00Z',metadata_sha256:'a'.repeat(64)}}:{})})+'\n');
  });
}
console.log('Relocated Node, both historical protocols and every packaged source engine passed; synthetic bridge, no provider calls.');
