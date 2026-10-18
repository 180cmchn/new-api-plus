package common

import (
	"strings"
	"sync/atomic"

	"github.com/shopspring/decimal"
)

var ReferralEnabled atomic.Bool
var ReferralRateBasisPoints atomic.Int64
var CommissionTransferFeeBasisPoints atomic.Int64

func init() {
	CommissionTransferFeeBasisPoints.Store(1000)
}

// ParseReferralPercent validates the human percentage before storing integer
// basis points. Quota calculations never depend on floating point percentages.
func ParseReferralPercent(value string) (int64, error) {
	if len(value) > 16 || strings.ContainsAny(value, "eE") {
		return 0, NewMessage("Referral percentage must be between 0 and 100 with at most two decimal places")
	}
	rate, err := decimal.NewFromString(value)
	if err != nil || rate.IsNegative() || rate.GreaterThan(decimal.NewFromInt(100)) || !rate.Equal(rate.Truncate(2)) {
		return 0, NewMessage("Referral percentage must be between 0 and 100 with at most two decimal places")
	}
	return rate.Mul(decimal.NewFromInt(100)).IntPart(), nil
}
