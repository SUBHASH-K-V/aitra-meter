#!/usr/bin/env python3
"""
aitra_meter_simulate.py
=======================
Reads GPU scenarios from a CSV file (or Excel saved as CSV),
fetches live WattTime grid carbon intensity, and prints all
aitra-meter derived metrics for every row.

Usage:
    WATTTIME_USERNAME=... WATTTIME_PASSWORD=... WATTTIME_REGION=... \
        python3 aitra_meter_simulate.py gpu_scenarios.csv

Or with a static fallback (no credentials needed):
    python3 aitra_meter_simulate.py gpu_scenarios.csv --fallback-gco2 400
"""

import csv
import sys
import os
import argparse
import urllib.request
import urllib.error
import base64
import json

# ── WattTime helpers ────────────────────────────────────────────────────────

def watttime_login(username, password):
    creds = base64.b64encode(f"{username}:{password}".encode()).decode()
    req = urllib.request.Request(
        "https://api.watttime.org/login",
        headers={"Authorization": f"Basic {creds}"}
    )
    with urllib.request.urlopen(req, timeout=10) as r:
        return json.load(r)["token"]

def watttime_fetch(token, region):
    url = f"https://api.watttime.org/v3/forecast?region={region}&signal_type=co2_moer&horizon_hours=0"
    req = urllib.request.Request(url, headers={"Authorization": f"Bearer {token}"})
    with urllib.request.urlopen(req, timeout=10) as r:
        data = json.load(r)
    # lbs CO2/MWh → gCO2/kWh
    moer_lbs = data["data"][0]["value"]
    return moer_lbs * 0.453592

def get_live_carbon(username, password, region):
    try:
        token = watttime_login(username, password)
        gco2 = watttime_fetch(token, region)
        return gco2, f"watttime live ({region})"
    except Exception as e:
        return None, str(e)

# ── Metric calculations (mirrors aggregation loop logic) ────────────────────

def calc_metrics(gpu_power_w, tokens_per_sec, cost_per_kwh, gco2_per_kwh):
    j_per_token          = gpu_power_w / tokens_per_sec
    tokens_per_joule     = 1.0 / j_per_token
    energy_1m_kj         = j_per_token * 1_000_000 / 1000
    co2_per_token        = j_per_token * gco2_per_kwh / 3_600_000
    co2_per_1m_tokens_g  = co2_per_token * 1_000_000
    cost_per_1m          = j_per_token * cost_per_kwh / 3_600_000 * 1_000_000
    return {
        "aitra_j_per_token":                 j_per_token,
        "aitra_tokens_per_joule":            tokens_per_joule,
        "aitra_model_energy_per_1m_tokens":  energy_1m_kj,
        "aitra_co2_per_token_grams":         co2_per_token,
        "co2_per_1m_tokens_grams":           co2_per_1m_tokens_g,
        "aitra_cost_per_million_tokens_usd": cost_per_1m,
    }

# ── Main ────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("csv_file", help="CSV file with GPU scenarios")
    parser.add_argument("--fallback-gco2", type=float, default=400.0,
                        help="Static fallback gCO2/kWh if WattTime is unavailable")
    args = parser.parse_args()

    # Fetch live carbon intensity
    username = os.getenv("WATTTIME_USERNAME", "")
    password = os.getenv("WATTTIME_PASSWORD", "")
    region   = os.getenv("WATTTIME_REGION", "")

    if username and password and region:
        print(f"🔄  Fetching live carbon intensity from WattTime ({region})...")
        gco2, source = get_live_carbon(username, password, region)
        if gco2 is None:
            print(f"⚠️   WattTime failed: {source}")
            print(f"⚠️   Using static fallback: {args.fallback_gco2} gCO2/kWh\n")
            gco2   = args.fallback_gco2
            source = "static (fallback)"
        else:
            print(f"✅  Live: {gco2:.2f} gCO2/kWh  [{source}]\n")
    else:
        gco2   = args.fallback_gco2
        source = "static (no credentials)"
        print(f"ℹ️   No WattTime credentials — using {gco2} gCO2/kWh (static)\n")

    # Read CSV
    with open(args.csv_file, newline="") as f:
        reader = csv.DictReader(f)
        rows = list(reader)

    if not rows:
        print("❌  CSV is empty.")
        sys.exit(1)

    # Print header
    SEP = "─" * 90
    print(f"  {'GPU':<30} {'J/tok':>8} {'tok/J':>8} {'gCO2/tok':>12} {'CO2/1M(g)':>12} {'$/1M tok':>10}")
    print(SEP)

    for row in rows:
        try:
            name     = row["gpu_name"]
            power    = float(row["gpu_power_watts"])
            tps      = float(row["tokens_per_second"])
            cost_kwh = float(row["electricity_cost_per_kwh"])
        except (KeyError, ValueError) as e:
            print(f"  ⚠️  Skipping row (bad data): {e}")
            continue

        m = calc_metrics(power, tps, cost_kwh, gco2)

        print(
            f"  {name:<30} "
            f"{m['aitra_j_per_token']:>8.4f} "
            f"{m['aitra_tokens_per_joule']:>8.4f} "
            f"{m['aitra_co2_per_token_grams']:>12.6f} "
            f"{m['co2_per_1m_tokens_grams']:>12.2f} "
            f"{m['aitra_cost_per_million_tokens_usd']:>10.4f}"
        )

    print(SEP)
    print(f"\n  📡  Grid intensity: {gco2:.2f} gCO2/kWh  [{source}]")
    print(f"  🕐  Timestamp: {__import__('datetime').datetime.utcnow().strftime('%Y-%m-%d %H:%M UTC')}\n")

if __name__ == "__main__":
    main()
