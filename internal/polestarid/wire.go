package polestarid

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

// The messages of the gRPC services, as pypolestar reconstructed them from the Android app
// 5.5.0 (pypolestar/proto/*.proto): decoded by hand with protowire, so that no generated
// code nor gRPC library is needed for two calls. A response is stored as JSON, its fields
// named as protobuf's JSON mapping names them (lowerCamelCase, an int64 in a string, an
// enum by its name), a field no message declares under its number: what a car gives that
// the reconstruction does not know is kept, and seen in the logs of the uploads.

// kind is how a field's value is read.
type kind int

const (
	kindSkip    kind = iota // read, not kept: the request's ID echoed, which changes at every call
	kindDouble              // double
	kindInt32               // int32
	kindInt64               // int64, written as a string
	kindString              // string
	kindEnum                // enum: its name, or its number when the message does not know it
	kindMessage             // message
)

// field is a field of a message.
type field struct {
	name  string
	kind  kind
	enum  []string // kindEnum: the names, by number
	inner message  // kindMessage
}

// message is a message's fields, by number.
type message map[protowire.Number]field

var (
	// pccs.vehiclestates.entities.common.v1.Timestamp.
	timestampMessage = message{1: {name: "seconds", kind: kindInt64}, 2: {name: "nanos", kind: kindInt32}}
	// EnergyConsumptionWh and EnergyConsumptionPercentage: a double in a message.
	valueMessage = message{1: {name: "value", kind: kindDouble}}

	// pccs.vehiclestates.entities.battery.v1.Battery: the Data Portal's battery domain is
	// the same entity, with the same fields.
	batteryMessage = message{
		1:  {name: "timestamp", kind: kindMessage, inner: timestampMessage},
		2:  {name: "batteryChargeLevelPercentage", kind: kindDouble},
		3:  {name: "averageEnergyConsumptionKwhPer100Km", kind: kindDouble},
		4:  {name: "estimatedDistanceToEmptyKm", kind: kindInt32},
		5:  {name: "estimatedChargingTimeToFullMinutes", kind: kindInt32},
		6:  {name: "chargerConnectionStatus", kind: kindEnum, enum: []string{"CHARGER_CONNECTION_STATUS_UNSPECIFIED", "CHARGER_CONNECTION_STATUS_CONNECTED", "CHARGER_CONNECTION_STATUS_DISCONNECTED", "CHARGER_CONNECTION_STATUS_FAULT"}},
		7:  {name: "chargingStatus", kind: kindEnum, enum: []string{"CHARGING_STATUS_UNSPECIFIED", "CHARGING_STATUS_CHARGING", "CHARGING_STATUS_IDLE", "CHARGING_STATUS_SCHEDULED", "CHARGING_STATUS_DISCHARGING", "CHARGING_STATUS_ERROR", "CHARGING_STATUS_SMART_CHARGING", "CHARGING_STATUS_DONE", "CHARGING_STATUS_SMART_CHARGING_PAUSED"}},
		8:  {name: "estimatedDistanceToEmptyMiles", kind: kindInt32},
		9:  {name: "estimatedChargingTimeMinutesToTargetDistance", kind: kindInt32},
		10: {name: "chargingPowerWatts", kind: kindInt32},
		11: {name: "chargingCurrentAmps", kind: kindInt32},
		12: {name: "averageEnergyConsumptionKwhPer100KmAutomatic", kind: kindDouble},
		13: {name: "averageEnergyConsumptionKwhPer100KmSinceCharge", kind: kindDouble},
		14: {name: "totalEnergyConsumptionWh", kind: kindDouble},
		15: {name: "totalEnergyConsumptionWhAutomatic", kind: kindDouble},
		16: {name: "totalEnergyConsumptionWhSinceCharge", kind: kindDouble},
		17: {name: "chargingType", kind: kindEnum, enum: []string{"CHARGING_TYPE_UNSPECIFIED", "CHARGING_TYPE_NONE", "CHARGING_TYPE_AC", "CHARGING_TYPE_DC", "CHARGING_TYPE_WIRELESS"}},
		18: {name: "chargingVoltageVolts", kind: kindInt32},
		19: {name: "estimatedChargingTimeMinutesToMinimumSoc", kind: kindInt32},
		20: {name: "energyConsumptionWhManual", kind: kindMessage, inner: valueMessage},
		21: {name: "energyConsumptionWhAutomatic", kind: kindMessage, inner: valueMessage},
		22: {name: "energyConsumptionWhSinceCharge", kind: kindMessage, inner: valueMessage},
		23: {name: "energyConsumptionPercentageManual", kind: kindMessage, inner: valueMessage},
		24: {name: "energyConsumptionPercentageAutomatic", kind: kindMessage, inner: valueMessage},
		25: {name: "energyConsumptionPercentageSinceCharge", kind: kindMessage, inner: valueMessage},
		// Its values are not known: written by number.
		26: {name: "chargerPowerStatus", kind: kindEnum, enum: []string{"CHARGER_POWER_STATUS_UNSPECIFIED"}},
	}
	// pccs.vehiclestates.services.battery.v1.GetBatteryResponse.
	batteryResponse = message{
		1: {name: "id", kind: kindSkip},
		2: {name: "vin", kind: kindString},
		3: {name: "battery", kind: kindMessage, inner: batteryMessage},
	}

	// pccs.chronos.messages.targetsoc.v1.TargetSoc.
	targetSoCMessage = message{
		1: {name: "batteryChargeTargetLevel", kind: kindInt32},
		2: {name: "chargeTargetLevelSettingType", kind: kindEnum, enum: []string{"CHARGE_TARGET_LEVEL_SETTING_TYPE_UNSPECIFIED", "DAILY", "LONG_TRIP", "CUSTOM"}},
		3: {name: "updatedAt", kind: kindInt64},
		4: {name: "source", kind: kindString},
		5: {name: "id", kind: kindString},
	}
	// pccs.chronos.messages.targetsoc.v1.GetTargetSocResponse.
	targetSoCResponse = message{
		1: {name: "id", kind: kindSkip},
		2: {name: "vin", kind: kindString},
		3: {name: "targetSoc", kind: kindMessage, inner: targetSoCMessage},
		4: {name: "pendingTargetSoc", kind: kindMessage, inner: targetSoCMessage},
		5: {name: "updatedAt", kind: kindInt64},
	}
)

// maxDepth bounds the nesting of the messages decoded: those declared nest two deep.
const maxDepth = 8

var errWire = errors.New("invalid protobuf")

// decode reads the wire bytes b of message m into its JSON mapping.
func decode(b []byte, m message) (map[string]any, error) { return decodeAt(b, m, 0) }

func decodeAt(b []byte, m message, depth int) (map[string]any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%w: nested too deep", errWire)
	}
	out := map[string]any{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, fmt.Errorf("%w: %w", errWire, protowire.ParseError(n))
		}
		b = b[n:]
		f, known := m[num]
		name := f.name
		if !known {
			name = strconv.Itoa(int(num))
		}
		var v any
		switch typ {
		case protowire.VarintType:
			x, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return nil, fmt.Errorf("%w: field %d: %w", errWire, num, protowire.ParseError(n))
			}
			b = b[n:]
			v = varint(f, known, x)
		case protowire.Fixed64Type:
			x, n := protowire.ConsumeFixed64(b)
			if n < 0 {
				return nil, fmt.Errorf("%w: field %d: %w", errWire, num, protowire.ParseError(n))
			}
			b = b[n:]
			v = x
			if known && f.kind == kindDouble {
				d := math.Float64frombits(x)
				if math.IsNaN(d) || math.IsInf(d, 0) {
					continue // JSON has neither: unknown
				}
				v = d
			}
		case protowire.Fixed32Type:
			x, n := protowire.ConsumeFixed32(b)
			if n < 0 {
				return nil, fmt.Errorf("%w: field %d: %w", errWire, num, protowire.ParseError(n))
			}
			b = b[n:]
			v = x
		case protowire.BytesType:
			x, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, fmt.Errorf("%w: field %d: %w", errWire, num, protowire.ParseError(n))
			}
			b = b[n:]
			var err error
			if v, err = bytesValue(f, known, x, depth); err != nil {
				return nil, fmt.Errorf("field %d: %w", num, err)
			}
		default:
			// A group, which proto3 has not: skipped, as an unknown field is.
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return nil, fmt.Errorf("%w: field %d: %w", errWire, num, protowire.ParseError(n))
			}
			b = b[n:]
			continue
		}
		if known && f.kind == kindSkip {
			continue
		}
		// The last value of a field wins, as protobuf reads a scalar repeated.
		out[name] = v
	}
	return out, nil
}

// varint reads a varint field's value; a known field of another kind is kept as an
// unknown one would be.
func varint(f field, known bool, x uint64) any {
	if !known {
		return x
	}
	switch f.kind {
	case kindInt32:
		return int64(int32(x)) //nolint:gosec // protobuf's int32: the low 32 bits, sign-extended
	case kindInt64:
		return strconv.FormatInt(int64(x), 10) //nolint:gosec // protobuf's int64, two's complement
	case kindEnum:
		if i := int32(x); i >= 0 && int(i) < len(f.enum) { //nolint:gosec // an enum is an int32
			return f.enum[i]
		}
		return int64(int32(x)) //nolint:gosec // an enum is an int32
	}
	return x
}

// bytesValue reads a length-delimited field's value: a string, a message, else base64.
func bytesValue(f field, known bool, x []byte, depth int) (any, error) {
	switch {
	case known && f.kind == kindMessage:
		return decodeAt(x, f.inner, depth+1)
	case known && (f.kind == kindString || f.kind == kindSkip):
		if !utf8.Valid(x) {
			return nil, fmt.Errorf("%w: a string not in UTF-8", errWire)
		}
		return string(x), nil
	}
	return base64.StdEncoding.EncodeToString(x), nil
}

// batteryRequest is GetBatteryRequest{id, vin}.
func batteryRequest(id, vin string) []byte {
	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	b = protowire.AppendString(b, id)
	b = protowire.AppendTag(b, 2, protowire.BytesType)
	return protowire.AppendString(b, vin)
}

// targetSoCRequest is GetTargetSocRequest{request: ChronosRequest{id, vin, source}}, the
// source "mobile" as the app (pypolestar) sends it.
func targetSoCRequest(id, vin string) []byte {
	inner := protowire.AppendTag(nil, 1, protowire.BytesType)
	inner = protowire.AppendString(inner, id)
	inner = protowire.AppendTag(inner, 2, protowire.BytesType)
	inner = protowire.AppendString(inner, vin)
	inner = protowire.AppendTag(inner, 3, protowire.BytesType)
	inner = protowire.AppendString(inner, "mobile")
	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(b, inner)
}
