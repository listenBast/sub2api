# Strict wallet protection — DRAFT / NOT READY TO DEPLOY

## Release gate / current implementation

This is now an **integrated but incomplete** implementation, not just a ledger
prototype. Real Alpha search and standalone Grok web/x search handlers reserve
funds before forwarding and use the real Apply/ApplyBatch money transaction.
**The full-site requirement is NOT met. Do not merge, release, deploy or execute
production repair yet.** Ordinary metered gateways remain legacy paths; they have
NOT silently been disabled. A nonnegative DB trigger alone cannot fund service
already delivered through those paths.

The chosen design is PostgreSQL exact-money reservation plus atomic finalization,
not a Redis estimate, a check only below 2, or truncating wallets at zero. The old
estimator (body bytes/4, guessed STT duration, default output tokens and a cap) is
not a proven charge ceiling and is never used by the integrated strict routes.

## Coverage matrix (implementation, not a release claim)

| Entry / operation | Implemented status and exact scope |
|---|---|
| OpenAI Alpha search | Real handler → immutable per-call price × user/group multiplier → DB reserve → durable dispatch marker → upstream → mandatory detached billing → outbox → Apply/ApplyBatch. Existing customer contract charges exactly one search invocation, not backend token/reasoning/image/service-tier usage. |
| Standalone Grok web/x search | Real handler → frozen zero-token, SearchCount=1 charge, including any configured per-request fee and search surcharge → same ledger/billing path. Not tool search embedded in a generated conversation. |
| Explicitly free standalone searches | Freeze zero price too (including a later admin price change); authoritative DB floor check; no artificial positive hold/debit. Unique server invocation ID prevents collapsing identical free requests. |
| Personal/team funds | All four key modes; DB membership check; debit only own `users.balance` / allocated `users.team_balance`, never an owner's wallet. Team-only cannot use personal money. No new team allocation occurs. |
| Apply and ApplyBatch | Reserved commands finalize instead of a second debit. Wallet outcome, billing dedup, key/account/subscription effects, exact receipt and outbox deletion share one transaction. Matching duplicate settlement has no further effects. |
| Asynchronous lane / restart | Persist command before process-local queue; periodic multi-process leased outbox recovery using SKIP LOCKED. Transient settle failure retains command and funds. Missing durable writer never falls back on a reserved path. |
| Cache / auth | Reserve/refund invalidate balance and auth caches best effort. Reservation permission is always PostgreSQL, never cached approval. Prepaid settlement invalidates, never queues a second Redis subtraction. Recovery of exact-full fixed charges does not change money again. |
| All wallet DB updates | Migration 254 rejects creation/worsening of negative balances and nonfinite values. Historical debt remains; repayment remains possible. This is defense in depth, NOT prepaid coverage of legacy writers. |
| Team membership | Original team recorded. Pending team-funded holds block exit/removal/switch/user reassignment; active→exit_pending remains possible. Existing team mutation lock ordering can cause a PostgreSQL deadlock rollback; error is fail-closed, not a funding bypass. |
| Anthropic messages; OpenAI chat/responses/websockets; Gemini generate | **NOT integrated**: bounded input expansion, output/reasoning, cache, tools, tiers and mapped/failover pricing remain to be implemented. |
| Embeddings / count tokens / SystemOne | **NOT integrated**: authoritative bounded billable inputs and endpoint-specific charge contract missing. |
| Images / edits / async image batches | **NOT integrated**: existing `frozen_balance` is separate; count/size/input-output token supplements and async captures must join one ceiling/receipt lifecycle. |
| Video / Grok media | **NOT integrated**: bounded duration/resolution/count and polling/delayed completion charges missing. |
| TTS / STT / audio / realtime | **NOT integrated**: enforced real units/duration/session budgets needed; byte estimates are not proof. |
| Gemini cached contents | **NOT integrated**: bounded storage duration plus mutation/extension/deletion lifecycle needed. |
| Subscription and simple mode | Explicitly separate existing no-wallet semantics, not a fallback for a failed wallet reserve. Their quota contracts are not replaced by this ledger. |
| Legacy Create / DeductBalance / unreserved Apply commands | **NOT prepaid**. Existing legacy partial-collection / uncovered-cost policy is NOT repaired by a trigger; cannot claim strict site-wide protection. |

## Exact-money and lifecycle invariants

- Ledger amounts are signed 64-bit integer 10^-8 units, and DB wallet changes use
  exact decimal strings. Current fixed-price billing is quantized ONCE to the
  existing 8-decimal billing policy, then frozen. No clamp or over-ceiling debit.
- Lock the user row with PostgreSQL FOR UPDATE across all app instances. Read
  membership in a fresh statement after acquiring that lock. Take the full
  selected-pool charge or fail atomically. Both wallets must be nonnegative;
  selected spendable funds must be **strictly greater than 0.1**, including when
  another request already holds funds. 0 and 0.1 are rejected; 0.10000001 is only
  admissible if it also covers the whole charge. No special unsafe bypass at 2.
- Unique server-generated reservation IDs cannot be supplied by a client. A
  repeated Reserve returns ErrReplay and never authorizes another forward.
  Internal failover uses the same one-invocation fixed-price contract/hold.
- Price and pool order are immutable for settlement. Charges never exceed their
  fixed invocation hold. The general primitive supports partial refunds to the
  ORIGINAL pools; the integrated paid searches settle the exact full hold.
- A durable dispatch marker is written BEFORE invoking the network forwarder.
  Known pre-forward failures release/refund. A live invocation that never called
  Forward can release even if a dispatch-marker commit acknowledgement was lost.
  After a crash that fact cannot be inferred from the marker alone.
- Alpha's explicit single-attempt non-2xx return and Grok's fully read explicit
  400/422 rejection refund. Unknown transport failures, cancellation, truncated
  streams, prior ambiguous attempts or process death **do not auto-refund**.
  Known terminal success is settled; an ambiguous outcome remains a visible
  reserved liability pending reconciliation. No TTL creates free service.
- The outbox recovers already persisted successful billing events, not unknown
  upstream outcomes. A crash after forwarding but before persisting a success
  still needs operational reconciliation. There is deliberately no guessed
  success/timeout settlement or automatic write-off.
- A final charge exceeding the ceiling fails with ErrExceeded and retains funds.
  This is an invariant incident, NOT a valid normal policy or evidence that an
  already served over-ceiling request was funded. Integrated searches pin the
  exact charge to make this impossible under their declared billing contract.
- The canonical JSON billing receipt, integer actual/pool split, dedup and
  counter effects are atomic. The existing presentation `usage_logs` write
  remains best effort OUTSIDE this transaction; do not describe it as atomic.
- Conservative supported money range: 0..92233720368.54775807; larger values fail
  closed. DB/schema/price failures return errors; Redis outage cannot grant money.

## Remaining precise release decisions / work

1. **Do not enable strict-site claims while unreserved legacy gateways run.**
   Finish proven ceilings per endpoint, with local/provider-enforced finite
   input/output/reasoning/tool/cache/multimodal budgets and a pinned worst-case
   price across allowed routes. Merely trusting the request's max_tokens, a model
   name, or a cost-estimator digest is not sufficient.
2. The least disruptive next implementation is bounded text-only requests on
   explicitly known model/pricing contracts: reject remote expanded input,
   unbounded server tools, unsupported media/live/cache-storage types in that
   strict subset, and impose a finite output+reasoning budget. Use validated
   model/context/tokenization metadata rather than inventing a byte/token ratio.
   Image-count billing can follow only where token supplements are excluded or
   separately bounded. Unknown model/route pricing must reject BEFORE forwarding.
3. Until those integrations exist, the only immediately provable blanket policy
   would reject ALL unsupported metered paths. That would substantially disable
   chat/media/etc.; this draft does NOT silently impose that policy. The precise
   availability choice is either keep admission closed pending implementation,
   or explicitly accept a temporary supported-endpoint allowlist. A DB trigger
   or leaving legacy uncovered-cost collection running is not a third solution.
4. Ambiguous upstream outcomes require an operational reconciliation source /
   operator workflow. Do not silently turn retained holds into permanent charges
   or refund them based only on time. Surface reservation/outbox age and retry
   counts before any rollout. Independent security/accounting review is needed.

## Production audit privacy and correction script

Actual affected identities, amounts and before/after balances remain in the
private operational report, never this public repo. The reviewed cohort has
sufficient OWN allocated team funds and unambiguous membership. Unrelated
nonmember debt and historical negative team quotas are excluded. Never debit an
owner again, infer a different team, forgive debt or create a top-up.

`scripts/operations/repair_team_personal_debt_20261120.sql` defaults to READ ONLY.
It is a psql operation, not an automatic migration, and has NOT been executed
against production. It requires explicit reviewed IDs/team/batch, re-reads current
amounts, rejects drift/insufficiency, locks and transfers matching debt from own
team allocation into personal balance in ONE transaction. Exact before/after
journal proves per-user and aggregate conservation. Repeating a committed batch
is idempotent; new debt needs a new reviewed batch.

Only after all gateway blockers are resolved AND deployment is approved:

1. Keep admission closed; drain/reconcile old requests AND asynchronous charges.
   Create and verify `pg_dump -Fc` backup, retain the old binary. An HTTP handler
   finishing alone is not evidence billing drained.
2. Re-run the read-only audit and review changed membership, newly indebted
   members, insufficient allocations and pre-existing negative team wallets.
3. Install/start the approved new binary while maintenance stays active; schema
   migrations must be present before any strict route or recovery worker runs.
   Follow the established `docker cp /app/sub2api`, correct `sub2api` ownership,
   chmod755 and docker restart procedure. NEVER compose recreate (old base image).
   Use `--version` only. No tags/releases/deployments are performed by this PR.
4. With explicit maintenance confirmation, execute psql with `-v apply=true`,
   `-v maintenance_confirmed=true`, `-v 'reviewed_ids={...}'`,
   `-v reviewed_team_id=...`, `-v batch_id=...` from the private review. Abort on
   drift/ambiguity. Re-read exact totals/journal and invalidate affected billing /
   auth caches after commit. Keep maintenance active until all checks pass.
5. Rollback also keeps admission closed: an old binary does not understand these
   reservations and cannot safely resume billing while holds exist. Preserve
   ledger/journals and reconcile; never drop safeguards just to bypass failures.

No version, team functionality, recharge multiplier/address/minimum/fee,
CoinGecko fail-closed price lock, auth snapshot128, real upstream-model policy or
exact self-host SSRF allowlisting is intentionally changed.

## Reproducible isolated tests (no paid upstream traffic)

```sh
cd backend
export WALLETGUARD_TEST_DSN='postgres://box@127.0.0.1:55439/walletguard_test?sslmode=disable'
go test -race -count=3 ./internal/pkg/walletguard ./internal/testutil/walletdb
go test -tags=unit -race -count=3 ./internal/handler ./internal/repository \
  -run 'StrictWallet|ReservedWallet'
go test -tags=unit ./internal/service ./internal/repository \
  ./internal/server/middleware ./internal/handler
```

Tests require 127.0.0.1 and database name walletguard_test, with isolated schemas.
Without the DSN PostgreSQL tests SKIP; that is not integration verification. The
CI workflow provides PostgreSQL17 and runs actual Gin handlers, mock HTTP upstream,
real repository/Apply/ApplyBatch/outbox and the ledger with race detection.
Coverage includes 0/.1/.10000001/2, all four funds modes, concurrent multi-connection
stores, insufficient funds, duplicate/conflicting settlement, over-ceiling error,
transaction rollback/restart recovery, predispatch failure, explicit rejection,
failover, SSE interruption, cancellation/unknown holds, mid-flight price changes,
stale/failed Redis and no second cache deduction. No customer API keys, real
orders, external billable requests or production writes are used.
