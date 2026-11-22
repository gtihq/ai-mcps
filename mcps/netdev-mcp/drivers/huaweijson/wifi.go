package huaweijson

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/device"
)

const wifiConfigPath = "/api/ntwk/WlanGuideBasic?type=notshowpassall"

var (
	metaRE = regexp.MustCompile(`<meta\s+name="([^"]+)"\s+content="([^"]+)"`)
)

// wifiEntry is one band's configuration returned by the router.
type wifiEntry struct {
	ID                string `json:"ID"`
	WifiSsid          string `json:"WifiSsid"`
	FrequencyBand     string `json:"FrequencyBand"`
	WifiSsidEnable    bool   `json:"WifiSsidEnable"`
	BeaconType        string `json:"BeaconType"`
	WpaEncryptionMode string `json:"WpaEncryptionMode"`
	Channel           any    `json:"Channel"`
	WMMEnable         bool   `json:"WMMEnable"`
	WifiHideBroadcast bool   `json:"WifiHideBroadcast"`
}

type wifiConfigResponse struct {
	WifiConfig []wifiEntry `json:"WifiConfig"`
	DbhoEnable bool        `json:"DbhoEnable"`
}

// WiFiNetworks lists the configured SSIDs and their bands.
func (d *Driver) WiFiNetworks(ctx context.Context) ([]device.WiFiNetwork, error) {
	if err := d.ensureAuth(ctx); err != nil {
		return nil, err
	}
	data, err := d.get(ctx, wifiConfigPath)
	if err != nil {
		return nil, err
	}
	var config wifiConfigResponse
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return nil, err
	}
	out := make([]device.WiFiNetwork, 0, len(config.WifiConfig))
	for _, entry := range config.WifiConfig {
		out = append(out, device.WiFiNetwork{
			SSID:    entry.WifiSsid,
			Band:    entry.FrequencyBand,
			Enabled: entry.WifiSsidEnable,
			ID:      entry.ID,
		})
	}
	return out, nil
}

// UpdateWiFi changes the SSID and/or password of one wireless network. The
// owner's own network is protected and can never be modified.
func (d *Driver) UpdateWiFi(ctx context.Context, ssid, newSSID, newPassword string) error {
	for _, protected := range d.protectedSSIDs() {
		if strings.EqualFold(ssid, protected) || strings.EqualFold(newSSID, protected) {
			return fmt.Errorf("refusing to modify protected Wi-Fi network %q", protected)
		}
	}
	if err := d.ensureAuth(ctx); err != nil {
		return err
	}
	if ssid == "" {
		return fmt.Errorf("ssid is required")
	}
	if newSSID == "" {
		newSSID = ssid
	}

	data, err := d.get(ctx, wifiConfigPath)
	if err != nil {
		return err
	}
	var config wifiConfigResponse
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return err
	}
	if len(config.WifiConfig) == 0 {
		return fmt.Errorf("no Wi-Fi configuration returned by the device")
	}

	page, err := d.get(ctx, "/html/index.html")
	if err != nil {
		return err
	}
	meta := map[string]string{}
	for _, match := range metaRE.FindAllStringSubmatch(page, -1) {
		meta[match[1]] = match[2]
	}
	if meta["n"] == "" || meta["e"] == "" {
		return fmt.Errorf("encryption key not found on the device login page")
	}

	buildRow := func(entry wifiEntry, target bool) map[string]any {
		name := entry.WifiSsid
		key := ""
		if target {
			name = newSSID
			if newPassword != "" {
				encrypted, err := encryptRSA(meta["n"], meta["e"], newPassword)
				if err == nil {
					key = encrypted
				}
			}
		}
		advertisement := 1
		if entry.WifiHideBroadcast {
			advertisement = 0
		}
		return map[string]any{
			"ID":                entry.ID,
			"ssid":              name,
			"ssidenable":        entry.WifiSsidEnable,
			"beacontype":        entry.BeaconType,
			"wpaencmode":        entry.WpaEncryptionMode,
			"notsetwebpassword": true,
			"channel":           entry.Channel,
			"wmm":               entry.WMMEnable,
			"advertisement":     advertisement,
			"wpakey":            key,
		}
	}

	first := config.WifiConfig[0]
	second := first
	if len(config.WifiConfig) > 1 {
		second = config.WifiConfig[1]
	}
	payload := map[string]any{
		"config2g":     buildRow(first, strings.EqualFold(first.WifiSsid, ssid)),
		"config5g":     buildRow(second, strings.EqualFold(second.WifiSsid, ssid)),
		"setGuestSsid": false,
		"DbhoEnable":   config.DbhoEnable,
	}
	request := map[string]any{
		"data":   payload,
		"action": "SsidSettings",
		"csrf": map[string]string{
			"csrf_param": meta["csrf_param"],
			"csrf_token": meta["csrf_token"],
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	encrypted, err := encryptRSA(meta["n"], meta["e"], string(body))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/api/ntwk/WlanGuideBasic", strings.NewReader(encrypted))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json;charset=UTF-8;enp;enc")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("_ResponseFormat", "JSON")
	req.Header.Set("accept", "application/json, text/javascript, */*; q=0.01")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wifi update returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// protectedSSIDs returns the SSIDs that must never be changed. The owner's
// network is protected by default; inventories can add more names.
func (d *Driver) protectedSSIDs() []string {
	configured := device.MapString(d.cfg.Extra["protected_ssids"])
	if configured == "" {
		return []string{"Gio Furhouse", "Gio 5Ghz"}
	}
	parts := strings.Split(configured, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}

// encryptRSA mirrors the router web UI: base64-encode the text, RSA-OAEP
// encrypt each chunk with the device public key, then concatenate hex output.
func encryptRSA(nHex, eHex, text string) (string, error) {
	n := new(big.Int)
	if _, ok := n.SetString(nHex, 16); !ok {
		return "", fmt.Errorf("invalid RSA modulus")
	}
	exponent, err := strconv.ParseInt(eHex, 16, 64)
	if err != nil {
		return "", err
	}
	publicKey := &rsa.PublicKey{N: n, E: int(exponent)}
	chunkSize := len(nHex)/2 - 42
	if chunkSize <= 0 {
		return "", fmt.Errorf("invalid RSA key size")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	var out strings.Builder
	for offset := 0; offset < len(encoded); offset += chunkSize {
		end := offset + chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		ciphertext, err := rsa.EncryptOAEP(sha1.New(), rand.Reader, publicKey, []byte(encoded[offset:end]), nil)
		if err != nil {
			return "", err
		}
		out.WriteString(hex.EncodeToString(ciphertext))
	}
	return out.String(), nil
}

// Compile-time assertion.
var _ device.WiFiConfigDriver = (*Driver)(nil)
