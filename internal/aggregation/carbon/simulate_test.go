// simulate_test.go — interactive GPU + carbon metrics simulator.
// Feed in GPU power and token throughput; get live co2/token, J/token, cost/token.
//
// Run:
//
//	WATTTIME_USERNAME=... WATTTIME_PASSWORD=... WATTTIME_REGION=... \
//	    go test -v -count=1 -run TestSimulateGPUMetrics ./internal/aggregation/carbon/
package carbon

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestSimulateGPUMetrics(t *testing.T) {
	// ── GPU inputs — change these to match your GPU ──────────────────────
	gpuPowerWatts := 300.0  // typical A100 inference load (W)
	tokensPerSecond := 80.0 // tokens/sec throughput
	electricityCostPerKWh := 0.08 // USD/kWh (typical US data-center rate)
	// ─────────────────────────────────────────────────────────────────────

	// Fetch live carbon intensity from WattTime (or use static fallback)
	user := os.Getenv("WATTTIME_USERNAME")
	pass := os.Getenv("WATTTIME_PASSWORD")
	region := os.Getenv("WATTTIME_REGION")

	var gco2PerKWh float64
	var source string

	if user != "" && pass != "" && region != "" {
		client := NewWattTimeClient(user, pass, region, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		live, err := client.Fetch(ctx)
		if err != nil {
			t.Logf("⚠️  WattTime fetch failed (%v) — using static fallback 400 gCO2/kWh", err)
			gco2PerKWh = 400.0
			source = "static (fallback)"
		} else {
			gco2PerKWh = live
			source = fmt.Sprintf("watttime live (%s)", region)
		}
	} else {
		gco2PerKWh = 400.0
		source = "static (no credentials set)"
	}

	// ── Derived metric calculations (mirrors aggregation loop logic) ──────
	//
	//  J/token       = power(W) / throughput(tok/s)
	//  gCO2/token    = J/token × gCO2/kWh ÷ 3,600,000
	//  cost/1M token = J/token × costPerKWh ÷ 3,600,000 × 1,000,000
	//  tokensPerJoule= 1 / (J/token)

	jPerToken := gpuPowerWatts / tokensPerSecond
	co2PerTokenGrams := jPerToken * gco2PerKWh / 3_600_000.0
	tokensPerJoule := 1.0 / jPerToken
	costPerMillionTokens := jPerToken * electricityCostPerKWh / 3_600_000.0 * 1_000_000.0
	energyPer1MTokens := jPerToken * 1_000_000.0 / 1000.0 // kJ

	t.Logf("")
	t.Logf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	t.Logf("  AITRA METER — GPU + Carbon Metrics Simulator")
	t.Logf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	t.Logf("")
	t.Logf("  📡  Grid carbon intensity")
	t.Logf("      aitra_grid_intensity_gco2_per_kwh  = %.2f gCO2/kWh", gco2PerKWh)
	t.Logf("      carbon_source                      = %s", source)
	t.Logf("")
	t.Logf("  🖥️  GPU inputs")
	t.Logf("      aitra_gpu_power_watts              = %.1f W", gpuPowerWatts)
	t.Logf("      tokens/sec throughput              = %.1f tok/s", tokensPerSecond)
	t.Logf("      electricity cost                   = $%.4f/kWh", electricityCostPerKWh)
	t.Logf("")
	t.Logf("  ⚡  Efficiency metrics")
	t.Logf("      aitra_j_per_token                  = %.4f J/token", jPerToken)
	t.Logf("      aitra_tokens_per_joule             = %.4f tok/J", tokensPerJoule)
	t.Logf("      aitra_model_energy_per_1m_tokens   = %.2f kJ/1M tokens", energyPer1MTokens)
	t.Logf("")
	t.Logf("  🌍  Carbon metrics")
	t.Logf("      aitra_co2_per_token_grams          = %.6f gCO2/token", co2PerTokenGrams)
	t.Logf("      per 1M tokens CO2                  = %.2f g  (%.4f kg)", co2PerTokenGrams*1e6, co2PerTokenGrams*1e3)
	t.Logf("")
	t.Logf("  💰  Cost metrics")
	t.Logf("      aitra_cost_per_million_tokens_usd  = $%.4f / 1M tokens", costPerMillionTokens)
	t.Logf("")
	t.Logf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}
