package router

import (
	"errors"
	"fmt"
)

var (
	ErrUnknownProtocol = errors.New("unknown payment protocol")
	ErrMalformedL402   = errors.New("malformed L402 challenge")
	ErrMissingInvoice  = errors.New("missing invoice in L402 response")
	ErrBudgetExceeded  = errors.New("payment would exceed budget")
	ErrPaymentFailed   = errors.New("payment settlement failed")
	ErrNoProvider      = errors.New("no payment provider configured for protocol")
)

// PaymentError wraps a payment failure with protocol and amount context.
type PaymentError struct {
	Protocol Protocol
	Amount   string
	Err      error
}

func (e *PaymentError) Error() string {
	return fmt.Sprintf("%s payment of %s failed: %v", e.Protocol, e.Amount, e.Err)
}

func (e *PaymentError) Unwrap() error {
	return e.Err
}
