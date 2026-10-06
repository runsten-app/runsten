package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"runsten/internal/core"
)

// SetVehicleModel writes the choice like the store, without checking it.
func (m *memReader) SetVehicleModel(_ context.Context, acc, vehicle, variantID string, acMaxKW core.Value[float64]) error {
	m.writes++
	if m.modelErr != nil {
		return m.modelErr
	}
	for i, v := range m.vehicles[acc] {
		if v.ID == vehicle {
			m.vehicles[acc][i].VariantID, m.vehicles[acc][i].ACMaxKW = variantID, acMaxKW
			return nil
		}
	}
	return ErrNotFound
}

func TestEffectiveModel(t *testing.T) {
	cat := mustCatalog(t)
	const (
		single = "YV1EL3AV0R2000001" // motor code EL: an EX30 Single Motor
		other  = "YV1SMLT0000DT0001" // a code no EX30 lists: Single and Twin Motor alike
	)
	ex30 := details("EX30", 2024, 69.0, true)
	xc40 := details("XC40", 2021, 78.012, true)
	tests := []struct {
		name        string
		details     core.Snapshot
		vin, chosen string
		stated      core.Value[float64]
		// The variant in effect, its source, the charger stated and the one in effect.
		variant  string
		source   variantSourceJSON
		statedKW core.Value[float64]
		chargerK core.Value[float64]
	}{
		{"recognized, the option not stated: 22", ex30, single, "", core.Value[float64]{}, "ex30-er-2024", variantDetected, core.Value[float64]{}, some(22.0)},
		{"recognized, 11 stated", ex30, single, "", some(11.0), "ex30-er-2024", variantDetected, some(11.0), some(11.0)},
		{"recognized, 22 stated", ex30, single, "", some(22.0), "ex30-er-2024", variantDetected, some(22.0), some(22.0)},
		{"a charger of no variant: ignored", ex30, single, "", some(7.4), "ex30-er-2024", variantDetected, core.Value[float64]{}, some(22.0)},
		{"no option: the standard charger", xc40, other, "", core.Value[float64]{}, "xc40-twin-2021", variantDetected, core.Value[float64]{}, some(11.0)},
		{"the option of another variant: ignored", xc40, other, "", some(22.0), "xc40-twin-2021", variantDetected, core.Value[float64]{}, some(11.0)},
		{"chosen among the candidates", ex30, other, "ex30-twin-2024", some(11.0), "ex30-twin-2024", variantChosen, some(11.0), some(11.0)},
		{"chosen over the recognized", ex30, single, "ex30-lfp-2024", core.Value[float64]{}, "ex30-lfp-2024", variantChosen, core.Value[float64]{}, some(11.0)},
		{"chosen, the charger of the recognized no longer", ex30, single, "ex30-lfp-2024", some(22.0), "ex30-lfp-2024", variantChosen, core.Value[float64]{}, some(11.0)},
		{"chosen of another model year", ex30, other, "ex30-p8-2027", core.Value[float64]{}, "ex30-p8-2027", variantChosen, core.Value[float64]{}, some(22.0)},
		{"chosen, gone from the catalog: recognized", ex30, single, "ex30-gone", core.Value[float64]{}, "ex30-er-2024", variantDetected, core.Value[float64]{}, some(22.0)},
		{"chosen, of another family: recognized", xc40, other, "ex30-er-2024", some(22.0), "xc40-twin-2021", variantDetected, core.Value[float64]{}, some(11.0)},
		{"chosen, gone, none recognized", ex30, other, "ex30-gone", some(11.0), "", "", core.Value[float64]{}, core.Value[float64]{}},
		{"none chosen, none recognized", ex30, other, "", some(11.0), "", "", core.Value[float64]{}, core.Value[float64]{}},
		{"a hybrid: neither", details("EX30", 2024, 69.0, false), single, "ex30-er-2024", some(11.0), "", "", core.Value[float64]{}, core.Value[float64]{}},
		{"details never read", core.Snapshot{}, single, "ex30-er-2024", some(11.0), "", "", core.Value[float64]{}, core.Value[float64]{}},
		{"unknown family", details("EX-SIM", 2026, 80, true), single, "ex30-er-2024", core.Value[float64]{}, "", "", core.Value[float64]{}, core.Value[float64]{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := effectiveModel(cat, tt.details, Vehicle{VIN: tt.vin, VariantID: tt.chosen, ACMaxKW: tt.stated})
			if m.Variant.OK != (tt.variant != "") || m.Variant.V.ID != tt.variant || m.Source != tt.source {
				t.Errorf("variant %+v (%v), source %q; want %q, %q", m.Variant.V.ID, m.Variant.OK, m.Source, tt.variant, tt.source)
			}
			if m.StatedChargerKW != tt.statedKW || m.ChargerKW != tt.chargerK {
				t.Errorf("charger stated %+v, in effect %+v; want %+v, %+v", m.StatedChargerKW, m.ChargerKW, tt.statedKW, tt.chargerK)
			}
			if m.Family != tt.details.Family || m.ModelYear != tt.details.ModelYear {
				t.Errorf("family %+v, year %+v", m.Family, m.ModelYear)
			}
		})
	}
}

func TestVariantsJSON(t *testing.T) {
	e, c := readerEnv(t)
	resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+chosen+"/variants", c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "variants.json", body)

	ids := func(body string) string {
		t.Helper()
		var l struct {
			Items []struct {
				ID        string `json:"id"`
				Candidate bool   `json:"candidate"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &l); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, v := range l.Items {
			id := v.ID
			if v.Candidate {
				id += "*"
			}
			out = append(out, id)
		}
		return strings.Join(out, " ")
	}
	for _, tt := range []struct {
		name    string
		vin     string
		details core.Snapshot
		want    string
	}{
		{
			"recognized: the only candidate", "YV1SMLT0000DT0001", details("XC40", 2021, 78.012, true),
			"xc40-twin-2021* xc40-single-2022 xc40-single-2024 xc40-er-2024 xc40-twin-2024",
		},
		{
			"nothing fits: no candidate", "YV1SMLT0000DT0001", details("XC40", 2021, 50, true),
			"xc40-twin-2021 xc40-single-2022 xc40-single-2024 xc40-er-2024 xc40-twin-2024",
		},
		{
			"every one fits: no candidate", "",
			core.Snapshot{Covers: core.FieldModel, Family: some("Polestar 3"), BatteryElectric: some(true)},
			"ps3-2024 ps3-rear-2026 ps3-dual-2026",
		},
		{"unknown family", "", details("EX-SIM", 2026, 80, true), ""},
		{"a hybrid", "", details("XC40", 2021, 78.012, false), ""},
		{"details never read", "", core.Snapshot{}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e.reader.vehicles[account][0].VIN = tt.vin
			e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 0), Snapshot: tt.details}}
			resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/variants", c)
			if resp.status != http.StatusOK {
				t.Fatalf("status %d: %s", resp.status, body)
			}
			if got := ids(body); got != tt.want {
				t.Errorf("variants %q\nwant     %q", got, tt.want)
			}
		})
	}
}

func TestPutVehicleModel(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/vehicles/"
	put := func(vehicle, body string) (reply, string) {
		t.Helper()
		return do(t, noFollow(), http.MethodPut, u+vehicle+"/model", jsonType, body, c)
	}
	model := func(body string) string {
		t.Helper()
		var v struct {
			Model json.RawMessage `json:"model"`
		}
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatal(err)
		}
		return string(v.Model)
	}

	// The choice of the fixtures, written again: the golden response.
	resp, body := put(chosen, `{"variant_id":"ex30-er-2024","ac_max_kw":11}`)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "vehicle-model.json", body)

	for _, tt := range []struct{ name, body, want string }{
		{
			"the Twin Motor, charger not stated", `{"variant_id":"ex30-twin-2024","ac_max_kw":null}`,
			`{"family":"EX30","model_year":2024,"variant":{"id":"ex30-twin-2024","name":"EX30 Twin Motor Performance / Cross Country",` +
				`"gross_kwh":69,"net_kwh":64,"ac_max_kw":11,"ac_option_kw":22,"dc_max_kw":153},"variant_source":"chosen","ac_max_kw":null}`,
		},
		{
			"back to the recognition: none", `{"variant_id":null,"ac_max_kw":null}`,
			`{"family":"EX30","model_year":2024,"variant":null,"variant_source":null,"ac_max_kw":null}`,
		},
	} {
		resp, body := put(chosen, tt.body)
		if resp.status != http.StatusOK || model(body) != tt.want {
			t.Errorf("%s: %d\n%s\nwant %s", tt.name, resp.status, model(body), tt.want)
		}
		// What was written shows on every read.
		if _, read := get(t, noFollow(), u+chosen, c); model(read) != tt.want {
			t.Errorf("%s, read again: %s", tt.name, model(read))
		}
	}

	// The recognized XC40: its charger can be stated without choosing the variant.
	resp, body = put(car, `{"variant_id":null,"ac_max_kw":11}`)
	if resp.status != http.StatusOK || !strings.Contains(body, `"variant_source":"detected","ac_max_kw":11}`) {
		t.Errorf("charger of the recognized variant: %d %s", resp.status, body)
	}

	writes := e.reader.writes
	for _, tt := range []struct{ name, vehicle, body, field string }{
		{"unknown variant", chosen, `{"variant_id":"ex30-gone","ac_max_kw":null}`, "variant_id"},
		{"another family", chosen, `{"variant_id":"xc40-twin-2021","ac_max_kw":null}`, "variant_id"},
		{"no family read", parked, `{"variant_id":"ex30-er-2024","ac_max_kw":null}`, "variant_id"},
		{"a charger of no variant", chosen, `{"variant_id":"ex30-er-2024","ac_max_kw":7.4}`, "ac_max_kw"},
		{"the option the variant lacks", chosen, `{"variant_id":"ex30-lfp-2024","ac_max_kw":22}`, "ac_max_kw"},
		{"the option the recognized lacks", car, `{"variant_id":null,"ac_max_kw":22}`, "ac_max_kw"},
		{"a charger without a variant", chosen, `{"variant_id":null,"ac_max_kw":11}`, "ac_max_kw"},
		{"a charger of zero", chosen, `{"variant_id":"ex30-er-2024","ac_max_kw":0}`, "ac_max_kw"},
		{"an empty ID", chosen, `{"variant_id":"","ac_max_kw":null}`, "variant_id"},
		{"a key missing", chosen, `{"variant_id":"ex30-er-2024"}`, "ac_max_kw"},
		{"an unknown key", chosen, `{"variant_id":null,"ac_max_kw":null,"id":"x"}`, "id"},
		{"not JSON", chosen, `{`, ""},
	} {
		resp, body := put(tt.vehicle, tt.body)
		if resp.status != http.StatusBadRequest || !strings.Contains(body, tt.field) {
			t.Errorf("%s: %d %s", tt.name, resp.status, body)
		}
		wantError(t, body, codeInvalidBody)
	}
	for _, contentType := range []string{"", "text/plain"} {
		resp, body := do(t, noFollow(), http.MethodPut, u+chosen+"/model", contentType, `{"variant_id":null,"ac_max_kw":null}`, c)
		if resp.status != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: %d", contentType, resp.status)
		}
		wantError(t, body, codeUnsupportedMediaType)
	}
	for _, id := range []string{foreign, "0b5c6c3e-3f0e-4a57-9d3b-2f4f9e8d1a09", "not-a-uuid"} {
		for _, req := range []struct{ method, suffix, body string }{
			{http.MethodPut, "/model", `{"variant_id":null,"ac_max_kw":null}`},
			{http.MethodGet, "/variants", ""},
		} {
			resp, body := do(t, noFollow(), req.method, u+id+req.suffix, jsonType, req.body, c)
			if resp.status != http.StatusNotFound {
				t.Errorf("%s %s%s: %d", req.method, id, req.suffix, resp.status)
			}
			wantError(t, body, codeNotFound)
		}
	}
	if e.reader.writes != writes {
		t.Errorf("%d refused writes reached the store", e.reader.writes-writes)
	}
	if v := e.reader.vehicles["other"][0]; v.VariantID != "" || v.ACMaxKW.OK {
		t.Errorf("another account's vehicle written: %+v", v)
	}

	// The vehicle gone between the read and the write.
	e.reader.modelErr = ErrNotFound
	if resp, body := put(chosen, `{"variant_id":null,"ac_max_kw":null}`); resp.status != http.StatusNotFound {
		t.Errorf("gone: %d %s", resp.status, body)
	}
}

func TestVehicleModelErrors(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	u := e.api.URL + "/api/v1/vehicles/" + chosen
	e.reader.modelErr = errors.New("boom")
	resp, body := do(t, noFollow(), http.MethodPut, u+"/model", jsonType, `{"variant_id":null,"ac_max_kw":null}`, c)
	if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
		t.Errorf("write failed: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)

	// The details not read: the reader fails once the vehicle is found.
	e.reader.modelErr = nil
	e.server.States = failingStates{}
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/variants", ""},
		{http.MethodPut, "/model", `{"variant_id":null,"ac_max_kw":null}`},
	} {
		resp, body := do(t, noFollow(), req.method, u+req.path, jsonType, req.body, c)
		if resp.status != http.StatusInternalServerError {
			t.Errorf("%s %s: %d %s", req.method, req.path, resp.status, body)
		}
		wantError(t, body, codeInternal)
	}
	if !strings.Contains(logs.String(), "boom") || !strings.Contains(logs.String(), errEvents.Error()) {
		t.Errorf("the cause is not logged:\n%s", logs)
	}
}
