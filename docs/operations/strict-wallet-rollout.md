# Strict wallet protection — DRAFT / NOT READY TO DEPLOY

## Status and decision

This PR is groundwork, **not a completed anti-overdraft fix**. No production
request path uses `internal/pkg/walletguard` yet. Existing behavior (including
existing fail-open paths) remains unchanged. Do not merge/release/deploy this PR
or run the production repair as if it solved the user's requirement.

The minimal *reliable* design is PostgreSQL exact-money reservation, then usage
settlement in the same transaction. A cache check below 2, Redis TTL reservations,
or truncating balances at zero cannot guarantee full collection. The current
estimator is not a ceiling: it uses body bytes/4, guessed STT duration, default
max tokens, and a configurable cap. Never pass that estimate as a proven ceiling.

## Production audit privacy

The actual affected IDs, exact debts and before/after balances stay in the
private operational report, not in this repository. The reviewed cohort has
sufficient own team quotas and unambiguous membership. Unrelated nonmember and
negative-team-quota debts are excluded. Never debit the owner's wallet again.
All fixtures in this PR are synthetic. The correction script requires explicit
reviewed IDs, team ID and a unique batch ID; amounts are always re-read.

## What the new primitive guarantees (only its own API)

- Integer 10^-8 units; exact numeric strings for wallet updates. No rounding,
  floats, saturation, unbounded reservation, or silently discarded charges.
- Serialize all wallets for one user with a PostgreSQL `FOR UPDATE` row lock.
  Multiple application pools/processes share that lock. Reserve the entire
  caller-proven ceiling from the selected personal/team pools or fail atomically.
- Selected spendable funds must be **strictly greater than 0.1**, including at
  admission after other holds. Both stored pools must be nonnegative. Nonmembers
  cannot spend team quota. `team_only` without active membership is rejected.
- A request ID is globally unique. Replayed IDs always return `ErrReplay`; a
  retry/uncertain commit must never authorize another upstream attempt.
- Final charge uses the original pool ordering; refund only unused money to the
  original member pools, even if membership/key mode changed. No owner debit.
- Settle/release is idempotent for identical outcomes, conflicting outcomes fail.
  Use `FinalizeTx` in the SAME transaction as usage dedup/log/account/key effects.
- Charge above ceiling fails with `ErrExceeded` and retains the hold. This is an
  invariant incident requiring reconciliation, **not a guarantee that an already
  served over-ceiling request was fully funded**. Therefore proving the ceiling
  before forwarding is mandatory, and remains unimplemented here.
- Holds never expire automatically. Client cancellation, stream disconnect,
  process death or an async queue timeout does not prove that no charge exists.
  Crash/unknown-outcome holds require durable reconciliation; potentially held
  funds reduce availability, never create free service.
- No Redis/cache dependency: cache staleness and Redis errors cannot grant funds
  in this primitive. DB/schema/validation failures are returned, never fail-open.
- Conservative supported range: 0 through 92233720368.54775807. Larger database
  balances are rejected, not clamped. Zero-cost ceilings are not a bypass.

## Missing integration — deployment blockers

The package is intentionally **not connected** until the following are solved:

1. Authoritative immutable price/route snapshot and proven request limits. Start
   with genuinely fixed-total-price operations, no extra metered charges. Token
   billing needs exact/safely bounded input including fetched/expanded content,
   enforced output+reasoning limits, cache writes/reads/storage, context tiers,
   service-tier/Ultra Fast multipliers and a bound for every failover attempt.
   Unknown prices or limits must reject *before* forwarding. The current
   estimator and `PricingDigest` string alone prove none of these properties.
2. Bind the durable reservation ID to every command, Apply AND ApplyBatch,
   usage log, retry/fallback and asynchronous worker. Dedup and finalization must
   be one transaction. A worker cannot deduct wallets again after the reserve.
3. Durable recovery for lost/dropped billing tasks, unknown upstream outcomes,
   delayed usage and process restart. A queue callback/refcount alone is unsafe.
4. Enforce the same availability on transfers, refunds, admin/manual deductions,
   batch-image `frozen_balance`, legacy `DeductBalance`/`UsageService.Create`, and
   all other wallet writers. The new primitive is NOT a global DB constraint.
5. Route-level coverage below, cache/auth invalidation after commit, independent
   review and representative mocked end-to-end gateway tests. No real paid
   requests or credentials are needed or allowed for tests.

### Entry-point coverage ledger (all currently NOT integrated)

| Entry/flow | Additional constraint before enabling |
|---|---|
|Anthropic messages, OpenAI chat/responses incl. websocket, Gemini generate|bounded tokens/reasoning/cache, tools, multimodal and all mapped/fallback prices|
|Embeddings/count tokens/SystemOne|bound input expansion and actual billable endpoint behavior|
|Search, alpha/web search, tool search|bound tool calls and auxiliary charges|
|Images and edits (incl. async batch)|bound count, input/output token supplements, exact price snapshot; integrate frozen funds|
|Video/Grok media|bound duration, count, resolution, provider completion/polling and delayed usage|
|TTS/STT/audio and live/realtime|real duration/unit limits, bounded streaming sessions; bytes-based estimates insufficient|
|Gemini cached contents|bounded storage duration and mutation lifecycle, not just creation fees|
|Subscriptions/simple mode|explicitly separate no-wallet semantics; no implicit wallet bypass|
|Usage ApplyBatch, legacy Create/DeductBalance|same durable request ID, atomic finalization, no alternate debit|
|Team allocations/transfers/admin/refunds|held funds unavailable; prevent overdrafts on every writer|

**Compatibility recommendation:** implement bounded fixed-price operations first;
explicitly reject unbounded metered operations while strict mode is active. This
would temporarily disable token-priced chat/live/audio/etc. until proven bounds
are implemented. That is a substantial availability change requiring a concrete
rollout decision, not something this draft silently deploys. Do not advertise
strict anti-overdraft while allowing those old paths as a fallback.

## Correction script / maintenance plan

`scripts/operations/repair_team_personal_debt_20261120.sql` defaults to a read-only
snapshot. It is a psql script, not an automatic application migration. It does
not touch the unrelated historical debts or create team transaction records that
could be mistaken for new allocations. A separate exact before/after repair
journal records the correction and proves per-wallet conservation.

Only after gateway coverage is complete and a release/deployment is approved:

1. Back up using the established `pg_dump -Fc` flow and verify the backup. Retain
   the old binary. Stop admission and drain/reconcile all old in-flight billing
   before making corrections. An HTTP handler finishing is not enough.
2. Re-run the read-only audit. Review any changed membership, added indebted
   members, insufficient quota or newly negative team wallets; abort on ambiguity.
3. After explicit maintenance confirmation run psql with both `-v apply=true`
   and `-v maintenance_confirmed=true`, plus `-v 'reviewed_ids={...}'`,
   `-v reviewed_team_id=...`, `-v batch_id=...` from the private reviewed report. The script locks memberships/teams and
   wallets, verifies the explicitly reviewed cohort, computes CURRENT debt,
   inserts journal rows and applies corrections in a single transaction.
   Insufficiency, drift, timeout or invariant violation rolls back everything.
4. Re-running the same batch returns its existing audit rows without any
   further wallet changes. Later debt requires a NEW reviewed batch, not a reset
   of the journal. Exact aggregate personal+team assets must be unchanged.
5. Invalidate affected auth/billing caches only after commit; use the existing
   application outbox/cache invalidation mechanism once integration is ready.
   Keep maintenance active until the new binary, invalidation and checks pass.
6. Deploy with the established `docker cp`/chown/chmod/restart procedure, never
   compose recreate. Preserve fork VERSION, team features, auth snapshot128,
   real model names, CoinGecko fail-closed locking and precise SSRF allowlisting.
   Use `--version` only. No rates/addresses/fees/financial settings are changed.

A DB `CHECK balance>=0` cannot simply be validated now: historical debt exists.
Do not delete that debt or add constraints that break repayment/admin operations
without an explicit migration strategy.

## Local-only tests

```
cd backend
WALLETGUARD_TEST_DSN='postgres://box@127.0.0.1:55439/walletguard_test?sslmode=disable' \
  go test -race -count=1 -v ./internal/pkg/walletguard
```

Integration tests refuse non-loopback hosts or databases not named
`walletguard_test`, and use isolated schemas. No customer data/API keys/orders or
real upstream requests are used. Without the DSN those tests SKIP: that is not a
passing integration verification. Gateway route/retry/streaming/worker/cache
end-to-end integration tests are still missing. The simulated cancellation,
restart and transaction tests apply to the ledger only, not the existing gateway.
