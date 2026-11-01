package adsc

import (
	"fmt"
	"math"
)

var (
	downlinkTags map[uint8]*typeDesc
	uplinkTags   map[uint8]*typeDesc
	requestTags  map[uint8]*typeDesc
)

func init() {
	basic := func(label, key string) *typeDesc {
		return &typeDesc{label, key, parseBasicReport, textBasicReport, jsonBasicReport}
	}
	empty := func(label, key string) *typeDesc {
		return &typeDesc{label, key, nil, textEmpty, jsonEmpty}
	}
	withContract := func(label, key string) *typeDesc {
		return &typeDesc{label, key, parseUint8, textContractNumber, jsonContractNumber}
	}
	modulus := func(label, key string) *typeDesc {
		return &typeDesc{label, key, parseUint8, textModulus, jsonModulus}
	}
	contractReq := func(label, key string) *typeDesc {
		return &typeDesc{label, key, parseContractRequest, textContractRequest, jsonContractRequest}
	}
	downlinkTags = map[uint8]*typeDesc{
		3:   withContract("Acknowledgement", "ack"),
		4:   {"Negative acknowledgement", "nack", parseNack, textNack, jsonNack},
		5:   {"Noncompliance notification", "noncomp_notify", parseNoncompNotify, textNoncompNotify, jsonNoncompNotify},
		6:   empty("Cancel emergency mode", "cancel_emergency_mode"),
		7:   basic("Basic report", "basic_report"),
		9:   basic("Emergency basic report", "emerg_basic_report"),
		10:  basic("Lateral deviation change event", "lat_dev_change_event"),
		12:  {"Flight ID data", "flight_id", parseFlightID, textFlightID, jsonFlightID},
		13:  {"Predicted route", "predicted_route", parsePredictedRoute, textPredictedRoute, jsonPredictedRoute},
		14:  {"Earth reference data", "earth_ref_data", parseEarthAirRef, textEarthRef, jsonEarthRef},
		15:  {"Air reference data", "air_ref_data", parseEarthAirRef, textAirRef, jsonAirRef},
		16:  {"Meteo data", "meteo_data", parseMeteo, textMeteo, jsonMeteo},
		17:  {"Airframe ID", "airframe_id", parseAirframeID, textAirframeID, jsonAirframeID},
		18:  basic("Vertical rate change event", "vspd_chg_event"),
		19:  basic("Altitude range event", "alt_range_event"),
		20:  basic("Waypoint change event", "wpt_change_event"),
		22:  {"Intermediate projection", "intermediate_projection", parseIntermediateProjection, textIntermediateProjection, jsonIntermediateProjection},
		23:  {"Fixed projection", "fixed_projection", parseFixedProjection, textFixedProjection, jsonFixedProjection},
		255: {"Reason", "reason", parseUint8, textDisReason, jsonDisReason},
	}
	uplinkTags = map[uint8]*typeDesc{
		1: empty("Cancel all contracts and terminate connection", "cancel_all_contracts"),
		2: withContract("Cancel contract", "cancel_contract"),
		6: withContract("Cancel emergency mode", "cancel_emergency_mode"),
		7: contractReq("Periodic contract request", "periodic_contract_req"),
		8: contractReq("Event contract request", "event_contract_req"),
		9: contractReq("Emergency periodic contract request", "emerg_periodic_contract_req"),
	}
	requestTags = map[uint8]*typeDesc{
		10: {"Report when lateral deviation exceeds", "report_when_lateral_dev_exceeds", parseLatDevChange, textLatDevChange, jsonLatDevChange},
		11: {"Reporting interval", "report_interval", parseReportingInterval, textReportingInterval, jsonReportingInterval},
		12: modulus("Flight ID", "flight_id"),
		13: modulus("Predicted route", "predicted_route"),
		14: modulus("Earth reference data", "earth_ref_data"),
		15: modulus("Air reference data", "air_ref_data"),
		16: modulus("Meteo data", "meteo_data"),
		17: modulus("Airframe ID", "airframe_id"),
		18: {"Report when vertical speed is", "report_when_vspd_is", parseVspdChange, textVspdChange, jsonVspdChange},
		19: {"Report when altitude out of range", "report_when_alt_out_of_range", parseAltRange, textAltRange, jsonAltRange},
		20: empty("Report waypoint changes", "report_wpt_changes"),
		21: {"Aircraft intent data", "acft_intent_data", parseAcftIntentGroup, textAcftIntent, jsonAcftIntent},
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- text ----

func textEmpty(c *ctx, label string, _ any) { c.printf("%s\n", label) }

func textContractNumber(c *ctx, label string, data any) {
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Contract number: %d\n", *data.(*uint8))
	c.indent--
}

func textModulus(c *ctx, label string, data any) {
	c.printf("%s: every %d reports\n", label, *data.(*uint8))
}

var nackReasons = [...]string{
	0:  "(null)",
	1:  "Duplicate group tag",
	2:  "Duplicate reporting interval tag",
	3:  "Event contract request with no data",
	4:  "Improper operational mode tag",
	5:  "Cancel request of a contract which does not exist",
	6:  "Requested contract already exists",
	7:  "Undefined contract request tag",
	8:  "Undefined error",
	9:  "Not enough data in request",
	10: "Invalid altitude range: low limit >= high limit",
	11: "Vertical speed threshold is 0",
	12: "Aircraft intent projection time is 0",
	13: "Lateral deviation threshold is 0",
}

func textNack(c *ctx, label string, data any) {
	n := data.(*nack)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Contract request number: %d\n", n.contractReqNum)
	c.printf("Reason: %d (%s)\n", n.reason, nackReasons[n.reason])
	if n.reason == 1 || n.reason == 2 {
		c.printf("Erroneous octet number: %d\n", n.extData)
	} else if n.reason == 7 {
		c.printf("Erroneous tag number: %d\n", n.extData)
	}
	c.indent--
}

var disReasons = map[int]string{0: "reason not specified", 1: "congestion", 2: "application not available", 8: "normal disconnect"}

func textDisReason(c *ctx, label string, data any) {
	reason := int(*data.(*uint8) >> 4)
	if d, ok := disReasons[reason]; ok {
		c.printf("%s: %s\n", label, d)
	} else {
		c.printf("%s: unknown (%d)\n", label, reason)
	}
}

func textNoncompGroup(c *ctx, g *noncompGroup) {
	c.printf("Tag %d:\n", g.tag)
	c.indent++
	what := "Unavailable parameters: "
	if g.unrecognized {
		what = "Unrecognized group"
	} else if g.wholeGroupUnavl {
		what = "Unavailable group"
	}
	c.printf("%s", what)
	if !g.unrecognized && !g.wholeGroupUnavl && g.paramCnt > 0 {
		for i := 0; i < g.paramCnt; i++ {
			fmt.Fprintf(c.sb, "%d ", g.params[i])
		}
	}
	c.sb.WriteString("\n")
	c.indent--
}

func textNoncompNotify(c *ctx, label string, data any) {
	n := data.(*noncompNotify)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Contract number: %d\n", n.contractReqNum)
	for i := range n.groups {
		textNoncompGroup(c, &n.groups[i])
	}
	c.indent--
}

var accuracyText = [...]string{
	"none (NAV capability lost)", "<30 nm", "<15 nm", "<8 nm", "<4 nm", "<1 nm", "<0.25 nm", "<0.05 nm",
}

func textBasicReport(c *ctx, label string, data any) {
	r := data.(*basicReport)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Lat: %.7f\n", r.lat)
	c.printf("Lon: %.7f\n", r.lon)
	c.printf("Alt: %d ft\n", r.alt)
	min := math.Trunc(r.timestamp / 60.0)
	c.printf("Time: %.3f sec past hour (:%02.0f:%06.3f)\n", r.timestamp, min, r.timestamp-60.0*min)
	c.printf("Position accuracy: %s\n", accuracyText[r.accuracy])
	c.printf("NAV unit redundancy: %s\n", [...]string{"lost", "OK"}[r.redundancy])
	c.printf("TCAS: %s\n", [...]string{"not available to ADS", "OK"}[r.tcas])
	c.indent--
}

func textFlightID(c *ctx, label string, data any) {
	f := data.(*flightID)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Flight ID: %s\n", f.id)
	c.indent--
}

func textPredictedRoute(c *ctx, label string, data any) {
	r := data.(*predictedRoute)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Next waypoint:\n")
	c.indent++
	c.printf("Lat: %.7f\n", r.latNext)
	c.printf("Lon: %.7f\n", r.lonNext)
	c.printf("Alt: %d ft\n", r.altNext)
	c.printf("ETA: %d sec\n", r.etaNext)
	c.indent--
	c.printf("Next+1 waypoint:\n")
	c.indent++
	c.printf("Lat: %.7f\n", r.latNextNext)
	c.printf("Lon: %.7f\n", r.lonNextNext)
	c.printf("Alt: %d ft\n", r.altNextNext)
	c.indent--
	c.indent--
}

func invalid(b bool) string {
	if b {
		return " (invalid)"
	}
	return ""
}

func textEarthRef(c *ctx, label string, data any) {
	r := data.(*earthAirRef)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("True track: %.1f deg%s\n", r.heading, invalid(r.headingInvalid))
	c.printf("Ground speed: %.1f kt\n", r.speed)
	c.printf("Vertical speed: %d ft/min\n", r.vertSpeed)
	c.indent--
}

func textAirRef(c *ctx, label string, data any) {
	r := data.(*earthAirRef)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("True heading: %.1f deg%s\n", r.heading, invalid(r.headingInvalid))
	c.printf("Mach speed: %.4f\n", r.speed/1000.0)
	c.printf("Vertical speed: %d ft/min\n", r.vertSpeed)
	c.indent--
}

func textIntermediateProjection(c *ctx, label string, data any) {
	p := data.(*intermediateProjection)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Distance: %.3f nm\n", p.distance)
	c.printf("True track: %.1f deg%s\n", p.track, invalid(p.trackInvalid))
	c.printf("Alt: %d ft\n", p.alt)
	c.printf("ETA: %d sec\n", p.eta)
	c.indent--
}

func textFixedProjection(c *ctx, label string, data any) {
	p := data.(*fixedProjection)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Lat: %.7f\n", p.lat)
	c.printf("Lon: %.7f\n", p.lon)
	c.printf("Alt: %d ft\n", p.alt)
	c.printf("ETA: %d sec\n", p.eta)
	c.indent--
}

func textMeteo(c *ctx, label string, data any) {
	m := data.(*meteo)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Wind speed: %.1f kt\n", m.windSpeed)
	c.printf("True wind direction: %.1f deg%s\n", m.windDir, invalid(m.windDirInvalid))
	c.printf("Temperature: %.2f C\n", m.temp)
	c.indent--
}

func textAirframeID(c *ctx, label string, data any) {
	a := data.([3]byte)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("ICAO ID: %02X%02X%02X\n", a[0], a[1], a[2])
	c.indent--
}

func textReportingInterval(c *ctx, label string, data any) {
	t := data.(*reportInterval)
	c.printf("%s: %d seconds\n", label, int(t.scalingFactor)*(int(t.rate)+1))
}

func textAcftIntent(c *ctx, label string, data any) {
	t := data.(*acftIntentReq)
	c.printf("%s: every %d reports, projection time: %d minutes\n", label, t.modulus, t.projTime)
}

func textLatDevChange(c *ctx, label string, data any) {
	c.printf("%s: %.3f nm\n", label, *data.(*float64))
}

func textVspdChange(c *ctx, label string, data any) {
	v := *data.(*int)
	sign := '>'
	if v < 0 {
		sign = '<'
		v = -v
	}
	c.printf("%s: %c%d ft/min\n", label, sign, v)
}

func textAltRange(c *ctx, label string, data any) {
	e := data.(*altRange)
	c.printf("%s: %d-%d ft\n", label, e.floor, e.ceiling)
}

func textContractRequest(c *ctx, label string, data any) {
	r := data.(*request)
	c.printf("%s:\n", label)
	c.indent++
	c.printf("Contract number: %d\n", r.contractNum)
	if len(r.tags) == 0 {
		return // libacars returns here without undoing the indent
	}
	for _, t := range r.tags {
		if t.typ == nil {
			c.printf("-- Unparseable tag %d\n", t.id)
			break
		}
		t.typ.text(c, t.typ.label, t.data)
	}
	c.indent--
}

// ---- JSON ----

func jsonEmpty(*ctx, any) {}

func jsonContractNumber(c *ctx, data any) { c.w.Int64("contract_num", int64(*data.(*uint8))) }

func jsonModulus(c *ctx, data any) { c.w.Int64("modulus", int64(*data.(*uint8))) }

func jsonNack(c *ctx, data any) {
	n := data.(*nack)
	c.w.Int64("contract_req_num", int64(n.contractReqNum))
	c.w.Int64("reason", int64(n.reason))
	if n.reason == 1 || n.reason == 2 {
		c.w.Int64("err_octet", int64(n.extData))
	} else if n.reason == 7 {
		c.w.Int64("err_tag", int64(n.extData))
	}
}

func jsonDisReason(c *ctx, data any) { c.w.Int64("reason_code", int64(*data.(*uint8)>>4)) }

func jsonNoncompGroup(c *ctx, g *noncompGroup) {
	c.w.Int64("noncomp_tag", int64(g.tag))
	cause := "params_unavailable"
	if g.unrecognized {
		cause = "group_unrecognized"
	} else if g.wholeGroupUnavl {
		cause = "group_unavailable"
	}
	c.w.Str("noncomp_cause", cause)
	if !g.unrecognized && !g.wholeGroupUnavl {
		c.w.ArrayStart("params")
		for i := 0; i < g.paramCnt; i++ {
			c.w.Int64("", int64(g.params[i]))
		}
		c.w.ArrayEnd()
	}
}

func jsonNoncompNotify(c *ctx, data any) {
	n := data.(*noncompNotify)
	c.w.Int64("contract_req_num", int64(n.contractReqNum))
	c.w.ArrayStart("msg_groups")
	for i := range n.groups {
		c.w.ObjectStart("")
		jsonNoncompGroup(c, &n.groups[i])
		c.w.ObjectEnd()
	}
	c.w.ArrayEnd()
}

// accuracyNM is a float table in libacars, so its values are single precision.
var accuracyNM = [...]float32{-1.0, 30.0, 15.0, 8.0, 4.0, 1.0, 0.25, 0.05}

func jsonBasicReport(c *ctx, data any) {
	r := data.(*basicReport)
	c.w.Double("lat", r.lat)
	c.w.Double("lon", r.lon)
	c.w.Int64("alt", int64(r.alt))
	c.w.Double("ts_sec", r.timestamp)
	c.w.Double("pos_accuracy_nm", float64(accuracyNM[r.accuracy]))
	c.w.Bool("nav_redundancy", r.redundancy == 1)
	c.w.Bool("tcas_avail", r.tcas == 1)
}

func jsonFlightID(c *ctx, data any) { c.w.Str("flight_id", data.(*flightID).id) }

func jsonPredictedRoute(c *ctx, data any) {
	r := data.(*predictedRoute)
	c.w.ObjectStart("next_wpt")
	c.w.Double("lat", r.latNext)
	c.w.Double("lon", r.lonNext)
	c.w.Int64("alt", int64(r.altNext))
	c.w.Int64("eta_sec", int64(r.etaNext))
	c.w.ObjectEnd()
	c.w.ObjectStart("next_next_wpt")
	c.w.Double("lat", r.latNextNext)
	c.w.Double("lon", r.lonNextNext)
	c.w.Int64("alt", int64(r.altNextNext))
	c.w.ObjectEnd()
}

func jsonEarthRef(c *ctx, data any) {
	r := data.(*earthAirRef)
	c.w.Double("true_trk_deg", r.heading)
	c.w.Bool("true_trk_valid", !r.headingInvalid)
	c.w.Double("gnd_spd_kts", r.speed)
	c.w.Int64("vspd_ftmin", int64(r.vertSpeed))
}

func jsonAirRef(c *ctx, data any) {
	r := data.(*earthAirRef)
	c.w.Double("true_hdg_deg", r.heading)
	c.w.Bool("true_hdg_valid", !r.headingInvalid)
	c.w.Double("spd_mach", r.speed/1000.0)
	c.w.Double("vspd_ftmin", float64(r.vertSpeed))
}

func jsonIntermediateProjection(c *ctx, data any) {
	p := data.(*intermediateProjection)
	c.w.Double("dist_nm", p.distance)
	c.w.Double("true_trk_deg", p.track)
	c.w.Bool("true_trk_valid", !p.trackInvalid)
	c.w.Int64("alt", int64(p.alt))
	c.w.Int64("eta_sec", int64(p.eta))
}

func jsonFixedProjection(c *ctx, data any) {
	p := data.(*fixedProjection)
	c.w.Double("lat", p.lat)
	c.w.Double("lon", p.lon)
	c.w.Int64("alt", int64(p.alt))
	c.w.Int64("eta_sec", int64(p.eta))
}

func jsonMeteo(c *ctx, data any) {
	m := data.(*meteo)
	c.w.Double("wind_spd_kts", m.windSpeed)
	c.w.Double("wind_dir_true_deg", m.windDir)
	c.w.Bool("wind_dir_valid", !m.windDirInvalid)
	c.w.Double("temp_c", m.temp)
}

func jsonAirframeID(c *ctx, data any) {
	a := data.([3]byte)
	c.w.Int64("icao_id", int64(a[0])<<16|int64(a[1])<<8|int64(a[2]))
}

func jsonReportingInterval(c *ctx, data any) {
	t := data.(*reportInterval)
	c.w.Int64("interval_secs", int64(int(t.scalingFactor)*(int(t.rate)+1)))
}

func jsonAcftIntent(c *ctx, data any) {
	t := data.(*acftIntentReq)
	c.w.Int64("modulus", int64(t.modulus))
	c.w.Int64("proj_time_mins", int64(t.projTime))
}

func jsonLatDevChange(c *ctx, data any) { c.w.Double("lat_dev_treshold_nm", *data.(*float64)) }

func jsonVspdChange(c *ctx, data any) {
	v := *data.(*int)
	abs := v
	if abs < 0 {
		abs = -abs
	}
	c.w.Int64("vspd_ftmin_threshold", int64(abs))
	c.w.Bool("higher_than", v >= 0)
}

func jsonAltRange(c *ctx, data any) {
	e := data.(*altRange)
	c.w.Int64("floor_alt", int64(e.floor))
	c.w.Int64("ceiling_alt", int64(e.ceiling))
}

func jsonContractRequest(c *ctx, data any) {
	r := data.(*request)
	c.w.Int64("contract_num", int64(r.contractNum))
	if len(r.tags) == 0 {
		return
	}
	c.w.ArrayStart("groups")
	for _, t := range r.tags {
		if t.typ == nil || t.typ.json == nil || t.typ.key == "" {
			break
		}
		c.w.ObjectStart("")
		c.w.ObjectStart(t.typ.key)
		t.typ.json(c, t.data)
		c.w.ObjectEnd()
		c.w.ObjectEnd()
	}
	c.w.ArrayEnd()
}
