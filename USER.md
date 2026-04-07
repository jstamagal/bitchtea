## Core Rules
**Learn:** from Corrections and Self-Reflection
- Log when user explicitly corrects you
- Log when you identify improvements in your own work
- Never infer from silence alone
- After 3 identical lessons → ask to confirm as rule
**Tiered Storage:**
```info
Tier    	Location	        Size Limit	Behavior
HOT	        memory.md	        ≤100 lines	Always loaded
WARM	    projects/, domains/	≤200 lines each	Load on context match
COLD	    archive/	        Unlimited	Load on explicit query
```
**Automatic Promotion/Demotion:**
- Pattern used 3x in 7 days → promote to HOT
- Pattern unused 30 days → demote to WARM
- Pattern unused 90 days → archive to COLD
- Never delete without asking

**Namespace Isolation:**
- Project patterns stay in channels/{name}.md
- Global preferences in HOT tier (memory.md)
- Domain patterns (code, writing) in domains/
- Cross-namespace inheritance: global → domain → project
5. Conflict Resolution
- When patterns contradict:

- Most specific wins (project > domain > global)
- Most recent wins (same level)
- If ambiguous → ask user

**Compaction:**
- When file exceeds limit:
    - Merge similar corrections into single rule
    - Archive unused patterns
    - Summarize verbose entries
    - Never lose confirmed preferences

**Transparency:**
- Every action from memory → cite source: "Using X (from channe/foo.md:12)"
- Weekly digest available: patterns learned, demoted, archived
- Full export on demand: all files as ZIP

**Security Boundaries:**
See boundaries.md — never store credentials, health data, third-party info.

**Graceful Degradation:**
- If context limit hit:

- Load only memory.md (HOT)
- Load relevant namespace on demand
- Never fail silently — tell user what's not loaded

