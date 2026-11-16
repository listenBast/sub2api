//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func bepusdtTestConfig() map[string]string {
	return map[string]string{"apiBase": "https://crypto.example", "apiToken": "test-only-token", "notifyUrl": "https://merchant.example/api/v1/payment/webhook/usdt", "returnUrl": "https://merchant.example/payment/result", "rate": "7", "minimumAmount": "1", "exactMatchConfirmed": "yes", "usdt_bep20Address": "0x1111111111111111111111111111111111111111"}
}
func TestBEpusdtAdminConfigPersistenceAndAddressRotation(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{entClient: client}
	cfg := bepusdtTestConfig()
	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{ProviderKey: payment.TypeBEpusdt, Name: "USDT", Config: cfg, SupportedTypes: []string{"usdt_bep20"}, Enabled: true})
	require.NoError(t, err)
	stored, err := svc.ListProviderInstancesWithConfig(ctx)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, cfg["usdt_bep20Address"], stored[0].Config["usdt_bep20Address"])
	require.NotContains(t, stored[0].Config, "apiToken")
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusPending, time.Now())
	id := strconv.FormatInt(inst.ID, 10)
	snapshot := map[string]any{"bepusdt_address": cfg["usdt_bep20Address"]}
	_, err = client.PaymentOrder.UpdateOneID(order.ID).SetProviderInstanceID(id).SetProviderSnapshot(snapshot).Save(ctx)
	require.NoError(t, err)
	next := "0x2222222222222222222222222222222222222222"
	_, err = svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{Config: map[string]string{"usdt_bep20Address": next, "apiToken": ""}})
	require.NoError(t, err)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, cfg["usdt_bep20Address"], reloaded.ProviderSnapshot["bepusdt_address"])
	saved, err := client.PaymentProviderInstance.Get(ctx, inst.ID)
	require.NoError(t, err)
	config, err := svc.decryptConfig(saved.Config)
	require.NoError(t, err)
	require.Equal(t, next, config["usdt_bep20Address"])
	require.Equal(t, cfg["apiToken"], config["apiToken"])
	_, err = svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{Config: map[string]string{"rateMode": "coingecko"}})
	require.ErrorContains(t, err, "pending orders")
	_, err = svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{Config: map[string]string{"apiBase": "https://different.example"}})
	require.ErrorContains(t, err, "pending orders")
	for _, bad := range []map[string]string{{"usdt_bep20Address": "not-an-address"}, {"usdt_bep20Address": ""}, {"rate": ""}, {"minimumAmount": ""}} {
		_, err = svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{Config: bad})
		require.Error(t, err)
	}
	refund := true
	_, err = svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{RefundEnabled: &refund})
	require.ErrorContains(t, err, "automatic refunds")
	// Stopping new orders must not prevent existing pinned callbacks.
	off := false
	_, err = svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{Enabled: &off, SupportedTypes: []string{}})
	require.NoError(t, err)
	paymentSvc := &PaymentService{entClient: client, configService: svc, loadBalancer: newWebhookProviderTestLoadBalancer(client)}
	prov, err := paymentSvc.GetWebhookProvider(ctx, payment.TypeBEpusdt, order.OutTradeNo)
	require.NoError(t, err)
	require.Equal(t, payment.TypeBEpusdt, prov.ProviderKey())
}
func TestBEpusdtChainEnablementRequiresAddress(t *testing.T) {
	cfg := bepusdtTestConfig()
	require.NoError(t, validateBEpusdtInstance(payment.TypeBEpusdt, "usdt_bep20", cfg, true, false))
	for _, method := range []string{"usdt_polygon", "usdt_trc20", "usdt_erc20", "alipay"} {
		require.Error(t, validateBEpusdtInstance(payment.TypeBEpusdt, method, cfg, true, false))
	}
	require.NoError(t, validateBEpusdtInstance(payment.TypeBEpusdt, "usdt_trc20", cfg, false, false))
	require.Error(t, validateBEpusdtInstance(payment.TypeBEpusdt, "", cfg, true, false))
}
func TestBEpusdtPaymentBindingAndDuplicateFulfillment(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusPending, time.Now())
	metadata := map[string]string{"bepusdt_instance": "7", "bepusdt_api_base": "https://crypto.example", "bepusdt_trade_id": "trade-1", "bepusdt_order_id": order.OutTradeNo, "bepusdt_method": "usdt_bep20", "bepusdt_address": bepusdtTestConfig()["usdt_bep20Address"], "bepusdt_amount": fmt.Sprint(order.PayAmount), "bepusdt_actual_amount": "2", "currency": "CNY"}
	snapshot := map[string]any{"schema_version": 2, "provider_key": payment.TypeBEpusdt, "provider_instance_id": "7"}
	for k, v := range metadata {
		snapshot[k] = v
	}
	order, err := client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeBalance).ClearPlanID().ClearSubscriptionGroupID().ClearSubscriptionDays().SetPaymentType("usdt_bep20").SetProviderKey(payment.TypeBEpusdt).SetProviderInstanceID("7").SetProviderSnapshot(snapshot).SetExpiresAt(time.Now().Add(time.Hour)).ClearPaidAt().Save(ctx)
	require.NoError(t, err)
	credited := 0.0
	repo := &paymentFulfillmentRedeemRepo{}
	users := &mockUserRepo{getByIDUser: &User{ID: order.UserID}}
	users.updateBalanceFn = func(_ context.Context, id int64, amount float64) error { credited += amount; return nil }
	redeem := NewRedeemService(repo, users, nil, &paymentFulfillmentRedeemCacheStub{}, nil, client, nil, nil)
	svc := &PaymentService{entClient: client, redeemService: redeem, userRepo: users}
	notification := func(m map[string]string) *payment.PaymentNotification {
		return &payment.PaymentNotification{OrderID: order.OutTradeNo, TradeNo: "trade-1", Amount: order.PayAmount, Status: payment.NotificationStatusSuccess, Metadata: m}
	}
	for _, key := range []string{"bepusdt_instance", "bepusdt_api_base", "bepusdt_trade_id", "bepusdt_order_id", "bepusdt_method", "bepusdt_address", "bepusdt_amount", "bepusdt_actual_amount", "currency"} {
		bad := map[string]string{}
		for k, v := range metadata {
			bad[k] = v
		}
		bad[key] = "wrong"
		require.Error(t, svc.HandlePaymentNotification(ctx, notification(bad), payment.TypeBEpusdt), key)
	}
	require.Zero(t, credited)
	for _, status := range []string{OrderStatusCancelled, OrderStatusExpired} {
		_, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(status).Save(ctx)
		require.NoError(t, err)
		require.Error(t, svc.HandlePaymentNotification(ctx, notification(metadata), payment.TypeBEpusdt))
		require.Zero(t, credited)
	}
	_, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusPending).SetExpiresAt(time.Now().Add(-time.Second)).Save(ctx)
	require.NoError(t, err)
	require.Error(t, svc.HandlePaymentNotification(ctx, notification(metadata), payment.TypeBEpusdt))
	_, err = client.PaymentOrder.UpdateOneID(order.ID).SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.HandlePaymentNotification(ctx, notification(metadata), payment.TypeBEpusdt))
	require.InDelta(t, order.Amount, credited, 1e-8)
	require.NoError(t, svc.HandlePaymentNotification(ctx, notification(metadata), payment.TypeBEpusdt))
	require.InDelta(t, order.Amount, credited, 1e-8)
	require.Len(t, repo.useCalls, 1)
	result, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, result.Status)
}

func TestBEpusdtCheckoutDetailsUseImmutableSnapshot(t *testing.T) {
	key := payment.TypeBEpusdt
	o := &dbent.PaymentOrder{PaymentType: "usdt_bep20", ProviderKey: &key, ProviderSnapshot: map[string]any{
		"bepusdt_method": "usdt_bep20", "bepusdt_address": bepusdtTestConfig()["usdt_bep20Address"], "bepusdt_actual_amount": "1.234567890123456789",
		"apiToken": "never-return-snapshot", "bepusdt_api_base": "https://private.example",
	}}
	details := PaymentOrderCryptoDetails(o)
	require.NotNil(t, details)
	require.Equal(t, "1.234567890123456789", details.Amount)
	require.Equal(t, "USDT", details.Asset)
	encoded, err := json.Marshal(details)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "apiToken")
	require.NotContains(t, string(encoded), "never-return-snapshot")
	require.NotContains(t, string(encoded), "private.example")
	require.Equal(t, o.ProviderSnapshot["bepusdt_address"], details.Address)
	require.Nil(t, PaymentOrderCryptoDetails(nil))
	for _, invalid := range []string{"0", "-1", "NaN", ""} {
		o.ProviderSnapshot["bepusdt_actual_amount"] = invalid
		require.Nil(t, PaymentOrderCryptoDetails(o))
	}
	o.ProviderSnapshot["bepusdt_actual_amount"] = "2"
	o.PaymentType = "usdt_polygon"
	require.Nil(t, PaymentOrderCryptoDetails(o))
	o.PaymentType = "usdt_bep20"
	o.ProviderKey = nil
	require.Nil(t, PaymentOrderCryptoDetails(o))
}

func TestBEpusdtAutomaticConfigAndQuoteSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{entClient: client}
	cfg := bepusdtTestConfig()
	delete(cfg, "rate")
	cfg["rateMode"] = "coingecko"
	_, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{ProviderKey: payment.TypeBEpusdt, Name: "Auto USDT", Config: cfg, SupportedTypes: []string{"usdt_bep20"}, Enabled: true})
	require.NoError(t, err)
	cfg["lockedRate"] = "6.6623951423103795"
	cfg["rateSource"] = "coingecko"
	cfg["rateQuotedAt"] = "2026-11-16T16:33:56Z"
	cfg["rateFetchedAt"] = "2026-11-16T16:34:04Z"
	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{ProviderKey: payment.TypeBEpusdt, Config: cfg}, CreateOrderRequest{})
	require.Equal(t, "6.6623951423103795", snapshot["bepusdt_rate"])
	cfg["lockedRate"] = "7"
	require.Equal(t, "6.6623951423103795", snapshot["bepusdt_rate"])
	require.Equal(t, "coingecko", snapshot["bepusdt_rate_source"])
	require.NotContains(t, snapshot, "apiToken")
}
