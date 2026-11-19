---
kind: chore
pri: 1
status: ready
area: .
---
# Set up ape/ from what this repo has

Goal: a fresh agent reading only root AGENTS.md → _FRAMEWORK.md + PROJECT.md can say (1) what the project is, (2) how
to build, test and check it, (3) what's decided, (4) what's next. Nothing open lost; nothing said twice.
Home by who needs it: true for anyone working on the code (build, test, checks, CI, style, architecture, the team's
workflow) → the code repo, where it is (AGENTS.md, CONTRIBUTING, design docs): a teammate or contributor without ape/
loses nothing. Only the owner's (their plans, status, rulings, private terms) → ape/. Unsure → stays. Others in
`git log --format=%an -- <file>` → shared unless the owner says otherwise. AGENTS.md is shared even in a solo repo.
House rules win: one forbids a step here (no deleting, always push, never touch X) → skip the step, owner task.
House practice (rules, or what the git log shows) vs _FRAMEWORK.md's defaults: who commits, how main is merged, the
commit message format, review, when to stop, how to ask. Each difference → owner task: keep theirs, switch to ape's,
or reconcile (house "subagents don't commit" vs agents committing on their branch → main only gets the
orchestrator's squash commits); `rec:` one. Theirs holds until answered; the ruling → PROJECT.md. No practice →
ape's default, no ask. Any other lasting choice this task doesn't settle → owner task too (_FRAMEWORK.md, Owner);
never decided silently.
Start: commit what `ape init` changed (it names the files; a protected branch → its usual route); alone →
`ape start T1`; split → this task stays unstarted, blocked on the parts. Replace `<Project>` in PROJECT.md,
DECISIONS.md. Run the checks once with `HOME=$(mktemp -d) TMPDIR=$(mktemp -d)`, comparing `.git/config` before and
after: what they write outside the worktree → PROJECT.md (Code); checks that can't pass here → PROJECT.md records the
failing set as the baseline.
Only README/LICENSE/CI found → build, test, checks from the code → AGENTS.md (below the pointer block); PROJECT.md
the rest; DECISIONS.md empty (candidate decisions → questions), questions (step 2), done. Else:
1. Inventory, tracked or not (`git status --ignored`; search tools skip ignored files): docs (README, plans, status,
   TODO files, ADRs, specs, research, notes, logs); agent files (AGENTS.md, CLAUDE.md, GEMINI.md, .cursor/rules,
   .github/copilot-instructions.md, nested; skill/command dirs + config); trackers (in-repo, the forge's issues);
   local + remote branches, worktrees, open PRs. For each, what reads or writes it (`git grep` its name: code, tests,
   CI, scripts, tools, skills, other repos).
   Before moving, trimming or deleting anything: the plan (each source → its home; what's deleted, trimmed, kept) in
   this task's Progress; ask the owner to approve it (Owner: on this task, `rec:` yes). File with it every question
   found so far (house practice above, owner tasks steps 2–5 would file), each with `rec:`: one review covers all.
   Changes asked → revise the plan, ask again.
   Too big for one agent → the orchestrator files a task per target: PROJECT.md, DECISIONS.md, ref/, new tasks (area
   `ape/tasks/<slug>`), code-repo edits (area `.`, a worktree); the owner's rulings, wherever written, to one part.
   One writer each; all read sources at one commit; shared rules + plan in this task's Progress (parts cite, never
   copy); `handoff <file | Tn>:` lines in a part's Progress, relayed by the orchestrator; this task
   `status=blocked needs=<every part>`.
2. Each piece → one home, rewritten per Writing, not copied:
   - shared (Goal) → stays; found only in private notes → AGENTS.md. What (a line; README says the rest),
     per-worktree setup (`ape start` runs none), owner (+ what's delegated), private terms, the owner's rulings on
     how we work → PROJECT.md: it links the shared docs, never repeats them. Headings are a start; change freely.
   - decisions in force (checked against the code) → DECISIONS.md, one line each; a revision merges into its
     original's line; why unrecorded → `why: unrecorded`. A numbered series keeps its numbers (DEC-7 → D7) unless
     `git grep -w D7` finds the name taken (then the old form stays). Decisions a doc that stays records with their
     why (ADRs, a spec, a design doc; one line each would tear it apart) stay there; DECISIONS.md links it, holds
     the rest; new decisions on its subject go there. A decision this migration changes → rewritten, `prov:` + owner
     task.
   - open work (in progress, planned, waiting on something), reproduced bugs → tasks; a long plan: a task per step
     of the next stage, one per later stage (split when it's next). A tracker (in-repo or forge) stays the backlog,
     its priorities and others' PRs too; PROJECT.md says where next is, how an item is taken on and closed; tasks
     only for work taken on, citing its ID (`from=gh#91`); never mirror it.
   - questions answerable now → owner tasks (_FRAMEWORK.md, Owner), all filed together; a default already in
     effect → also a `prov:` decision. Work goes on. Deferred to a later stage → not owner tasks (nobody can answer
     them yet): they stay in a doc that stays, else go to that stage's task (many → ref/); the stage's task cites
     them.
   - research, reports, outside specs still true, private → ref/, present tense. Docs about this code stay (below).
   - stays in the code repo: shared (Goal); what code, tests, tools or skills read; docs about the code (spec,
     design, architecture: they change with it, _FRAMEWORK.md Writing); docs for users and contributors (README,
     CHANGELOG, CONTRIBUTING, API); research beside the code it measured; a tool's own dir. Trim what moved (a mixed
     file: the owner's plan, status, questions now on tasks); history inside → a follow-up task; ape/ links, never
     repeats; PROJECT.md (Architecture) names each design doc. One agents read, over ~300 lines → owner task, after this
     migration: split by subject, `rec:` yes (a file per subject: its decisions, rejected options, open questions).
   - done work, status logs, session notes, dated text → deleted if tracked; rules _FRAMEWORK.md states → left out
     of what moves (shared docs keep theirs). Read by a check, over ~50 files, or a tool's dir → owner task, kept
     until answered. Untracked → content moves, file stays.
3. IDs in code or docs: private ones → Dn or `Tn` (done work's: dropped); public ones (issues, ADRs) stay.
   In-flight branches keep old IDs; their task lists them. A moved path something else reads → update that if you
   control it, else leave the path.
4. Private terms (names, hosts, accounts, devices, home paths) in the code repo → moved, listed in PROJECT.md. Kept:
   what the code needs or publishes on purpose (LICENSE, package metadata, CODEOWNERS, a device model the logic
   needs). Many files, or already pushed public (moving doesn't erase history) → owner task.
5. Branches: merged topic branches (squash-merged PRs too; never main, release or base) → `git branch -d`, listed. In
   flight = changes not on main by a person or agent (not tool-owned snapshots or sync refs). Stale (months old, far
   behind) → owner task: revive or drop. In use (a process in it, a change < 3 h old) → owner task, skip. Else a task
   each, ready, state + old IDs in Progress. Untracked work → a WIP commit on its branch; ignored env/secrets → same
   path in the main checkout; big ignored artefacts → the project's cache dir, path in Progress. Rename → ape/Tn-slug
   (pushed: task says `git push origin HEAD:<old name>`); remove the worktree; `ape start Tn` remakes it.
6. Agent files (root, nested): shared (Goal). Their conventions stay; the owner's lines in them move (PROJECT.md,
   tasks); history goes (step 2). `ape init` put the pointer block atop root AGENTS.md, CLAUDE.md, .claude/CLAUDE.md
   (not one that imports or links AGENTS.md). Other root harness files whose harness doesn't read AGENTS.md
   (GEMINI.md): the pointer block at the top. Skill and command dirs stay.
Migration tasks are temporary: permanent tasks never cite them.
Split → before closing, a fresh agent samples ≥ 40 items (open work, rulings, rules, decisions with numbers) at
the read commit, finds each one's home and reports lost or distorted ones; fix them. Commit messages aren't proof.
done: every inventoried item has a home or was deleted as history (say which in `ape commit` messages); no CHECK;
PROJECT.md's "Not set up" line gone; a fresh subagent given only root AGENTS.md answers Goal's (1)–(4), and one told
to ignore ape/ answers (2) as well as before setup; no subagent → say so and list each answer with its source.
