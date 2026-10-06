package volvo

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const fixturesDir = "../../testdata/volvo-demo-car"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixturesDir, name)) //nolint:gosec // repository file
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFingerprintIgnoresTimes(t *testing.T) {
	a := []byte(`{"data":{"odometer":{"timestamp":"2026-09-25T15:37:22Z","unit":"km","value":42}}}`)
	b := []byte(`{"data":{"odometer":{"value":42,"unit":"km","timestamp":"2026-09-25T16:00:00Z"}}}`)
	c := []byte(`{"data":{"odometer":{"timestamp":"2026-09-25T15:37:22Z","unit":"km","value":43}}}`)
	loc1 := []byte(`{"data":{"geometry":{"coordinates":[1,2,0]},"properties":{"timestamp":"x","heading":"1"}},"operationId":"a"}`)
	loc2 := []byte(`{"data":{"geometry":{"coordinates":[1,2,0]},"properties":{"timestamp":"y","heading":"1"}},"operationId":"b"}`)

	fp := func(raw []byte) []byte {
		t.Helper()
		f, err := Fingerprint(raw)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	if !bytes.Equal(fp(a), fp(b)) {
		t.Error("same value, different timestamp: different fingerprints")
	}
	if bytes.Equal(fp(a), fp(c)) {
		t.Error("different value: same fingerprint")
	}
	if !bytes.Equal(fp(loc1), fp(loc2)) {
		t.Error("location: operationId and timestamp should be ignored")
	}
	if _, err := Fingerprint([]byte("{")); err == nil {
		t.Error("invalid JSON accepted")
	}
}

func TestParseFixtures(t *testing.T) {
	engine, err := ParseEngineStatus(fixture(t, "cv-engine-status.json"))
	if err != nil || engine != "STOPPED" {
		t.Errorf("engine-status = %q, %v", engine, err)
	}
	charging, err := ParseChargingStatus(fixture(t, "energy-v2-state.json"))
	if err != nil || charging != "IDLE" {
		t.Errorf("chargingStatus = %q, %v", charging, err)
	}
}

func TestParseChargingStatusAbsent(t *testing.T) {
	raw := []byte(`{"chargingStatus":{"status":"ERROR","code":"NOT_SUPPORTED","message":"x"}}`)
	if got, err := ParseChargingStatus(raw); err != nil || got != "" {
		t.Errorf("on error: %q, %v", got, err)
	}
	if _, err := ParseChargingStatus([]byte("[")); err == nil {
		t.Error("invalid JSON accepted")
	}
	if _, err := ParseEngineStatus([]byte("[")); err == nil {
		t.Error("invalid JSON accepted")
	}
}
