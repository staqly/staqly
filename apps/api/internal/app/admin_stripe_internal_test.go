package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type stripeFake struct {
	forms map[string]url.Values // "METHOD path" -> last form
	query url.Values
}

func adminStripeTest(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*Service, Principal, *stripeFake) {
	t.Helper()
	s, p, _ := publishTest(t)
	f := &stripeFake{forms: map[string]url.Values{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.forms[r.Method+" "+r.URL.Path] = r.PostForm
		f.query = r.URL.Query()
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	s.Cfg.Mode = ModeCloud
	s.Cfg.Billing = BillingConfig{SecretKey: "sk_test", WebhookSecret: "whsec", APIBase: srv.URL}
	return s, p, f
}

func TestAdminPaymentsBillingDisabled(t *testing.T) {
	s, _, _ := publishTest(t)
	_, err := s.AdminListPayments(context.Background(), "", "", "", 10)
	if e, ok := err.(*Error); !ok || e.Code != "billing_disabled" || e.Status != 409 {
		t.Fatalf("got %v", err)
	}
}

func TestAdminPaymentsFailedFilterAndMapping(t *testing.T) {
	s, p, f := adminStripeTest(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"has_more": true, "data": []map[string]any{
			{"id": "in_1", "customer": "cus_known", "status": "open", "attempt_count": 2, "created": 1700000000},
			{"id": "in_2", "customer": "cus_known", "status": "open", "attempt_count": 0},
			{"id": "in_3", "customer": "cus_other", "status": "uncollectible"},
			{"id": "in_4", "customer": "cus_known", "status": "paid"},
			{"id": "in_5", "customer": "cus_known", "status": "draft", "attempt_count": 1},
		}})
	})
	if _, err := s.DB.Exec(`UPDATE workspaces SET stripe_customer_id = 'cus_known' WHERE id = ?`, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	pg, err := s.AdminListPayments(context.Background(), "failed", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 2 || pg.Items[0].ID != "in_1" || pg.Items[1].ID != "in_3" {
		t.Fatalf("items %+v", pg.Items)
	}
	if pg.Items[0].WorkspaceID != p.WorkspaceID || pg.Items[0].WorkspaceName == "" || pg.Items[1].WorkspaceID != "" {
		t.Errorf("mapping %+v", pg.Items)
	}
	if pg.Items[0].Created != time.Unix(1700000000, 0).UTC().Format(time.RFC3339) || pg.NextCursor != "in_5" {
		t.Errorf("created %q cursor %q", pg.Items[0].Created, pg.NextCursor)
	}
	if f.query.Get("status") != "" {
		t.Errorf("failed must not be sent as a Stripe status")
	}
	if _, err := s.AdminListPayments(context.Background(), "paid", p.WorkspaceID, "in_0", 10); err != nil {
		t.Fatal(err)
	}
	if f.query.Get("status") != "paid" || f.query.Get("customer") != "cus_known" || f.query.Get("starting_after") != "in_0" {
		t.Errorf("query %v", f.query)
	}
}

func TestAdminPaymentsWorkspaceWithoutCustomer(t *testing.T) {
	s, p, _ := adminStripeTest(t, func(w http.ResponseWriter, r *http.Request) { t.Error("Stripe must not be called") })
	pg, err := s.AdminListPayments(context.Background(), "", p.WorkspaceID, "", 10)
	if err != nil || len(pg.Items) != 0 {
		t.Fatalf("%v %+v", err, pg)
	}
}

func TestAdminPromoCreateValidation(t *testing.T) {
	s, _, _ := adminStripeTest(t, func(w http.ResponseWriter, r *http.Request) { t.Error("Stripe must not be called") })
	future := time.Now().Add(48 * time.Hour).Format("2006-01-02")
	ok := AdminPromoInput{Code: "SAVE10", PercentOff: 10, Duration: "once"}
	cases := map[string]func(*AdminPromoInput){
		"both off types":     func(i *AdminPromoInput) { i.AmountOff = 500; i.Currency = "usd" },
		"no off type":        func(i *AdminPromoInput) { i.PercentOff = 0 },
		"bad code":           func(i *AdminPromoInput) { i.Code = "a!" },
		"repeating no month": func(i *AdminPromoInput) { i.Duration = "repeating" },
		"percent 101":        func(i *AdminPromoInput) { i.PercentOff = 101 },
		"amount no currency": func(i *AdminPromoInput) { i.PercentOff = 0; i.AmountOff = 500 },
		"bad duration":       func(i *AdminPromoInput) { i.Duration = "weekly" },
		"past expiry":        func(i *AdminPromoInput) { i.ExpiresAt = "2020-01-01" },
	}
	_ = future
	for name, mut := range cases {
		in := ok
		mut(&in)
		if _, err := s.AdminCreatePromotionCode(context.Background(), Admin{Email: "a@b.c"}, in); err == nil {
			t.Errorf("%s: expected error", name)
		} else if e, isE := err.(*Error); !isE || e.Code != "validation_failed" {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestAdminPromoCreateAndDeactivate(t *testing.T) {
	s, _, f := adminStripeTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/coupons":
			_, _ = w.Write([]byte(`{"id":"cp_1"}`))
		case strings.HasSuffix(r.URL.Path, "/promo_1"):
			_, _ = w.Write([]byte(`{"id":"promo_1","code":"SAVE","active":false,"promotion":{"type":"coupon","coupon":{"id":"cp_1","percent_off":10}}}`))
		default:
			// As Stripe API 2026-09-30 answers (the sandbox, 2026-11-13): `coupon` is an unknown parameter.
			if r.Form.Get("coupon") != "" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":{"code":"parameter_unknown","param":"coupon","type":"invalid_request_error"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"promo_1","code":"SAVE10","active":true,"max_redemptions":5,"promotion":{"type":"coupon","coupon":{"id":"cp_1","amount_off":null,"percent_off":10,"duration":"repeating","duration_in_months":3}}}`))
		}
	})
	ctx := context.Background()
	exp := time.Now().Add(48 * time.Hour).Format("2006-01-02")
	got, err := s.AdminCreatePromotionCode(ctx, Admin{Email: "a@b.c"}, AdminPromoInput{
		Code: "save10", PercentOff: 10, Duration: "repeating", DurationInMonths: 3, MaxRedemptions: 5, ExpiresAt: exp})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "promo_1" || got.Coupon.ID != "cp_1" || got.Coupon.PercentOff != 10 || got.Coupon.DurationInMonths != 3 {
		t.Errorf("got %+v", got)
	}
	c := f.forms["POST /v1/coupons"]
	if c.Get("percent_off") != "10" || c.Get("duration") != "repeating" || c.Get("duration_in_months") != "3" {
		t.Errorf("coupon form %v", c)
	}
	pc := f.forms["POST /v1/promotion_codes"]
	if pc.Get("promotion[type]") != "coupon" || pc.Get("promotion[coupon]") != "cp_1" || pc.Get("expand[]") != "promotion.coupon" || pc.Get("code") != "SAVE10" || pc.Get("max_redemptions") != "5" || pc.Get("expires_at") == "" {
		t.Errorf("promo form %v", pc)
	}
	d, err := s.AdminDeactivatePromotionCode(ctx, Admin{Email: "a@b.c"}, "promo_1")
	if err != nil || d.Active || d.Coupon.PercentOff != 10 {
		t.Fatalf("%v %+v", err, d)
	}
	if f.forms["POST /v1/promotion_codes/promo_1"].Get("active") != "false" {
		t.Errorf("deactivate form %v", f.forms)
	}
}

// Was a bug: with Stripe API 2025-09 and later every code was listed with an empty coupon: the code's coupon moved
// to promotion.coupon, an id unless expanded. The three shapes: expanded, an id, and the old `coupon` object.
func TestAdminPromoList(t *testing.T) {
	var expand string
	s, _, _ := adminStripeTest(t, func(w http.ResponseWriter, r *http.Request) {
		expand = r.URL.Query().Get("expand[]")
		_, _ = w.Write([]byte(`{"has_more":false,"data":[
			{"id":"promo_1","code":"X","active":true,"expires_at":1700000000,"promotion":{"type":"coupon","coupon":{"id":"c","amount_off":500,"currency":"usd","duration":"once","duration_in_months":null,"percent_off":null}}},
			{"id":"promo_2","code":"Y","active":true,"promotion":{"type":"coupon","coupon":"c2"}},
			{"id":"promo_3","code":"Z","active":true,"coupon":{"id":"c3","percent_off":20,"duration":"forever"}},
			{"id":"promo_4","code":"GONE","active":true,"promotion":{"type":"coupon","coupon":{"id":"c4","object":"coupon","deleted":true}}}]}`))
	})
	pg, err := s.AdminListPromotionCodes(context.Background(), "", 10)
	if err != nil || len(pg.Items) != 4 || pg.Items[0].ExpiresAt == "" || pg.NextCursor != "" || expand != "data.promotion.coupon" {
		t.Fatalf("%v %+v (expand %q)", err, pg, expand)
	}
	if c := pg.Items[0].Coupon; c.ID != "c" || c.AmountOff != 500 || c.Currency != "usd" || c.Duration != "once" {
		t.Errorf("expanded coupon: %+v", c)
	}
	if c := pg.Items[1].Coupon; c.ID != "c2" {
		t.Errorf("coupon as an id: %+v", c)
	}
	if c := pg.Items[2].Coupon; c.ID != "c3" || c.PercentOff != 20 || c.Deleted {
		t.Errorf("coupon before API 2025-09: %+v", c)
	}
	if c := pg.Items[3].Coupon; c.ID != "c4" || !c.Deleted {
		t.Errorf("a coupon deleted in Stripe: %+v", c)
	}
}

func TestAdminPromoCreateFailureDeletesCoupon(t *testing.T) {
	var deleted string
	s, _, _ := adminStripeTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/coupons":
			_, _ = w.Write([]byte(`{"id":"cp_1"}`))
		case r.Method == "POST" && r.URL.Path == "/v1/promotion_codes":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"code":"resource_already_exists","param":"code"}}`))
		case r.Method == "DELETE":
			deleted = r.URL.Path
			_, _ = w.Write([]byte(`{"id":"cp_1","deleted":true}`))
		}
	})
	_, err := s.AdminCreatePromotionCode(context.Background(), Admin{Email: "a@b.c"}, AdminPromoInput{Code: "SAVE10", PercentOff: 10, Duration: "once"})
	if !isCode(err, "payment_provider_error") {
		t.Fatalf("got %v", err)
	}
	if deleted != "/v1/coupons/cp_1" {
		t.Fatalf("orphan coupon not deleted: %q", deleted)
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("audit rows %d %v", n, err)
	}
}

// Was noise: a refusal the caller turns into a 409 or 404 (a lost add-on race) was logged as an error like one
// nobody handles. With stripeHandles it is a warning; anything else Stripe refuses stays an error.
func TestHandledStripeRefusalIsAWarning(t *testing.T) {
	s, _, _ := adminStripeTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"param":"` + r.URL.Query().Get("param") + `","type":"invalid_request_error"}}`))
	})
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewTextHandler(&logs, nil))
	ctx := stripeHandles(context.Background(), func(_ int, param string) bool { return param == "plan" })
	for param, level := range map[string]string{"plan": "level=WARN", "customer": "level=ERROR"} {
		logs.Reset()
		if err := s.stripe(ctx, "GET", "/v1/x?param="+param, nil, nil); err == nil {
			t.Fatal("setup: the request was not refused")
		}
		if !strings.Contains(logs.String(), level) {
			t.Errorf("a refusal of %q is logged as %q, want %s", param, logs.String(), level)
		}
	}
}
