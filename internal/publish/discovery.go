package publish

import (
	"encoding/json"
	"strconv"

	"runsten/internal/core"
)

// Home Assistant's MQTT discovery: one retained configuration per entity, under
// <discovery prefix>/<component>/runsten_<vehicle id>/<object>/config, which creates
// the entity, grouped in a device per vehicle, and reads the topics of State. An empty
// configuration removes the entity.
//
// Home Assistant's MQTT integration (checked against 2026.9) ignores an empty payload,
// or refuses it with a warning, and takes "None" for unknown: the templates turn the
// empty payload of an unknown value into "None", so that the entity says unknown rather
// than keeping the previous value. A device tracker keeps its position on attributes
// without one, and drops it on null coordinates (with a warning in its log).

// Model is what a vehicle is taken to be, for its device: the variant in effect from
// the catalog, else the family the vendor reports. Empty fields are unknown.
type Model struct {
	Variant string // the variant's name
	Family  string
	Brand   string // as written: Volvo, Polestar
	Year    int    // model year; 0: unknown
}

// idDigits of a vehicle's ID tell it apart where nothing else does.
const idDigits = 6

// Labels names the vehicles of an account, by ID, as the web interface does, but never
// by their VIN: the variant, else the family, with the model year; before the details
// are read, Volvo and the first characters of its ID. Two that read alike end with the
// first characters of their ID.
func Labels(models map[string]Model) map[string]string {
	labels := make(map[string]string, len(models))
	count := map[string]int{}
	for id, m := range models {
		l := label(id, m)
		labels[id] = l
		count[l]++
	}
	for id, l := range labels {
		if count[l] > 1 {
			labels[id] = l + " · " + short(id)
		}
	}
	return labels
}

func label(id string, m Model) string {
	name := m.name()
	if name == "" {
		return "Volvo " + short(id)
	}
	if m.Year > 0 {
		return name + " · " + strconv.Itoa(m.Year)
	}
	return name
}

func (m Model) name() string {
	if m.Variant != "" {
		return m.Variant
	}
	return m.Family
}

func short(id string) string { return id[:min(idDigits, len(id))] }

// entity is an entity of a vehicle's device.
type entity struct {
	component string // sensor, binary_sensor, device_tracker
	object    string
	name      string
	topic     string // the State topic it reads
	config    config
}

// unknown is a value template that takes an empty payload for unknown.
const unknown = "{{ value if value != '' else 'None' }}"

// onOff is a value template of a binary sensor on a text value: on, off, or unknown for
// any other (a cable fault, an error).
func onOff(on, off string) string {
	return "{{ 'ON' if value == '" + on + "' else ('OFF' if value == '" + off + "' else 'None') }}"
}

// chargingTemplate tells whether the vehicle charges, from its charging status: unknown
// only when the status is.
const chargingTemplate = "{{ 'ON' if value == 'charging' else ('OFF' if value != '' else 'None') }}"

func entities() []entity {
	sensor := func(object, name, topic, deviceClass, unit, stateClass string) entity {
		return entity{"sensor", object, name, topic, config{
			DeviceClass: deviceClass, Unit: unit, StateClass: stateClass, ValueTemplate: unknown,
		}}
	}
	enum := func(object, name, topic string, options ...string) entity {
		return entity{"sensor", object, name, topic, config{DeviceClass: "enum", Options: options, ValueTemplate: unknown}}
	}
	binary := func(object, name, topic, deviceClass, template string) entity {
		return entity{"binary_sensor", object, name, topic, config{
			DeviceClass: deviceClass, ValueTemplate: template, PayloadOn: "ON", PayloadOff: "OFF",
		}}
	}
	readAt := sensor("read_at", "Last read", "read_at", "timestamp", "", "")
	readAt.config.EntityCategory = "diagnostic"
	return []entity{
		sensor("battery_level", "Battery", "battery_level", "battery", "%", "measurement"),
		sensor("range", "Range", "range_km", "distance", "km", "measurement"),
		enum("charging_status", "Charging status", "charging_status", values(
			core.ChargingIdle, core.ChargingActive, core.ChargingDone, core.ChargingScheduled,
			core.ChargingDischarging, core.ChargingError)...),
		enum("charge_type", "Charge type", "charge_type", values(core.AC, core.DC)...),
		sensor("charging_power", "Charging power", "charging_power_kw", "power", "kW", "measurement"),
		sensor("target_soc", "Charge limit", "target_soc", "", "%", ""),
		sensor("odometer", "Odometer", "odometer_km", "distance", "km", "total_increasing"),
		readAt,
		binary("plug", "Plug", "plug", "plug", onOff(string(core.Connected), string(core.Disconnected))),
		binary("charging", "Charging", "charging_status", "battery_charging", chargingTemplate),
		binary("engine", "Engine", "engine", "running", onOff(string(core.EngineRunning), string(core.EngineStopped))),
		// Its position from the attributes of the location's JSON; an empty location
		// makes it unknown.
		{"device_tracker", "location", "Location", "location", config{
			SourceType: "gps", JSONAttributesTemplate: `{{ value if value != '' else '{"latitude": null, "longitude": null}' }}`,
		}},
	}
}

func values[T ~string](vs ...T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

// config is an entity's discovery configuration, as Home Assistant reads it.
type config struct {
	Name                   string   `json:"name"`
	UniqueID               string   `json:"unique_id"`
	StateTopic             string   `json:"state_topic,omitempty"`
	JSONAttributesTopic    string   `json:"json_attributes_topic,omitempty"`
	JSONAttributesTemplate string   `json:"json_attributes_template,omitempty"`
	ValueTemplate          string   `json:"value_template,omitempty"`
	DeviceClass            string   `json:"device_class,omitempty"`
	StateClass             string   `json:"state_class,omitempty"`
	Unit                   string   `json:"unit_of_measurement,omitempty"`
	Options                []string `json:"options,omitempty"`
	PayloadOn              string   `json:"payload_on,omitempty"`
	PayloadOff             string   `json:"payload_off,omitempty"`
	SourceType             string   `json:"source_type,omitempty"`
	EntityCategory         string   `json:"entity_category,omitempty"`
	AvailabilityTopic      string   `json:"availability_topic"`
	PayloadAvailable       string   `json:"payload_available"`
	PayloadNotAvailable    string   `json:"payload_not_available"`
	QoS                    int      `json:"qos"`
	Device                 device   `json:"device"`
	Origin                 origin   `json:"origin"`
}

type device struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
}

type origin struct {
	Name       string `json:"name"`
	SupportURL string `json:"support_url"`
}

// discoveryTopic is where an entity's configuration goes.
func discoveryTopic(b Broker, vehicleID string, e entity) string {
	return b.DiscoveryPrefix + "/" + e.component + "/runsten_" + vehicleID + "/" + e.object + "/config"
}

// Discovery is the configuration of each entity of the vehicle, retained, named label
// (Labels). Without the location published, the device tracker's is empty: it removes
// the one published before.
func Discovery(b Broker, vehicleID, label string, m Model) []Message {
	base := b.TopicPrefix + "/vehicles/" + vehicleID + "/"
	dev := device{Identifiers: []string{"runsten_" + vehicleID}, Name: label, Manufacturer: m.Brand, Model: m.name()}
	es := entities()
	msgs := make([]Message, 0, len(es))
	for _, e := range es {
		msg := Message{Topic: discoveryTopic(b, vehicleID, e), Retained: true}
		if e.component != "device_tracker" || b.PublishLocation {
			c := e.config
			c.Name, c.UniqueID = e.name, "runsten_"+vehicleID+"_"+e.object
			if e.component == "device_tracker" {
				c.JSONAttributesTopic = base + e.topic
			} else {
				c.StateTopic = base + e.topic
			}
			c.AvailabilityTopic, c.PayloadAvailable, c.PayloadNotAvailable = AvailabilityTopic(b.TopicPrefix), Online, Offline
			c.QoS, c.Device = qos, dev
			c.Origin = origin{Name: "Runsten", SupportURL: "https://runsten.app"}
			payload, err := json.Marshal(c)
			if err != nil {
				panic(err) // a struct of strings always marshals
			}
			msg.Payload = string(payload)
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// qos is the quality of service of every message: at least once.
const qos = 1

// RemoveDiscovery empties the configuration of each entity of the vehicle: Home
// Assistant removes them.
func RemoveDiscovery(b Broker, vehicleID string) []Message {
	es := entities()
	msgs := make([]Message, len(es))
	for i, e := range es {
		msgs[i] = Message{Topic: discoveryTopic(b, vehicleID, e), Retained: true}
	}
	return msgs
}

// RemoveState empties the vehicle's state topics: the broker keeps none of its values.
func RemoveState(b Broker, vehicleID string) []Message {
	msgs := State(b, vehicleID, core.Current{})
	for i := range msgs {
		msgs[i].Payload = ""
	}
	return msgs
}
