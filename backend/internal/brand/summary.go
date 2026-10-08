package brand

import "context"

type PaymentTotals struct {
	Currency  string  `json:"currency"`
	Collected float64 `json:"collected"`
	Refunded  float64 `json:"refunded"`
}
type Summary struct {
	Users           int64           `json:"users"`
	Keys            int64           `json:"keys"`
	Orders          int64           `json:"orders"`
	BalanceUSD      float64         `json:"balance_usd"`
	UsageChargesUSD float64         `json:"usage_charges_usd"`
	AccountCostUSD  float64         `json:"account_cost_usd"`
	UsageMarginUSD  float64         `json:"usage_margin_usd"`
	Payments        []PaymentTotals `json:"payments_by_currency"`
}

func (s *Store) Summary(ctx context.Context) (Summary, error) {
	out := Summary{Payments: []PaymentTotals{}}
	err := s.DB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM users WHERE deleted_at IS NULL),
		(SELECT COUNT(*) FROM api_keys WHERE deleted_at IS NULL),
		(SELECT COUNT(*) FROM payment_orders),
		(SELECT COALESCE(SUM(balance),0) FROM users WHERE deleted_at IS NULL),
		(SELECT COALESCE(SUM(actual_cost),0) FROM usage_logs),
		(SELECT COALESCE(SUM(COALESCE(account_stats_cost,total_cost)*COALESCE(account_rate_multiplier,1)),0) FROM usage_logs)`).
		Scan(&out.Users, &out.Keys, &out.Orders, &out.BalanceUSD, &out.UsageChargesUSD, &out.AccountCostUSD)
	if err != nil {
		return out, err
	}
	out.UsageMarginUSD = out.UsageChargesUSD - out.AccountCostUSD
	// PayAmount is the merchant currency; Amount/refund_amount are wallet USD.
	// The existing refund calculation uses the original order's payment ratio.
	// Usage margin follows the existing dashboard's configured account cost.
	// Merchant cash flow stays separate because it can use another currency.
	rows, err := s.DB.QueryContext(ctx, `SELECT
		COALESCE(NULLIF(UPPER(provider_snapshot->>'currency'),''),'CNY') AS currency,
		COALESCE(SUM(pay_amount),0),
		COALESCE(SUM(CASE WHEN status IN ('PARTIALLY_REFUNDED','REFUNDED') AND amount>0
			THEN LEAST(refund_amount/amount,1)*pay_amount ELSE 0 END),0)
		FROM payment_orders
		WHERE status IN ('COMPLETED','REFUND_REQUESTED','REFUNDING','REFUND_PENDING','REFUND_FAILED','PARTIALLY_REFUNDED','REFUNDED')
		GROUP BY 1 ORDER BY 1`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p PaymentTotals
		if err := rows.Scan(&p.Currency, &p.Collected, &p.Refunded); err != nil {
			return out, err
		}
		out.Payments = append(out.Payments, p)
	}
	return out, rows.Err()
}
