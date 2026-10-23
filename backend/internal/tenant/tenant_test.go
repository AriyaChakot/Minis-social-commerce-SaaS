package tenant_test

import (
	"errors"
	"testing"

	"mscs/internal/tenant"
)

func ptr(v int64) *int64 { return &v }

func TestShippingFeeFor(t *testing.T) {
	cases := []struct {
		name     string
		rates    tenant.ShippingRates
		subtotal int64
		want     int64
	}{
		{"free shipping shop", tenant.ShippingRates{}, 10000, 0},
		{"flat fee", tenant.ShippingRates{FeeSatang: 4000}, 10000, 4000},
		{"below the free threshold", tenant.ShippingRates{FeeSatang: 4000, FreeFromSatang: ptr(50000)}, 49999, 4000},
		{"exactly the free threshold is free", tenant.ShippingRates{FeeSatang: 4000, FreeFromSatang: ptr(50000)}, 50000, 0},
		{"above the free threshold", tenant.ShippingRates{FeeSatang: 4000, FreeFromSatang: ptr(50000)}, 90000, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.rates.FeeFor(c.subtotal); got != c.want {
				t.Fatalf("FeeFor(%d) = %d, want %d", c.subtotal, got, c.want)
			}
		})
	}
}

func TestShippingValidate(t *testing.T) {
	cases := []struct {
		name  string
		rates tenant.ShippingRates
		field string // "" = valid
	}{
		{"free", tenant.ShippingRates{}, ""},
		{"fee and threshold", tenant.ShippingRates{FeeSatang: 5000, FreeFromSatang: ptr(100000)}, ""},
		{"highest fee", tenant.ShippingRates{FeeSatang: tenant.MaxShippingFeeSatang}, ""},
		{"negative fee", tenant.ShippingRates{FeeSatang: -1}, "fee_satang"},
		{"fee too high", tenant.ShippingRates{FeeSatang: tenant.MaxShippingFeeSatang + 1}, "fee_satang"},
		{"zero threshold", tenant.ShippingRates{FeeSatang: 5000, FreeFromSatang: ptr(0)}, "free_from_satang"},
		{"threshold too high", tenant.ShippingRates{FreeFromSatang: ptr(tenant.MaxFreeFromSatang + 1)}, "free_from_satang"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.rates.Validate()
			var verr *tenant.ValidationError
			switch {
			case c.field == "" && err != nil:
				t.Fatalf("want valid, got %v", err)
			case c.field != "" && (!errors.As(err, &verr) || verr.Field != c.field):
				t.Fatalf("want ValidationError on %s, got %v", c.field, err)
			}
		})
	}
}
