package httpapi

import (
	"context"
	"fmt"

	"digitral/backend/internal/provider"
)

// The three provider clients have different OTP signatures (imi carries a
// transid and action; axisnet/myXL are stateless). These adapters keep the API
// layer provider-blind.
type sender3 interface {
	SendOTP(context.Context, string, string) (string, error)
}
type sender2 interface {
	SendOTP(context.Context, string) (string, error)
}
type validator4 interface {
	ValidateOTP(context.Context, string, string, string) (string, error)
}
type validator3 interface {
	ValidateOTP(context.Context, string, string) (string, error)
}
type guestTokener interface {
	GuestToken(context.Context) (string, error)
}

func sendOTP(ctx context.Context, c provider.Client, msisdn string) (string, error) {
	if g, ok := c.(guestTokener); ok {
		if _, err := g.GuestToken(ctx); err != nil {
			return "", err
		}
	}
	if s, ok := c.(sender3); ok {
		return s.SendOTP(ctx, msisdn, "login")
	}
	if s, ok := c.(sender2); ok {
		return s.SendOTP(ctx, msisdn)
	}
	return "", fmt.Errorf("client %s cannot send OTP", c.Provider())
}

func validateOTP(ctx context.Context, c provider.Client, msisdn, transid, otp string) (string, error) {
	if v, ok := c.(validator4); ok {
		return v.ValidateOTP(ctx, msisdn, transid, otp)
	}
	if v, ok := c.(validator3); ok {
		return v.ValidateOTP(ctx, msisdn, otp)
	}
	return "", fmt.Errorf("client %s cannot validate OTP", c.Provider())
}
