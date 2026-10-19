package cli

import (
	"flag"
	"fmt"
	"strings"
)

type billingInfo struct {
	Enabled            bool    `json:"enabled"`
	Plan               string  `json:"plan"`
	SubscriptionStatus *string `json:"subscription_status"`
	PlanExpiresAt      *string `json:"plan_expires_at"`
	Suspended          bool    `json:"suspended"`
	HasCustomer        bool    `json:"has_customer"`
}

// billingCommand: Stripe billing of a cloud workspace (admin key).
func billingCommand() *Command {
	url := func(path, what string) func(fs *flag.FlagSet) runFunc {
		return func(fs *flag.FlagSet) runFunc {
			return func(c *Ctx, _ []string) error {
				cl, err := c.Client()
				if err != nil {
					return err
				}
				var out struct{ URL string }
				raw, err := cl.Post(c, path, nil, &out)
				if err != nil || c.JSON {
					return jsonOr(c, raw, err)
				}
				fmt.Fprintln(c.Out, out.URL)
				fmt.Fprintf(c.Stderr, "Open this link in a browser to %s.\n", what)
				return nil
			}
		}
	}
	return &Command{
		Name:  "billing",
		Short: "Show the subscription, pay for or switch a plan, or open the billing portal (cloud)",
		Subs: []*Command{
			leaf("status", "", "Show billing status: plan, subscription status, paid-through date", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var b billingInfo
					raw, err := cl.Get(c, "/v1/billing", nil, &b)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					deref := func(p *string) string {
						if p == nil {
							return "-"
						}
						return *p
					}
					c.kv("Billing enabled", fmt.Sprint(b.Enabled), "Plan", b.Plan, "Subscription", deref(b.SubscriptionStatus),
						"Plan ends", fmtTime(deref(b.PlanExpiresAt)), "Suspended", fmt.Sprint(b.Suspended))
					return nil
				}
			}, "staqly billing status", "staqly billing status --json"),
			leaf("checkout", "", "Pay for a plan (Starter, Plus, Pro, Business), monthly or yearly: prints a Stripe Checkout link; a paying workspace is switched instead", 0, 0, func(fs *flag.FlagSet) runFunc {
				plan := fs.String("plan", "pro", "starter, plus, pro or business")
				yearly := fs.Bool("yearly", false, "billed yearly (20% off twelve months, or more)")
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					interval := "month"
					if *yearly {
						interval = "year"
					}
					var out struct{ URL string }
					raw, err := cl.Post(c, "/v1/billing/checkout", map[string]any{"plan": *plan, "interval": interval}, &out)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintln(c.Out, out.URL)
					if strings.Contains(out.URL, "checkout=switched") {
						fmt.Fprintf(c.Stderr, "Switched the subscription to %s (%sly); the plan changes once Stripe confirms.\n", *plan, interval)
					} else {
						fmt.Fprintln(c.Stderr, "Open this link in a browser to pay.")
					}
					return nil
				}
			}, "staqly billing checkout --plan plus --yearly", "staqly billing checkout --plan business"),
			leaf("portal", "", "Print a Stripe customer portal link (payment method, invoices, cancel)", 0, 0,
				url("/v1/billing/portal", "manage billing"), "staqly billing portal"),
		},
	}
}
