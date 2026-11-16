# USDT recharge with self-hosted BEpusdt / 自托管 USDT 充值

**Status:** code + mock contract tests only. Not deployed, no real address configured,
no real payment made, and no browser/visual acceptance performed.

## Website administrator setup / 网站系统管理员设置

In **System settings → Payment → BEpusdt · USDT → Create/edit provider**:

1. Enable the BEpusdt payment provider type. Create a **disabled** instance first.
2. Set the HTTPS BEpusdt base URL (origin or reverse-proxy prefix, no API endpoint,
   credentials, query or fragment), API token, and public HTTPS website callback
   and return base URLs. Callback: `/api/v1/payment/webhook/usdt`;
   browser return: `/payment/result`. The token is omitted from admin read responses;
   an empty token on edit keeps the stored token. This uses the existing payment
   configuration storage (currently plaintext JSON in DB); protect/encrypt backups
   and restrict database and administrator access. It does not introduce a vault.
3. **Enter/edit each public receiving address in this website**, independently:

   | Website field | Network | Native BEpusdt `trade_type` |
   |---|---|---|
   | BSC / BEP20 | BNB Smart Chain | `usdt.bep20` |
   | Polygon PoS | Polygon PoS (not zkEVM) | `usdt.polygon` |
   | TRON / TRC20 | TRON | `usdt.trc20` |
   | Ethereum / ERC20 | Ethereum mainnet | `usdt.erc20` |

   No addresses are compiled into the application. No private key, seed phrase,
   signing authority, or withdrawal permission is needed. EVM fields require
   `0x` + 40 hex digits and reject the zero address; TRON requires its T/base58check
   format and checksum. Format checks are **not** proof of ownership, exchange support, or
   correct network: the administrator must verify the exchange/wallet's deposit
   instructions (including supported token/network and minimum deposit).
4. Select only chains with valid addresses in the instance's payment-type chips.
   Unselect a chain to stop **new** orders for it, or disable the whole instance.
   Address edits, chain disablement, and instance disablement do not rewrite
   existing orders or their bound addresses. Keep the same BEpusdt instance,
   token, matching policy, rate/minimum configuration and old-address scanning
   operational until pending orders finish. The backend blocks changes to
   runtime identity/required configuration and deletion while orders are in
   progress. Editing a receiving address is deliberately allowed during this
   period. Clearing an enabled chain's address is rejected: unselect it first.
5. Set an **explicit positive fixed CNY/USDT rate** and **minimum payment in CNY**.
   There are no defaults, market-rate fallbacks, or zero-amount orders. CNY is the
   existing payment settlement currency; existing recharge multipliers, fees,
   bonuses, subscription conversion and fulfillment rules continue to apply.
   The rate is CNY per 1 USDT, not its inverse. Rate expressions such as `~1.02`
   are intentionally unsupported, since upstream can silently fall back.
6. Configure BEpusdt's **traditional/exact amount matching**, not prefix or rounded
   matching, and explicitly confirm that prerequisite in the website's field.
   This confirmation is an operator attestation, **not a remote configuration
   probe**: the merchant API does not return the actual scanned transfer amount
   in ordinary-mode notifications. We necessarily trust the configured engine's
   scanner/matching policy. Never enable prefix/rounding tolerances for this
   integration. Configure node/RPC providers and confirmation depths suitable for
   each enabled chain in BEpusdt. Verify monitoring of explicitly supplied
   addresses, token contracts, confirmation/reorg behavior, callback retries and
   fee/rounding/granularity in staging before enabling real money.
7. Enable the instance only after those fields and operational prerequisites are
   complete. Missing rate/minimum/matching confirmation/selected-chain address
   rejects enablement. There is no automatic on-chain refund facility. Automatic
   refund switches are rejected/hidden for this provider.

**中文要点：** 四链收款地址在 sub2api 网站系统管理员后台分别保存和修改，
不是要求发给机器人，也不是只在 BEpusdt 后台展示。选择已配置的链才能启用。
新订单将所选链的 `address` 实际传给原生创建交易接口；返回地址必须匹配，
随后把地址、网络、交易号和精确金额写入订单快照。改地址仅影响新订单，
旧订单继续校验旧地址；停用链/实例也不应丢弃旧订单通知。
上线前必须由管理员自行配置公开地址、固定汇率、最低额并核对引擎精确匹配。

## Effective receiving address integration (not display-only)

This uses signed `POST /api/v1/order/create-transaction` with explicit
`trade_type`, `address`, positive numeric `amount`, `fiat=CNY`, `rate`, `timeout`,
`notify_url`, and `redirect_url`. In the inspected upstream source,
`model/trade.go:CreateTradeOrder/createTrade` builds `NewWallet(p.Address,
p.TradeType)` and replaces the available-wallet list with that supplied address.
The scanner matches transfers against pending orders by their saved match address
and trade type (`task/transfer.go:getReceivableOrders/orderTransferMatch`). It is
**not** merely a local UI field and does not require copying that address
manually into an unrelated gateway wallet setting. BEpusdt must still have the
network scanner/RPC operational. Test this behavior against your deployed,
version-pinned BEpusdt build; do not assume other forks/versions behave identically.
The response's order ID, trade ID, network, fiat, amount and receiving address are
checked before checkout is returned; a mismatch fails closed.

Do not reuse merchant order IDs, recreate pending gateway orders out of band,
change their payment network, or reuse one BEpusdt instance across untrusted
merchants. The API token signs *all* merchant traffic in that gateway; use an
isolated instance/token per trust boundary. The native API is **not** the Epay
`api.php?act=order` API.

## User checkout and payment proof

- Users choose a named USDT network. Existing recharge UI renders a QR of the
  **hosted cashier URL** or opens that cashier on mobile. The cashier displays
  the exact USDT quantity and selected network/address. The QR is not a bare
  address next to a misleading CNY amount. Existing order polling, result page,
  cancellation and recovery mechanisms remain in use.
- Native JSON webhook signatures follow upstream sorted scalar MD5+API-token
  canonicalization, with constant-time comparison. Unknown/duplicate/nested
  fields, trailing content, delimiter smuggling, missing IDs/hash, nonpositive
  amounts and malformed decimals are rejected. No token is logged.
- Only a **signed successful** callback plus a successful same-instance
  `POST /api/v1/pay/info` cross-check may fulfill an order. Info is unsigned and
  payer-facing: it is **never sufficient payment proof on its own**. A browser
  redirect cannot credit an account. Generic query reconciliation refuses to
  promote a paid-looking Info response without the signed callback.
- Info must agree with the callback's order/trade IDs, status, CNY and exact
  fiat/crypto amounts and address, and identify one of the four allowed USDT
  chains. The persisted creation snapshot must then match instance, base URL,
  trade/order IDs, chain, address, currency and exact decimal amounts. Monetary
  authorization compares decimal strings/decimals; conversion to float exists
  only at the existing payment interface adapter, not for crypto arithmetic.
- Upstream query failures, fingerprint/permission denials and malformed replies
  fail closed and return a retryable non-success webhook response. There is no
  fallback to an admin endpoint, old API, forged payer fingerprint, or unsigned
  query credit. Fixed-network `create-transaction` avoids the selectable
  cashier/update-order fingerprint-binding flow. Validate access with the
  version actually deployed; investigate failed confirmations rather than
  bypassing them.
- Existing conditional PENDING→PAID update, fulfillment lease, and transactional,
  idempotent redeem-code balance credit provide duplicate-callback/crash retry
  protection. Completed orders do not recharge again. Existing personal/team
  balance functionality is unchanged.
- Cancelled/expired orders, callback arrival after the local expiry, mismatching
  amounts or any binding conflict are **not automatically credited**; they need
  manual investigation. Even a legitimately on-time transfer with a delayed
  callback may require manual review under this conservative rule. A dedicated
  order audit entry records rejected USDT bindings/late callbacks. Under/overpay
  handling is not a promise to accept such transfers: configure exact matching.
  Refunds require a separately approved/manual process; no chain transfer is
  performed by sub2api.

## Deploy/acceptance checklist (not performed here)

- Back up the application DB, then deploy reviewed application code and a pinned
  BEpusdt build **only with separate user approval**. No new DB migration is
  needed: existing provider config and order `provider_snapshot` JSON are used.
- Put BEpusdt behind HTTPS, restrict admin/API token access, preserve its DB,
  allow outbound sub2api→Info/create/cancel and inbound gateway→webhook traffic.
  Never expose node credentials or the token in browser config/logs.
- Check all four network/address choices, dark/light and narrow screens, empty
  address validation, enabled/disabled chips, address rotation with a pending
  order, cashier/QR, mobile return and result polling in a browser. Automated
  component tests do **not** replace this visual review.
- Conduct separately approved low-value real settlement tests for every enabled
  chain, exact amount, duplicate/retried callbacks, cancellation, expiry,
  delayed callback, under/overpay, query outage and wrong-instance callbacks.
- Agree operational policies for late/short/excess payments and manual refunds,
  and choose rate, minimum, network fees and confirmation depths. None was
  chosen on the user's behalf. Only approved correct/exact/on-time payments
  take the normal auto-credit path.

## Local UI preview

From this worktree run `pnpm --dir frontend dev --host 0.0.0.0 --port 5173` and
use a **local/staging** backend only (`VITE_DEV_PROXY_TARGET=http://localhost:8080` / Vite proxy as configured
in `frontend/vite.config.ts`). Open `/admin/settings` → Payment, or the recharge
page. Use local test administrator credentials; do not connect this preview to
production. For compiled preview: `pnpm --dir frontend build`, then
`pnpm --dir frontend preview --host 0.0.0.0 --port 4173`.
No preview is started automatically and no visual check is claimed.

## Verified public references

Protocol/source revision inspected: upstream `v03413/bepusdt`
`8a91be6a155c28cc07db18aaad0ecd06aae71582` (2026-11-16 Asia/Shanghai).

- [Native API/signature docs](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/docs/api/api.md)
- [Native create/Info/cancel handlers](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/app/handler/epusdt/epusdt.go)
- [Explicit address wallet selection](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/app/model/trade.go)
- [Notification payload and signer](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/app/task/notify/notify.go)
- [Signing implementation](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/app/utils/utils.go)
- [Pending-order address/chain/amount matching](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/app/task/transfer.go)
- [Exact/prefix/rounded matching](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/docs/payment-match-mode/README.md)
- [Supported network identifiers](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/docs/trade-type.md)
