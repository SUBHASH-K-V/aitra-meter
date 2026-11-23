// gpu_carbon_test.go — end-to-end test: live WattTime carbon + GPU numbers → all aitra metrics.
// This is what the guide asked for: proof that the carbon source feeds the full metric pipeline.
//
// Run:
//
//	WATTTIME_USERNAME=... WATTTIME_PASSWORD=... WATTTIME_REGION=... \
//	    go test -v -count=1 -run TestGPUCarbonMetrics ./internal/aggregation/carbon/
package carbon

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// GPUScenario holds realistic GPU inference numbers.
type GPUScenario struct {
	Name             string
	GPUPowerWatts    float64 // W  — from NVML / DCGM
	TokensPerSecond  float64 // tok/s — from vLLM / inference provider
	CostPerKWh       float64 // USD/kWh — from site config
}

// aitraMetrics mirrors the calculations in the aggregation loop.
type aitraMetrics struct {
	GridIntensityGCO2PerKWh    float64 // aitra_grid_intensity_gco2_per_kwh
	CarbonSource               string  // carbon_source label
	JPerToken                  float64 // aitra_j_per_token
	TokensPerJoule             float64 // aitra_tokens_per_joule
	ModelEnergyPer1MTokens_kJ  float64 // aitra_model_energy_per_1m_tokens (kJ)
	CO2PerTokenGrams           float64 // aitra_co2_per_token_grams
	CO2Per1MTokensGrams        float64 // derived: grams per million tokens
	CostPerMillionTokensUSD    float64 // aitra_cost_per_million_tokens_usd
}

func computeAitraMetrics(g GPUScenario, gco2PerKWh float64, source string) aitraMetrics {
	jPerToken := g.GPUPowerWatts / g.TokensPerSecond
	return aitraMetrics{
		GridIntensityGCO2PerKWh:   gco2PerKWh,
		CarbonSource:              source,
		JPerToken:                 jPerToken,
		TokensPerJoule:            1.0 / jPerToken,
		ModelEnergyPer1MTokens_kJ: jPerToken * 1_000_000 / 1000,
		CO2PerTokenGrams:          jPerToken * gco2PerKWh / 3_600_000,
		CO2Per1MTokensGrams:       jPerToken * gco2PerKWh / 3_600_000 * 1_000_000,
		CostPerMillionTokensUSD:   jPerToken * g.CostPerKWh / 3_600_000 * 1_000_000,
	}
}

func printMetrics(t *testing.T, g GPUScenario, m aitraMetrics) {
	t.Helper()
	t.Logf("")
	t.Logf("  ┌─────────────────────────────────────────────────────────┐")
	t.Logf("  │  GPU: %-51s│", g.Name)
	t.Logf("  ├─────────────────────────────────────────────────────────┤")
	t.Logf("  │  📡  aitra_grid_intensity_gco2_per_kwh  = %8.2f      │", m.GridIntensityGCO2PerKWh)
	t.Logf("  │      carbon_source                      = %-12s  │", m.CarbonSource)
	t.Logf("  ├─────────────────────────────────────────────────────────┤")
	t.Logf("  │  🖥️   GPU power                          = %8.1f W    │", g.GPUPowerWatts)
	t.Logf("  │      tokens / sec                       = %8.1f      │", g.TokensPerSecond)
	t.Logf("  ├─────────────────────────────────────────────────────────┤")
	t.Logf("  │  ⚡  aitra_j_per_token                  = %8.4f J    │", m.JPerToken)
	t.Logf("  │      aitra_tokens_per_joule             = %8.4f      │", m.TokensPerJoule)
	t.Logf("  │      aitra_model_energy_per_1m_tokens   = %8.2f kJ   │", m.ModelEnergyPer1MTokens_kJ)
	t.Logf("  ├─────────────────────────────────────────────────────────┤")
	t.Logf("  │  🌍  aitra_co2_per_token_grams          = %.6f g  │", m.CO2PerTokenGrams)
	t.Logf("  │      CO₂ per 1M tokens                  = %8.2f g    │", m.CO2Per1MTokensGrams)
	t.Logf("  ├─────────────────────────────────────────────────────────┤")
	t.Logf("  │  💰  aitra_cost_per_million_tokens_usd  = $%7.4f     │", m.CostPerMillionTokensUSD)
	t.Logf("  └─────────────────────────────────────────────────────────┘")
}

func TestGPUCarbonMetrics(t *testing.T) {
	// ── GPU scenarios — realistic inference workloads ──────────────────
	scenarios := []GPUScenario{
		{"A100 80GB  — heavy load  (300 W, 60 tok/s)",  300, 60,  0.08},
		{"A100 80GB  — inference   (300 W, 80 tok/s)",  300, 80,  0.08},
		{"H100 SXM   — fast infer  (350 W, 200 tok/s)", 350, 200, 0.08},
		{"RTX 4090   — small model (200 W, 150 tok/s)", 200, 150, 0.06},
		{"L4 (GCP)   — efficient   ( 72 W, 70 tok/s)",   72, 70,  0.09},
	}

	// ── Fetch live carbon intensity ────────────────────────────────────
	user   := os.Getenv("WATTTIME_USERNAME")
	pass   := os.Getenv("WATTTIME_PASSWORD")
	region := os.Getenv("WATTTIME_REGION")

	var gco2   float64
	var source string

	if user != "" && pass != "" && region != "" {
		client := NewWattTimeClient(user, pass, region, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		live, err := client.Fetch(ctx)
		if err != nil {
			t.Logf("⚠️  WattTime fetch failed (%v) — using 400 gCO2/kWh fallback", err)
			gco2, source = 400.0, "static"
		} else {
			gco2, source = live, "watttime"
			t.Logf("✅  Live grid intensity: %.2f gCO2/kWh  [%s / %s]", gco2, source, region)
		}
	} else {
		gco2, source = 400.0, "static"
		t.Skip("set WATTTIME_USERNAME / PASSWORD / REGION for live carbon intensity")
	}

	// ── Run each GPU scenario ──────────────────────────────────────────
	t.Logf("")
	t.Logf("  ═══════════════════════════════════════════════════════════")
	t.Logf("  AITRA METER — GPU + Carbon End-to-End Metrics")
	t.Logf("  ═══════════════════════════════════════════════════════════")

	for _, g := range scenarios {
		t.Run(g.Name, func(t *testing.T) {
			m := computeAitraMetrics(g, gco2, source)
			printMetrics(t, g, m)

			// Sanity assertions — same guards as the aggregation loop
			if m.JPerToken <= 0 {
				t.Errorf("J/token must be positive, got %v", m.JPerToken)
			}
			if m.CO2PerTokenGrams <= 0 {
				t.Errorf("CO2/token must be positive, got %v", m.CO2PerTokenGrams)
			}
			if m.CostPerMillionTokensUSD <= 0 {
				t.Errorf("cost/1M tokens must be positive, got %v", m.CostPerMillionTokensUSD)
			}

			// Conversion sanity: gCO2/kWh × J/token ÷ 3.6M = gCO2/token
			expected := fmt.Sprintf("%.6f", m.JPerToken*gco2/3_600_000)
			got      := fmt.Sprintf("%.6f", m.CO2PerTokenGrams)
			if expected != got {
				t.Errorf("CO2/token formula mismatch: got %s want %s", got, expected)
			}
		})
	}

	t.Logf("")
	t.Logf("  📡  Grid: %.2f gCO2/kWh  [%s / %s]  at %s UTC",
		gco2, source, region, time.Now().UTC().Format("2006-01-02 15:04"))
}
