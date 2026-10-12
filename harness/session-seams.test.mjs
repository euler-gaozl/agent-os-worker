// Characterization of the exact pinned upstream package, not a platform
// persistence implementation or an end-to-end recovery acceptance test.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Context } from '@deepseek-ai/cordis';
import SessionStore from '@deepseek-ai/dsh-session';
import { createUserMessage } from '@deepseek-ai/dsh-llm';

async function setup(t) {
  const ctx = new Context();
  await ctx.plugin(SessionStore, {});
  t.after(async () => { await ctx.fiber.dispose(); });
  return ctx;
}
function append(session, text) {
  return session.append('user/message', {
    message: createUserMessage({ content: [{ type: 'text', text }], source: { kind: 'user' } }),
  }, { surfaceOp: 'append' });
}

test('pinned DSH exposes an awaited flush barrier independently of append', async (t) => {
  const ctx = await setup(t);
  const session = ctx.sessions.create('durability-probe');
  const buffered = [];
  let release;
  const accepted = new Promise(resolve => { release = resolve; });
  let entered;
  const started = new Promise(resolve => { entered = resolve; });
  ctx.on('session/event', (_session, event) => { buffered.push(event); });
  ctx.on('session/flush', async () => { entered(); await accepted; });
  append(session, 'Fixture only, no provider call.');
  assert.equal(buffered.length, 1);
  let completed = false;
  const barrier = ctx.sessions.flush(session).then(value => { completed = true; return value; });
  await started;
  assert.equal(completed, false);
  release();
  assert.equal(await barrier, true);
  assert.equal(completed, true);
});

test('a persistence failure rejects the awaited upstream barrier', async (t) => {
  const ctx = await setup(t);
  const session = ctx.sessions.create('failed-durability-probe');
  append(session, 'Fixture only.');
  ctx.on('session/flush', async () => { throw new Error('controlled storage failure'); });
  await assert.rejects(ctx.sessions.flush(session), /controlled storage failure/);
});

test('upstream surface compaction retains original events in session memory', async (t) => {
  const ctx = await setup(t);
  const session = ctx.sessions.create('memory-probe');
  const first = append(session, 'First original evidence '.repeat(128));
  const second = append(session, 'Second original evidence '.repeat(128));
  // This probes the supported surface replacement primitive. It does not claim
  // to test an LLM summary, automatic compaction, a memory plateau or disk I/O.
  session.append('user/message', {
    message: createUserMessage({content:[{type:'text',text:'Compact fixture summary.'}],source:{kind:'user'}}),
  }, {surfaceOp:{op:'replace',startSeq:first.seq,endSeq:second.seq},sourceEventSeqs:[first.seq,second.seq]});
  const history = session.snapshotEvents();
  assert.equal(history.length, 3);
  assert.equal(history[0].data.message.content[0].text, first.data.message.content[0].text);
  assert.equal(history[1].data.message.content[0].text, second.data.message.content[0].text);
});
