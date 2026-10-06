package main

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"SIM_SCENARIO": "s.yaml"}))
	if err != nil {
		t.Fatal(err)
	}
	limits, faults, auth := cfg.apiBehavior(&scenario.Scenario{})
	if cfg.addr != ":8080" || cfg.speed != 1 || cfg.driveUpload != time.Minute || cfg.logLevel != slog.LevelInfo ||
		!reflect.DeepEqual(limits, volvoapi.DefaultLimits()) || faults.ErrorRate != 0 || faults.Latency != 0 || faults.Outages != nil ||
		!reflect.DeepEqual(auth, volvoapi.DefaultOAuth()) || cfg.databaseURL != "" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"SIM_SCENARIO": "s.yaml", "SIM_ADDR": ":9000", "SIM_SPEED": "60", "SIM_DRIVE_UPLOAD": "30s",
		"SIM_DAILY_QUOTA": "0", "SIM_RATE_PER_MINUTE": "5", "SIM_LOG_LEVEL": "debug",
		"SIM_TOKEN_TTL": "30m", "SIM_ERROR_RATE": "0.2", "SIM_LATENCY": "100ms",
		"SIM_CLIENT_ID": "id", "SIM_CLIENT_SECRET": "secret", "SIM_REDIRECT_URI": "https://example.test/cb",
		"SIM_ACCESS_TOKEN_TTL": "299s", "SIM_GRANT_TTL": "48h", "SIM_API_KEYS": "key-a, key-b",
		"SIM_DATABASE_URL": "postgres://sim@db/sim",
	}))
	if err != nil {
		t.Fatal(err)
	}
	// The environment takes precedence over the scenario.
	ten, fifty := 10, 50
	start := time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
	limits, faults, auth := cfg.apiBehavior(&scenario.Scenario{Start: start, API: scenario.API{
		DailyQuota: &ten, PerMinute: &fifty, TokenTTL: time.Hour, ErrorRate: 0.5,
		Outages:        []scenario.Outage{{After: time.Hour, Duration: 30 * time.Minute}},
		AccessTokenTTL: time.Hour, RefreshTokenTTL: 3 * time.Hour, GrantTTL: 24 * time.Hour,
	}})
	if cfg.addr != ":9000" || cfg.speed != 60 || cfg.driveUpload != 30*time.Second || cfg.logLevel != slog.LevelDebug ||
		cfg.databaseURL != "postgres://sim@db/sim" {
		t.Errorf("config = %+v", cfg)
	}
	if !reflect.DeepEqual(limits, volvoapi.Limits{DailyQuota: 0, PerMinute: 5, TokenTTL: 30 * time.Minute, Keys: []string{"key-a", "key-b"}}) {
		t.Errorf("limits = %+v", limits)
	}
	wantOutage := volvoapi.Outage{From: start.Add(time.Hour), To: start.Add(90 * time.Minute)}
	if faults.ErrorRate != 0.2 || faults.Latency != 100*time.Millisecond || len(faults.Outages) != 1 || faults.Outages[0] != wantOutage {
		t.Errorf("faults = %+v", faults)
	}
	// Scenario over the defaults (refresh token), environment over the scenario (the rest).
	if auth.ClientID != "id" || auth.ClientSecret != "secret" || auth.RedirectURI != "https://example.test/cb" ||
		auth.AccessTokenTTL != 299*time.Second || auth.RefreshTokenTTL != 3*time.Hour || auth.GrantTTL != 48*time.Hour {
		t.Errorf("oauth = %+v", auth)
	}
}

func TestScenarioOverridesDefaults(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"SIM_SCENARIO": "s.yaml"}))
	if err != nil {
		t.Fatal(err)
	}
	quota := 150
	limits, _, _ := cfg.apiBehavior(&scenario.Scenario{API: scenario.API{DailyQuota: &quota}})
	if limits.DailyQuota != 150 || limits.PerMinute != 100 {
		t.Errorf("limits = %+v", limits)
	}
}

func TestScenarioList(t *testing.T) {
	tests := []struct {
		value   string
		want    []string
		wantErr bool
	}{
		{"scenarios/commute.yaml", []string{"scenarios/commute.yaml"}, false},
		{"a.yaml,b.yaml", []string{"a.yaml", "b.yaml"}, false},
		{" a.yaml , b.yaml ", []string{"a.yaml", "b.yaml"}, false},
		{"a.yaml,", nil, true},
		{",a.yaml", nil, true},
	}
	for _, tt := range tests {
		cfg, err := loadConfig(env(map[string]string{"SIM_SCENARIO": tt.value}))
		if (err != nil) != tt.wantErr {
			t.Errorf("%q: error %v", tt.value, err)
			continue
		}
		if !tt.wantErr && strings.Join(cfg.scenarios, "|") != strings.Join(tt.want, "|") {
			t.Errorf("%q: scenarios %q, want %q", tt.value, cfg.scenarios, tt.want)
		}
	}
}

func TestLoadFleet(t *testing.T) {
	dir := "../../scenarios/"
	tests := []struct {
		name    string
		files   []string
		vins    []string
		start   time.Time
		wantErr string
	}{
		{"one scenario", []string{"commute.yaml"}, []string{"YV1SMLT0000DT0001"}, time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC), ""},
		{
			"two scenarios, the clock at the earliest start",
			[]string{"errand.yaml", "ex30.yaml"},
			[]string{"YV1SMLT0000ER0001", "YV1EL00000EX30001"},
			time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC), "",
		},
		{"the same VIN twice", []string{"commute.yaml", "commute.yaml"}, nil, time.Time{}, "same vin"},
		{"api section not first", []string{"ex30.yaml", "errand.yaml"}, nil, time.Time{}, "only the first scenario may have an api section"},
		{"missing file", []string{"commute.yaml", "nothing.yaml"}, nil, time.Time{}, "reading scenario"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := make([]string, len(tt.files))
			for i, f := range tt.files {
				files[i] = dir + f
			}
			scs, fleet, err := loadFleet(files, vehicle.DefaultUploadPolicy())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var vins []string
			for _, sim := range fleet.Simulations() {
				vins = append(vins, sim.VIN())
			}
			if len(scs) != len(files) || strings.Join(vins, ",") != strings.Join(tt.vins, ",") || !fleet.Start().Equal(tt.start) {
				t.Errorf("%d scenarios, VINs %v, start %v", len(scs), vins, fleet.Start())
			}
		})
	}
}

func TestLoadConfigErrors(t *testing.T) {
	_, err := loadConfig(env(map[string]string{
		"SIM_SPEED": "-1", "SIM_DRIVE_UPLOAD": "fast", "SIM_DAILY_QUOTA": "lots", "SIM_LOG_LEVEL": "loud",
		"SIM_TOKEN_TTL": "-1s", "SIM_ERROR_RATE": "2", "SIM_LATENCY": "slow", "SIM_GRANT_TTL": "0s",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		"SIM_SCENARIO", "SIM_SPEED", "SIM_DRIVE_UPLOAD", "SIM_DAILY_QUOTA", "SIM_LOG_LEVEL",
		"SIM_TOKEN_TTL", "SIM_ERROR_RATE", "SIM_LATENCY", "SIM_GRANT_TTL",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

func TestAPIKeysList(t *testing.T) {
	if _, err := loadConfig(env(map[string]string{"SIM_SCENARIO": "s.yaml", "SIM_API_KEYS": "key-a,,key-b"})); err == nil {
		t.Error("an empty key accepted")
	}
}
