// Package provider defines the balance contract every provider client satisfies,
// plus the shared dashboard parser for the Digitral (imi) protocol.
package provider

import "context"

// PackageQuota is one package's remaining data. Remaining is already formatted
// ("N MB") because each provider reports a different unit.
type PackageQuota struct {
	Name      string `json:"name"`
	Remaining string `json:"remaining"`
	Unit      string `json:"unit"`
}

// Balance is the provider-blind result shape. Every provider must fill it the
// same way; the dashboard never sees a provider-specific field.
type Balance struct {
	Balance       any            `json:"balance"`
	Quota         any            `json:"quota"`
	Unit          string         `json:"unit"`
	Used          any            `json:"used"`
	Unlimited     bool           `json:"unlimited"`
	ActiveUntil   any            `json:"active_until"`
	GraceUntil    any            `json:"grace_until"`
	PackageQuotas []PackageQuota `json:"package_quotas"`
	Bonstri       any            `json:"bonstri"`

	// SubscriptionStatus is myXL-only; other providers leave it unset.
	SubscriptionStatus any `json:"subscription_status"`
}

// Client is implemented by imi (bima+/myIM3), axisnet and myXL.
type Client interface {
	Provider() string
	Balance(ctx context.Context, msisdn string) (Balance, error)
}
