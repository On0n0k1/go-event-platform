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
	gatewayURL  = "http://localhost:8080"
	projectName = "go-event-platform-it"
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
