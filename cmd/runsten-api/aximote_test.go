package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"runsten/internal/api"
	"runsten/internal/aximote"
	"runsten/internal/simulator/aximoteapi"
)

func TestAximoteVehicles(t *testing.T) {
	srv := httptest.NewServer(aximoteapi.NewHandler(aximoteapi.DevToken, aximoteapi.Default(time.Now(), "YV1SMLT0000ER0001"))) //nolint:forbidigo // the fake's history only
	defer srv.Close()
	a := aximoteVehicles{aximote.NewClient(srv.URL, srv.Client())}
	vs, err := a.AximoteVehicles(context.Background(), aximoteapi.DevToken)
	if err != nil {
		t.Fatal(err)
	}
	want := []api.AximoteVehicle{
		{ID: "veh-ex30", Name: "My EX30", Make: "Volvo", Model: "EX30", VIN: "YV1SMLT0000ER0001", Year: 2024},
		{ID: "veh-v60", Name: "Old V60", Make: "Volvo", Model: "V60", Year: 2016},
	}
	if len(vs) != 2 || vs[0] != want[0] || vs[1] != want[1] {
		t.Errorf("vehicles %+v", vs)
	}
	_, err = a.AximoteVehicles(context.Background(), "axm_pat_00000000000000000000000000")
	var r interface{ TokenRefused() bool }
	if !errors.As(err, &r) || !r.TokenRefused() {
		t.Errorf("a token refused: %v", err)
	}
}
