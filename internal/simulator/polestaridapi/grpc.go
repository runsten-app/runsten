package polestaridapi

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"runsten/internal/simulator/vehicle"
)

// The gRPC services the app reads beyond the GraphQL (pypolestar's reconstruction of the
// Android app 5.5.0): the battery on Volvo's C3, whose host a discovery service answers,
// and the charge limit on Polestar's PCCS. The real hosts serve the methods at their
// root: so does the simulator, which answers the discovery with its own host. Spoken over
// HTTP/2, unencrypted with prior knowledge: the simulator's server must accept it.
const (
	// DiscoveryPath is the discovery service's, at the root of
	// https://cnepmob.volvocars.com on the real one.
	DiscoveryPath = Prefix + "cnepmob"
	// BatteryPath is GetLatestBattery's, on C3.
	BatteryPath = "/services.vehiclestates.battery.BatteryService/GetLatestBattery"
	// TargetSoCPath is GetTargetSoc's, on PCCS.
	TargetSoCPath = "/pccs.chronos.services.v1.TargetSocService/GetTargetSoc"
)

// gRPC's status codes the simulator answers.
const (
	grpcOK               = 0
	grpcInvalidArgument  = 3
	grpcNotFound         = 5
	grpcPermissionDenied = 7
	grpcUnauthenticated  = 16
)

// discovery answers C3's host and port: the simulator's own, as the request reached it.
// The real answer's shape beyond these two fields is not known (pypolestar reads them).
func (h *Handler) discovery(w http.ResponseWriter, r *http.Request) {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host, port = r.Host, "80"
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		http.Error(w, "bad host", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/volvo.cloud.cnepmob.v1+json")
	_ = json.NewEncoder(w).Encode(map[string]any{"c3": map[string]any{"grpcHost": host, "grpcPort": p}})
}

// battery answers GetLatestBattery: the car's battery, as the Data Portal's simulator
// renders the same entity.
func (h *Handler) battery(w http.ResponseWriter, r *http.Request) {
	now := h.clk.Now()
	vin, src, ok := h.grpcCar(w, r, now)
	if !ok {
		return
	}
	// The request's ID, which the real one echoes, is left out: the client does not read it.
	resp := appendString(nil, 2, vin)
	resp = appendMessage(resp, 3, h.renderGRPCBattery(src.ReportAt(now), now))
	writeGRPC(w, resp, grpcOK, "")
}

// targetSoC answers GetTargetSoc, a stream, with one message: the charge limit the
// scenario's last charge set. A Polestar 2 is not provisioned on PCCS: PERMISSION_DENIED,
// as pypolestar saw.
func (h *Handler) targetSoC(w http.ResponseWriter, r *http.Request) {
	now := h.clk.Now()
	vin, src, ok := h.grpcCar(w, r, now)
	if !ok {
		return
	}
	rep := src.ReportAt(now)
	if rep.Spec.Model == "Polestar 2" {
		writeGRPC(w, nil, grpcPermissionDenied, "Permission denied")
		return
	}
	// The instant of the setting is the last energy upload: the real one is unknown.
	set := rep.EnergyAt.Unix()
	target := appendVarint(nil, 1, uint64(int64(rep.Energy.TargetSoC))) //nolint:gosec // a percentage
	target = appendVarint(target, 2, 3)                                 // CUSTOM
	target = appendVarint(target, 3, uint64(set))                       //nolint:gosec // after 1970
	target = appendString(target, 4, "mobile")
	target = appendString(target, 5, sign("target-soc", vin)) // the setting's, the same at every read
	resp := appendString(nil, 2, vin)
	resp = appendMessage(resp, 3, target)
	resp = appendVarint(resp, 5, uint64(set)) //nolint:gosec // after 1970
	writeGRPC(w, resp, grpcOK, "")
}

// grpcCar checks a call's access and its car, named by the vin metadata, and answers the
// error otherwise.
func (h *Handler) grpcCar(w http.ResponseWriter, r *http.Request, now time.Time) (string, Source, bool) {
	if !h.authorized(r, now) {
		// Assumed: the real gateway's answer to an expired token is not seen yet.
		writeGRPC(w, nil, grpcUnauthenticated, "Unauthenticated")
		return "", nil, false
	}
	if err := readRequest(r); err != nil {
		writeGRPC(w, nil, grpcInvalidArgument, "invalid request")
		return "", nil, false
	}
	vin := r.Header.Get("Vin")
	src, ok := h.srcs[vin]
	if !ok {
		writeGRPC(w, nil, grpcNotFound, "Vehicle not found")
		return "", nil, false
	}
	return vin, src, true
}

// renderGRPCBattery is the Battery message: what the car uploaded last, its power, current
// and voltage only while charging (protobuf leaves a zero out), and only on a model that
// reports them.
func (h *Handler) renderGRPCBattery(r vehicle.Report, now time.Time) []byte {
	e := r.Energy
	at := h.energyAt(r, now)
	connection := uint64(2) // DISCONNECTED
	if e.PluggedIn {
		connection = 1 // CONNECTED
	}
	status := uint64(2) // IDLE
	switch e.Charging {
	case vehicle.Charging:
		status = 1
	case vehicle.Done:
		status = 7
	case vehicle.Idle:
	}
	typ := uint64(1) // NONE
	switch e.ChargeType {
	case vehicle.AC:
		typ = 2
	case vehicle.DC:
		typ = 3
	case vehicle.NoCharge:
	}
	km := int(e.SoC / 100 * r.Spec.UsableKWhAt(at) / r.Spec.ConsumptionKWhPer100km * 100)
	stamp := appendVarint(nil, 1, uint64(at.Unix())) //nolint:gosec // after 1970
	b := appendMessage(nil, 1, stamp)
	b = appendDouble(b, 2, float64(int(e.SoC)))
	if e.TotalDistanceKm > 0 {
		b = appendDouble(b, 3, math.Round(e.TotalEnergyKWh/e.TotalDistanceKm*1000)/10)
	}
	b = appendVarint(b, 4, uint64(km)) //nolint:gosec // a range
	if e.Charging == vehicle.Charging && e.ChargingPowerW > 0 {
		kwh := (e.TargetSoC - e.SoC) / 100 * r.Spec.UsableKWhAt(at)
		b = appendVarint(b, 5, uint64(math.Ceil(kwh/(e.ChargingPowerW/1000)*60))) //nolint:gosec // minutes
	}
	b = appendVarint(b, 6, connection)
	b = appendVarint(b, 7, status)
	b = appendVarint(b, 8, uint64(float64(km)/1.609344))
	if e.Charging == vehicle.Charging && e.ChargingPowerW > 0 && r.Spec.Capabilities.ChargingPower.While(true) == vehicle.Supported {
		const volts = 400 // assumption, as the Data Portal's simulator: three-phase AC or a DC charge
		b = appendVarint(b, 10, uint64(e.ChargingPowerW))
		b = appendVarint(b, 11, uint64(math.Round(e.ChargingPowerW/volts)))
		b = appendVarint(b, 18, volts)
	}
	return appendVarint(b, 17, typ)
}

// energyAt is when the car uploaded its battery: on a change, and every ParkedUpload while
// it does not drive.
func (h *Handler) energyAt(r vehicle.Report, now time.Time) time.Time {
	at := r.EnergyAt
	if every := h.behavior.ParkedUpload; every > 0 && !r.Status.Driving && !now.Before(at) {
		at = at.Add(now.Sub(at) / every * every)
	}
	return at
}

// readRequest reads a call's message, a frame: the car is the vin metadata's, as the
// real one reads it (the message names it too).
func readRequest(r *http.Request) error {
	var head [5]byte
	if _, err := io.ReadFull(r.Body, head[:]); err != nil {
		return err //nolint:wrapcheck // answered as an invalid request
	}
	n := binary.BigEndian.Uint32(head[1:])
	if head[0] != 0 || n > 1<<16 {
		return io.ErrUnexpectedEOF
	}
	_, err := io.ReadFull(r.Body, make([]byte, n))
	return err //nolint:wrapcheck // answered as an invalid request
}

// writeGRPC answers a call: its message, if any, then the status in the trailers; without
// a message, the status in the headers (a trailers-only response), as gRPC servers do.
func writeGRPC(w http.ResponseWriter, msg []byte, code int, message string) {
	w.Header().Set("Content-Type", "application/grpc")
	if msg == nil {
		w.Header().Set("Grpc-Status", strconv.Itoa(code))
		w.Header().Set("Grpc-Message", message)
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
	w.WriteHeader(http.StatusOK)
	frame := make([]byte, 5, 5+len(msg))
	binary.BigEndian.PutUint32(frame[1:], uint32(len(msg))) //nolint:gosec // a message of a few bytes
	_, _ = w.Write(append(frame, msg...))                   //nolint:gosec // protobuf, not a page; the header is sent: nothing more to do
	w.Header().Set("Grpc-Status", strconv.Itoa(code))
	w.Header().Set("Grpc-Message", message)
}

func appendVarint(b []byte, num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(b, num, protowire.VarintType), v)
}

func appendDouble(b []byte, num protowire.Number, v float64) []byte {
	return protowire.AppendFixed64(protowire.AppendTag(b, num, protowire.Fixed64Type), math.Float64bits(v))
}

func appendString(b []byte, num protowire.Number, v string) []byte {
	return protowire.AppendString(protowire.AppendTag(b, num, protowire.BytesType), v)
}

func appendMessage(b []byte, num protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(b, num, protowire.BytesType), v)
}
