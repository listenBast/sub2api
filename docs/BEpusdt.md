# USDT recharge with self-hosted BEpusdt / 自托管 USDT 充值

**Acceptance boundary:** automated code/mock contract tests, not real settlement.
Publishing a sub2api release does **not** install BEpusdt, configure addresses or
update a running website. No production deployment, real transfer, wallet-app
scan compatibility test or browser/visual acceptance has been performed here.

## 给站长的最短操作指南（发布后自行测试）

**不是只填地址就能自动到账。** sub2api 管理订单和余额；独立运行的 BEpusdt
负责扫描区块链、确认转账并发送签名通知。两边都运行且配置正确才有自动充值。
发布软件和更新你的网站是两件事：本次发布不包含生产服务器升级；更新生产须另行批准。

1. **先准备隔离测试环境。** 安装本次 sub2api 发布包，在自己的测试站验收后，
   再安排经批准的生产升级。先不要开启对外 USDT 收款。
2. **单独安装 BEpusdt。** 按官方版本对应的
   [Docker 安装文档](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/docs/docker/docker.md)
   或 [Linux 安装文档](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/docs/linux/install.md)
   部署并固定经过验收的版本/镜像摘要，不要盲目跟随 `latest`。
   保护管理后台和 API 令牌、持久化数据并备份、保持服务器时钟同步、配置 HTTPS。
   核对所启用链的 RPC 可用性、正确的 USDT 合约、确认数和重组处理。
   此集成只需公开收款地址；不要给网站、机器人或聊天提供钱包私钥/助记词。
3. **核对收款引擎。** 在 BEpusdt 设置传统/精确金额匹配，**不能用前缀或修约匹配**。
   核实当前版本确实监控原生创建交易接口传入的地址。网站的“已核对精确匹配”
   是管理员确认，不会远程读取引擎设置。不要在待付订单存在时更换实例、令牌或匹配规则。
4. **网站系统管理员 → 系统设置 → 支付。** 启用 BEpusdt 服务商类型，创建一个
   暂停用的实例。分别填写 BSC/BEP20、Polygon PoS、TRON/TRC20、Ethereum/ERC20
   的收款地址；只勾选准备好且已核对地址/网络/代币的链。地址在这个网站里保存和修改，
   不需要发到聊天，也不是写死在代码中。没有地址的链不要启用。
5. **填实例和财务配置。** 填 BEpusdt 的 HTTPS 基础地址和 API 令牌；回调是
   `https://你的网站域名/api/v1/payment/webhook/usdt`，返回页是
   `https://你的网站域名/payment/result`。基础地址不应带创建/查单接口路径。
   管理员自行确定正数固定“人民币/1 USDT”汇率和人民币最低支付额；没有默认财务策略。
   还须核对已有充值倍率/手续费和交易所最低入金/提现手续费，勿把余额金额或人民币金额
   当成应付 USDT 数量。检查完成后，才启用实例及支付总开关。
6. **保证双向连通。** 网站服务端要能访问该 BEpusdt 的创建、查单和取消接口；
   BEpusdt 要能向网站回调地址 POST JSON。不能让登录页、验证码、WAF 挑战或反向代理
   改写回调正文；API 令牌不能放进前端、URL、截图或公开日志。
7. **用普通测试用户创建一笔小额订单。** 先记下订单号和余额。金额应满足你自己
   配置的最低额及收款钱包/交易所的最低入金要求。逐项核对页面的网络、USDT、完整地址、
   精确数量和倒计时。**二维码只包含地址，不包含链、代币或金额**；扫码后仍需手动核对
   网络、选择 USDT、填写精确数量，并确保扣除提现手续费后的实收数量一致。
   不保证币安或其它应用自动选链/填金额；不兼容扫码时用复制地址/数量按钮。
8. **你自行确认后只付款一次。** 应在倒计时结束前完成，等待区块确认和签名通知；
   网站订单应完成、余额只增加一次。不要因页面暂未刷新而重复转账。
   再由有权限的运维通过 BEpusdt 该版本支持的原订单重通知机制重投同一签名通知，
   核对订单不重复充值；刷新/浏览器返回本身不能触发充值。
   每条准备开放的链均须单独验收，先验收一条也可以，其余保持关闭。

**不要用真实资金故意测试跨链、少付、多付、取消后转账或晚付。** 这些先在隔离 mock
环境验证。此类真实异常订单保守地进入人工核查，不承诺自动补单或自动退款。
改地址后，新订单使用新地址，旧待付订单继续使用创建时地址；旧地址/链扫描必须保留
到旧订单结清。不要把删除旧实例或重新建单当成恢复支付的方法。

若“已转账但未充值”，先停止重复付款，按订单号核对：链/USDT 合约/地址/精确实收数量、
交易确认、订单是否过期或已取消、BEpusdt 原订单状态及重通知、网站同实例查单是否成功、
网站订单审计记录。上游查单失败、金额/网络/地址不符、晚到通知都不会通过浏览器点击
“已支付”强行入账。不得通过关闭验签、伪造回调或只相信查单结果来绕过检查。

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

- Users choose a named USDT network. The website displays the **order-bound
  receiving address, network + USDT, exact USDT quantity (a decimal string), copy
  buttons and expiry countdown** on desktop and mobile. The QR encodes **only
  that receiving address**, not a cashier URL, wallet URI, chain ID, token contract
  or amount. This matches BEpusdt's inspected official cashier, which calls
  `qrcode({text: paymentAddress})`. The create API itself returns `token` (address),
  `actual_amount` (USDT quantity), `trade_type`, and `payment_url` (hosted cashier),
  **not** a pre-rendered QR image or wallet payment URI.
- After scanning, users must manually select the displayed network and USDT and
  enter the exact quantity, ensuring withdrawal fees do not reduce the amount
  received. Address-only QR cannot distinguish the three EVM networks. Never
  assume Binance or any other wallet/exchange auto-selects a network, token or
  amount; scan compatibility must be checked on the actual app/version. Copy
  address/quantity is available when scanning is unsupported. Do not send a
  chain's native token or transfer after expiry/cancellation. CNY settlement and
  balance-credit amounts are not the quantity of USDT to transfer.
- Creation persists the earlier local/gateway deadline, never extends it. The
  nonsecret `crypto_payment` response allowlists method/asset/address/amount only;
  provider credentials and the full snapshot are not exposed. Recovery refreshes
  USDT instructions from the authenticated immutable order snapshot (not current
  admin settings or cached local transfer data), failing closed on query errors.
  Existing polling, result pages, cancellation and terminal states remain shared.
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

- [Official cashier: address-only QR implementation](https://github.com/v03413/bepusdt/blob/8a91be6a155c28cc07db18aaad0ecd06aae71582/static/checkout/official/assets/js/checkout.js)
