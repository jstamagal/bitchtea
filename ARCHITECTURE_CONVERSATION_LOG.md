# Reconstructed Architecture Conversation Log

This is a reconstructed, human-readable log of the architecture discussion as I understand it from the Gemini log plus our conversation here. It is not a raw transcript. It is meant to capture the substance of what was worked through and what was concluded.

---

## Conversation

**You:** I am trying to explain something that is IRC-shaped, but not literally IRC. Most coding tools are file-tree based. This is not that. I want contexts that feel like channels and subchannels.

**Assistant:** So the context model is not just "repo + cwd + one thread." It is something more like channels, with a main context and child contexts.

**You:** Right. Think `#cornhub` as a main context. Then `#cornhub.website` is a subcontext. I can switch focus between them. The child should be able to read parent context, but it should not write back into the parent.

**Assistant:** So parent-read, child-write-self.

**You:** Yes.

**Assistant:** And what about direct persona conversations?

**You:** That is where the IRC analogy matters. Back in ircii days, you had to keep typing `/msg nickname ...` every time. Then `/query` came along. It was really just a routing mechanic. It made Enter send to that nickname until you changed focus again.

**Assistant:** So `/query` is not a new kind of place. It is not like a channel. It is a routing mechanic that changes where your normal input goes.

**You:** Exactly. Channel is real. Nickname/persona is real. `/query` is routing.

**Assistant:** So if you `/query coding-buddy`, your normal text goes to `coding-buddy` instead of the current channel, but slash commands are still slash commands.

**You:** Yes. That is the right model.

**Assistant:** Then the main context model becomes:
- channel = real context
- subchannel = child context
- nickname/persona = real direct target
- `/query` = routing focus to that direct target

**You:** Yes.

---

**You:** Memory has to follow context identity, not just cwd.

**Assistant:** Meaning channel memory, subchannel memory, and direct-target memory are separate.

**You:** Yes. The thing I kept circling around is that memory is scoped by channel/query identity.

**Assistant:** We looked at a few memory shapes.

**You:** Yes. We considered:
- pure markdown
- hybrid markdown + sqlite
- daemon-style memory like claude-mem

**Assistant:** But the place you kept landing for phase 1 was markdown.

**You:** Yes. Pure markdown for phase 1. That is the line. HOT memory is markdown. Durable memory is markdown summaries by day. Keep it simple first.

**Assistant:** So for phase 1:
- pure markdown
- active memory per context
- durable/day-based summaries
- explicit search/recall still matters

**You:** Yes. The low-level memory plumbing can exist already, but the important thing is the shape of the context model.

**Assistant:** And later, hybrid/sqlite/daemon ideas can still exist, but they are not phase 1.

**You:** Right.

---

**You:** There also needs to be a daemon / janitor / heartbeat thing.

**Assistant:** Is that phase 1 or later?

**You:** Phase 1.

**Assistant:** So even if the full future system is bigger, the idea of background lifecycle, janitoring, compaction help, or heartbeat is part of the first real architecture.

**You:** Yes, but it should be simple and useful. Not a bunch of infrastructure theater.

**Assistant:** So the daemon/janitor/heartbeat idea is real scope, but it needs to stay minimal.

**You:** Exactly.

---

**You:** Queries and channels must not bleed into each other automatically.

**Assistant:** So direct targets do not see channels by default.

**You:** Yes. Not unless explicitly invited.

**Assistant:** Then there is explicit bridging.

**You:** Right. If I invite `coding-buddy` into `#cornhub`, that is not the same as saying all queries just naturally know channel context.

**Assistant:** So default rule:
- direct target sees no channel
- explicit invite/bridge grants channel context

**You:** Yes.

---

**Assistant:** When a persona is invited into a channel, is that a temporary look-in or a real join?

**You:** A real join. That part matters.

**Assistant:** So if `coding-buddy` joins `#cornhub`, he is actually there until parted.

**You:** Yes. But the important part is that the channel still has its own resident agent — the chanop, basically. Inviting `coding-buddy` does not replace the chanop.

**Assistant:** So the channel has its own resident context, and invited personas are additional joined participants.

**You:** Right.

---

**You:** But here is the thing I was thinking through: if `coding-buddy` is joined in `#cornhub`, what does he actually receive?

**Assistant:** Full stream, or on-demand injection?

**You:** I was not sure. If he is fully joined, does every message get streamed to his session file? If I invite multiple personas into the same channel, are we duplicating everything into all of them? That is where echo and context bloat can happen.

**Assistant:** So there are two possibilities:
1. stream everything into each joined persona
2. only give context when addressed

**You:** Right. And what I settled on was not full raw history, not full tool history. What I want is: if I address `coding-buddy`, he should have enough context to answer.

**Assistant:** Then the catch-up context for a joined persona should be summary + recent conversation lines.

**You:** Yes.

**Assistant:** And not tool chatter.

**You:** Right. Just the actual conversation. My messages plus the chanop conversation, the actual human/agent discussion.

**Assistant:** And for a default number of recent lines, you landed around 50.

**You:** Yes. Summary + last 50 conversation lines sounds right.

---

**You:** Also, if `coding-buddy` and `waifu` are in the channel, I want to be able to fully talk with both and have them fully talk to each other eventually. That is a V2 target.

**Assistant:** So V2 is a real multi-persona channel.

**You:** Yes, but without echo problems like when multiple agents joined IRC in OpenClaw and things got stupid.

**Assistant:** So for future multi-persona channels, messages need real identity.

**You:** Exactly.

**Assistant:** That implies:
- shared channel history
- speaker identity on each message
- channel identity on each message
- message/source identity strong enough that a speaker does not re-ingest their own output as new input

**You:** Yes. That is the point. If a joined persona speaks in the channel, that message becomes part of channel history for everyone else, but the speaker should not re-consume their own output as fresh input.

**Assistant:** So channel history is shared truth, but no self-echo.

**You:** Yes.

---

**Assistant:** So if `coding-buddy` is in `#cornhub` and you say `coding-buddy: what do you think?`, what should happen?

**You:** He should be able to answer because he has been sitting there silently absorbing enough conversation context.

**Assistant:** Meaning joined personas silently absorb channel conversation context.

**You:** Yes — conversation context, not tool calls.

**Assistant:** So in-channel addressing works because the joined persona has enough context to answer.

**You:** Exactly.

---

**Assistant:** Then there is the chanop itself.

**You:** Right. Joining a channel auto-creates or auto-attaches the chanop for that channel. That resident persona/agent is basically the channel operator.

**Assistant:** So talking in the channel by default is talking with the chanop.

**You:** Yes.

**Assistant:** But then chanop cannot just be a generic assistant. Different channels may need different prompt stacks.

**You:** Exactly. This is another important unresolved piece. The chanop needs to be distinguishable by prompt stack / profile. Some channels might be coding channels, some assistant-y, some roleplay, whatever. Lots of things in the system may inject prompts, so we need to know what kind of chanop we are dealing with.

**Assistant:** So one unresolved design question is chanop prompt-stack/profile differentiation.

**You:** Yes. That needs to be nailed down.

---

**You:** On restart, I want things to come back.

**Assistant:** Meaning open contexts and current focus restore.

**You:** Yes.

**Assistant:** So if you had channels or direct-target routing open, they should come back with focus intact.

**You:** Yes.

**Assistant:** Then session/context identity has to be real in the event model and persistence.

**You:** Yes.

---

**You:** Providers are a separate thread, but they matter.

**Assistant:** We spent time comparing the bespoke stack with other libraries and SDKs.

**You:** Right. The point was never purity. It was: should we keep building it all ourselves, should we borrow from official `openai-go`, should we borrow from Charm’s stack, should we fork `fantasy` or `catwalk`, etc.

**Assistant:** And the conclusion was not “stay custom no matter what.”

**You:** No. The conclusion was: borrow or fork useful things if they help. Don’t rebuild everything for ego. But also don’t hand control away if it hurts the actual behavior we want.

**Assistant:** So the provider strategy is pragmatic borrowing/forking, not purity-for-purity’s-sake custom work.

**You:** Yes.

---

**Assistant:** So if I boil down the architecture conclusions, the important resolved parts are:

1. Channel is real.
2. Subchannel is real and child-scoped.
3. Nickname/persona is a real direct target.
4. Query is routing, not a separate place.
5. Phase 1 memory is pure markdown.
6. Restart restores open contexts and focus.
7. Direct targets do not see channels by default.
8. Invited personas actually join until parted.
9. Joined personas get summary + recent conversation lines, not tool history.
10. Joined persona speech becomes part of shared channel history, but no self-echo.
11. Daemon/janitor/heartbeat is phase 1.
12. Provider strategy is borrow/fork useful pieces.

**You:** Yes.

**Assistant:** And the unresolved parts are:

1. exact chanop prompt-stack/profile differentiation
2. exact daemon/janitor/heartbeat scope and behavior
3. exact provider borrowing plan
4. the implementation details needed to make the IRC-shaped runtime real

**You:** Yes. That is where we are at.

---

## Short version of what we figured out

### Resolved
- Phase 1 memory = pure markdown.
- Channel/subchannel/direct-target model matters.
- Query is routing.
- Direct targets only see channels when invited.
- Invited personas actually join until parted.
- Catch-up context = summary + recent conversation lines, not tool chatter.
- Shared channel history should support multiple personas later, but no self-echo.
- Restart restores open contexts and focus.
- Daemon/janitor/heartbeat is phase 1.
- Provider strategy = borrow/fork useful pieces.

### Still not fully locked
- how chanop prompt stack/profile selection works
- exact daemon/janitor/heartbeat behavior
- exact provider borrowing/forking implementation strategy
- the concrete code path that turns this into the real runtime

