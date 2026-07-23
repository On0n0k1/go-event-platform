package events

import "github.com/nats-io/nats.go"

// natsHeaderCarrier adapts nats.Header to OTel's propagation.TextMapCarrier
// so trace context can be injected into (or extracted from) NATS message
// headers -- NATS has no official OTel instrumentation, unlike HTTP/gRPC, so
// this propagation is done by hand.
type natsHeaderCarrier nats.Header

func (c natsHeaderCarrier) Get(key string) string {
	values := nats.Header(c)[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (c natsHeaderCarrier) Set(key, value string) {
	nats.Header(c)[key] = []string{value}
}

func (c natsHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
