// Package imi implements the Digitral protocol shared by bima+ and myIM3.
// They differ only in brand constants (servicekey, channel, app version, host).
package imi

import (
	"strconv"
	"strings"

	"digitral/backend/internal/cryptoutil"
)

const (
	appOS     = "Android"
	secret    = "$"
	defaultUA = ""
)

// Config is the per-brand set of constants from the decompiled app.
type Config struct {
	Base          string
	Brand         string
	Authorization string
	ServiceKey    string
	Channel       string
	AppVersion    string
}

var (
	ConfigBima = Config{
		Base: "https://bimaplus-api.ioh.co.id/api/v2", Brand: "bima+",
		Authorization: "642d1cc69d90666962726e",
		ServiceKey:    "FPi7ZP3Jy8Uv3KBd4QeG",
		Channel:       "BIMA", AppVersion: "5.17.0",
	}
	ConfigMyIM3 = Config{
		Base: "https://myim3api1.ioh.co.id/api/v2", Brand: "myIM3",
		Authorization: "642d1cc69d90666962726e",
		ServiceKey:    "i4WxFMMLvWqnrvuAyg58",
		Channel:       "V2MYIM3", AppVersion: "82.17.0",
	}
)

func configFor(name string) Config {
	if name == "myim3" {
		return ConfigMyIM3
	}
	return ConfigBima
}

// Device is the per-install identity sent on every request.
type Device struct {
	ID, Name, OEM, Model, OSVer, Faaid, Gaid string
}

// DefaultDevice mirrors Python BimaClient's fallback values: server.py never
// passes device_profile into the client, so these are what the app sends.
func DefaultDevice() Device {
	return Device{
		ID: "0123456789abcdef", Name: "generic", OEM: "samsung",
		Model: "SM-A235F", OSVer: "14",
		Faaid: "fa000000000000000000000000000000",
		Gaid:  "00000000-0000-0000-0000-000000000001",
	}
}

// headers mirrors BimaClient.baseline() + build_headers(): parent line, no child.
func headers(cfg Config, dev Device, path, body string, reqTime int64, token string) map[string]string {
	rt := strconv.FormatInt(reqTime, 10)
	h := map[string]string{
		"Authorization":        cfg.Authorization,
		"Content-Type":         "application/json",
		"Accept-Encoding":      "gzip",
		"Connection":           "close",
		"User-Agent":           defaultUA,
		"X-DEVICEID":           dev.ID,
		"X-DEVICENAME":         dev.Name,
		"X-IMI-App-OS":         appOS,
		"X-IMI-App-OSVersion":  dev.OSVer,
		"X-IMI-App-OEM":        dev.OEM,
		"X-IMI-App-Model":      dev.Model,
		"X-IMI-CHANNEL":        cfg.Channel,
		"X-IMI-SERVICEKEY":     cfg.ServiceKey,
		"X-IMI-VERSION":        cfg.AppVersion,
		"X-IMI-LANGUAGE":       "EN",
		"X-LOW-QUOTA":          "NO",
		"X-SPTRAVEL":           "NO",
		"X-FAIID":              dev.Faaid,
		"X-IMI-ADID":           dev.Gaid,
		"X-IMI-ADID-ISLIMITED": "false",
		"X-IMI-UID":            rt,
		"X-IMI-NETWORK":        "WiFi",
	}
	tokenID := token
	if tokenID == "" {
		// putToken: fresh installs put the epoch in TOKENID, never omit it.
		tokenID = rt
	}
	h["X-IMI-TOKENID"] = tokenID
	if strings.Contains(path, "/token/guest/") || strings.Contains(path, "/token/app/") {
		h["X-ESIM-SUPPORT"] = "false"
	}
	h["X-IMI-HASH"] = cryptoutil.SHA512Hex(
		"parent" + secret + appOS + secret + cfg.AppVersion + secret + tokenID +
			"&SALT=" + cryptoutil.GetValues(rt))
	h["x-imi-oauth"] = cryptoutil.SHA512Hex(
		"REQBODY=" + body + "&SALT=" + cryptoutil.GetValues(strings.TrimSpace(tokenID)))
	return h
}
