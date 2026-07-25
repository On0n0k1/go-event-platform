//go:build integration

// Package integration boots the full stack with `docker compose` and
// exercises it over real HTTP, against real Postgres and real inter-service
// calls. Run with: go test -tags=integration ./integration/...
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

const (
	gatewayURL   = "http://localhost:8080"
	analyticsURL = "http://localhost:8084"
	projectName  = "go-event-platform-it"
)

func TestMain(m *testing.M) {
	if out, err := compose("up", "--build", "-d").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "docker compose up failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	code := m.Run()

	if out, err := compose("down", "-v").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "docker compose down failed: %v\n%s\n", err, out)
	}

	os.Exit(code)
}

func compose(args ...string) *exec.Cmd {
	fullArgs := append([]string{"compose", "-f", "../docker-compose.yml", "-p", projectName}, args...)
	return exec.Command("docker", fullArgs...)
}

func waitForHealthy(t *testing.T, url string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("service at %s did not become healthy within %s", url, timeout)
}

type item struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type order struct {
	ID       string `json:"id"`
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
	Status   string `json:"status"`
}

type stats struct {
	OrdersCount           int `json:"orders_count"`
	TotalQuantityReserved int `json:"total_quantity_reserved"`
}

func getItem(t *testing.T, sku string) item {
	t.Helper()

	resp, err := http.Get(gatewayURL + "/items/" + sku)
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get item status = %d", resp.StatusCode)
	}

	var it item
	if err := json.NewDecoder(resp.Body).Decode(&it); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	return it
}

func postOrder(t *testing.T, sku string, quantity int) *http.Response {
	t.Helper()

	body, err := json.Marshal(map[string]any{"sku": sku, "quantity": quantity})
	if err != nil {
		t.Fatalf("encode order request: %v", err)
	}

	resp, err := http.Post(gatewayURL+"/orders", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create order request: %v", err)
	}
	return resp
}

func postRestock(t *testing.T, sku string, quantity int) *http.Response {
	t.Helper()

	body, err := json.Marshal(map[string]any{"quantity": quantity})
	if err != nil {
		t.Fatalf("encode restock request: %v", err)
	}

	resp, err := http.Post(gatewayURL+"/items/"+sku+"/restock", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("restock request: %v", err)
	}
	return resp
}

func createOrder(t *testing.T, sku string, quantity int) order {
	t.Helper()

	resp := postOrder(t, sku, quantity)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create order status = %d", resp.StatusCode)
	}

	var o order
	if err := json.NewDecoder(resp.Body).Decode(&o); err != nil {
		t.Fatalf("decode order: %v", err)
	}
	return o
}

func getOrder(t *testing.T, id string) order {
	t.Helper()

	resp, err := http.Get(gatewayURL + "/orders/" + id)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get order status = %d", resp.StatusCode)
	}

	var o order
	if err := json.NewDecoder(resp.Body).Decode(&o); err != nil {
		t.Fatalf("decode order: %v", err)
	}
	return o
}

func getStats(t *testing.T) stats {
	t.Helper()

	resp, err := http.Get(analyticsURL + "/stats")
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get stats status = %d", resp.StatusCode)
	}

	var s stats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	return s
}

func TestOrderFlow(t *testing.T) {
	waitForHealthy(t, gatewayURL+"/healthz", 60*time.Second)

	before := getItem(t, "SKU-001")

	o := createOrder(t, "SKU-001", 3)
	if o.Status != "confirmed" {
		t.Fatalf("order status = %q, want confirmed", o.Status)
	}
	if o.Quantity != 3 {
		t.Fatalf("order quantity = %d, want 3", o.Quantity)
	}

	fetched := getOrder(t, o.ID)
	if fetched.ID != o.ID || fetched.SKU != o.SKU {
		t.Fatalf("fetched order = %+v, want match for %+v", fetched, o)
	}

	after := getItem(t, "SKU-001")
	if after.Quantity != before.Quantity-3 {
		t.Fatalf("stock after order = %d, want %d", after.Quantity, before.Quantity-3)
	}
}

func TestOrderFlowInsufficientStockDoesNotReserve(t *testing.T) {
	waitForHealthy(t, gatewayURL+"/healthz", 60*time.Second)

	before := getItem(t, "SKU-002")

	resp := postOrder(t, "SKU-002", before.Quantity+1)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("order status = %d, want 409", resp.StatusCode)
	}

	after := getItem(t, "SKU-002")
	if after.Quantity != before.Quantity {
		t.Fatalf("stock changed after rejected order: before=%d after=%d", before.Quantity, after.Quantity)
	}
}

// TestRestockIncreasesStock confirms restock's effect is visible through the
// same cache-aside read path as everything else -- GetItem is read again
// right after, so a stale cache entry would make this fail even though the
// write itself succeeded.
func TestRestockIncreasesStock(t *testing.T) {
	waitForHealthy(t, gatewayURL+"/healthz", 60*time.Second)

	before := getItem(t, "SKU-001")

	resp := postRestock(t, "SKU-001", 7)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restock status = %d, want 200", resp.StatusCode)
	}

	var restocked item
	if err := json.NewDecoder(resp.Body).Decode(&restocked); err != nil {
		t.Fatalf("decode restock response: %v", err)
	}
	if restocked.Quantity != before.Quantity+7 {
		t.Fatalf("restock response quantity = %d, want %d", restocked.Quantity, before.Quantity+7)
	}

	after := getItem(t, "SKU-001")
	if after.Quantity != before.Quantity+7 {
		t.Fatalf("stock after restock = %d, want %d", after.Quantity, before.Quantity+7)
	}
}

func TestRestockRejectsNonPositiveQuantity(t *testing.T) {
	waitForHealthy(t, gatewayURL+"/healthz", 60*time.Second)

	resp := postRestock(t, "SKU-001", 0)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("restock status = %d, want 400", resp.StatusCode)
	}
}

// settleOutbox waits long enough for the outbox relay's poll interval (500ms
// in docker-compose.yml) to flush any event still in flight from a prior
// test, so a "before" stats snapshot isn't taken mid-delivery.
func settleOutbox() {
	time.Sleep(1500 * time.Millisecond)
}

// TestOrderCreatedEventReachesAnalytics confirms the asynchronous path: an
// order created through the gateway results in an OrderCreated event
// published to NATS JetStream, consumed independently by analytics-service,
// with no direct HTTP call between order-service and analytics-service.
func TestOrderCreatedEventReachesAnalytics(t *testing.T) {
	waitForHealthy(t, gatewayURL+"/healthz", 60*time.Second)
	waitForHealthy(t, analyticsURL+"/healthz", 60*time.Second)
	settleOutbox()

	before := getStats(t)

	o := createOrder(t, "SKU-001", 2)

	deadline := time.Now().Add(10 * time.Second)
	for {
		after := getStats(t)
		if after.OrdersCount == before.OrdersCount+1 && after.TotalQuantityReserved == before.TotalQuantityReserved+o.Quantity {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("analytics stats did not reflect order %s within timeout: before=%+v after=%+v", o.ID, before, after)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestOrderCreatedEventSurvivesNATSOutage exercises the transactional outbox
// end to end: order-service's DB write (order + outbox event) is independent
// of NATS, so creating an order still succeeds while NATS is down, and the
// background relay delivers the event once NATS comes back -- without
// delivering it more than once, which requires JetStream's Nats-Msg-Id
// dedup (a naive retry-until-success relay can otherwise double-publish on
// an ambiguous failure, e.g. a timeout where the server actually received
// the message before the ack was lost).
func TestOrderCreatedEventSurvivesNATSOutage(t *testing.T) {
	waitForHealthy(t, gatewayURL+"/healthz", 60*time.Second)
	waitForHealthy(t, analyticsURL+"/healthz", 60*time.Second)
	settleOutbox()

	if out, err := compose("stop", "nats").CombinedOutput(); err != nil {
		t.Fatalf("docker compose stop nats: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_, _ = compose("start", "nats").CombinedOutput()
	})

	before := getStats(t)

	// The DB write (order + outbox event, one transaction) doesn't touch
	// NATS at all, so this must still succeed with NATS fully down.
	o := createOrder(t, "SKU-002", 2)
	if o.Status != "confirmed" {
		t.Fatalf("order status = %q, want confirmed", o.Status)
	}

	// Give the relay a few failed poll attempts against the down broker
	// before restoring it, so this actually exercises retry-then-recover
	// rather than just a single lucky attempt.
	time.Sleep(3 * time.Second)

	if out, err := compose("start", "nats").CombinedOutput(); err != nil {
		t.Fatalf("docker compose start nats: %v\n%s", err, out)
	}
	waitForHealthy(t, gatewayURL+"/healthz", 30*time.Second)

	deadline := time.Now().Add(20 * time.Second)
	for {
		after := getStats(t)
		if after.OrdersCount == before.OrdersCount+1 && after.TotalQuantityReserved == before.TotalQuantityReserved+o.Quantity {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("analytics stats did not reflect order %s within timeout: before=%+v after=%+v", o.ID, before, after)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Give any (incorrect) duplicate deliveries a moment to land, then
	// confirm the count didn't creep past exactly one delivery.
	time.Sleep(2 * time.Second)
	final := getStats(t)
	if final.OrdersCount != before.OrdersCount+1 || final.TotalQuantityReserved != before.TotalQuantityReserved+o.Quantity {
		t.Fatalf("stats kept changing after the expected delivery -- possible duplicate delivery: before=%+v final=%+v", before, final)
	}
}
