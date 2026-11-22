package huaweijson

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"regexp"
)

var metaValueRE = regexp.MustCompile(`<meta\s+name="([^"]+)"\s+content="([^"]+)"`)

// nonce returns a random hex string like the browser SCRAM client uses.
func nonce() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// pbkdf2 derives keyLength bytes with HMAC using the supplied hash.
func pbkdf2(password, salt []byte, iterations, keyLength int, newHash func() hash.Hash) []byte {
	size := newHash().Size()
	blockCount := (keyLength + size - 1) / size
	out := make([]byte, 0, blockCount*size)
	for block := 1; block <= blockCount; block++ {
		message := append(append([]byte{}, salt...), byte(block>>24), byte(block>>16), byte(block>>8), byte(block))
		mac := hmac.New(newHash, password)
		_, _ = mac.Write(message)
		u := mac.Sum(nil)
		t := append([]byte{}, u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(newHash, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLength]
}

func hmacHash(newHash func() hash.Hash, key, message []byte) []byte {
	mac := hmac.New(newHash, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil)
}

// csrf reads the current CSRF pair from the router login page.
func (d *Driver) csrf(ctx context.Context) (map[string]string, error) {
	page, err := d.get(ctx, "/html/index.html")
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, match := range metaValueRE.FindAllStringSubmatch(page, -1) {
		values[match[1]] = match[2]
	}
	if values["csrf_param"] == "" || values["csrf_token"] == "" {
		return nil, fmt.Errorf("CSRF token not found on the device page")
	}
	return map[string]string{
		"csrf_param": values["csrf_param"],
		"csrf_token": values["csrf_token"],
	}, nil
}

// postJSON sends a JSON body wrapped in the data/csrf envelope the router
// expects and returns the response bytes.
func (d *Driver) postJSON(ctx context.Context, path string, data any) ([]byte, error) {
	csrfValues, err := d.csrf(ctx)
	if err != nil {
		return nil, err
	}
	return d.postJSONWithCSRF(ctx, path, data, csrfValues)
}

// postJSONWithCSRF sends a request with an explicit CSRF pair, needed when
// the previous response rotated the token.
func (d *Driver) postJSONWithCSRF(ctx context.Context, path string, data any, csrfValues map[string]string) ([]byte, error) {
	body, err := json.Marshal(map[string]any{"data": data, "csrf": csrfValues})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d em POST %s: %s", resp.StatusCode, path, string(responseData))
	}
	return responseData, nil
}

// ensureAuth performs the SCRAM login once per driver instance. The router
// accepts both SHA-1 and SHA-256 variants depending on firmware, so both are
// attempted before giving up.
func (d *Driver) ensureAuth(ctx context.Context) error {
	if d.authenticated {
		return nil
	}
	if err := d.simpleLogin(ctx); err == nil {
		d.authenticated = true
		return nil
	}
	for _, newHash := range []func() hash.Hash{sha256.New, sha1.New} {
		if err := d.scramLogin(ctx, newHash); err == nil {
			d.authenticated = true
			return nil
		}
	}
	return fmt.Errorf("Wi-Fi login rejected by the device")
}

// simpleLogin uses the non-SCRAM password hash supported by some firmware.
func (d *Driver) simpleLogin(ctx context.Context) error {
	csrfValues, err := d.csrf(ctx)
	if err != nil {
		return err
	}
	inner := sha256.Sum256([]byte(d.cfg.Password))
	seed := base64.StdEncoding.EncodeToString(inner[:])
	combined := d.cfg.Username + seed + csrfValues["csrf_param"] + csrfValues["csrf_token"]
	outer := sha256.Sum256([]byte(combined))
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"UserName":  d.cfg.Username,
			"Password":  hex.EncodeToString(outer[:]),
			"LoginFlag": 1,
		},
		"csrf": csrfValues,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/api/system/user_login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if !bytes.Contains(data, []byte(`"errorCategory":"ok"`)) && !bytes.Contains(data, []byte(`"errcode":0`)) {
		return fmt.Errorf("simple login rejected: %s", string(data))
	}
	return nil
}

// scramLogin runs one SCRAM exchange with the selected HMAC hash.
func (d *Driver) scramLogin(ctx context.Context, newHash func() hash.Hash) error {
	first, err := nonce()
	if err != nil {
		return err
	}
	challengeData, err := d.postJSON(ctx, "/api/system/user_login_nonce", map[string]string{
		"username":   d.cfg.Username,
		"firstnonce": first,
	})
	if err != nil {
		return err
	}
	var challenge struct {
		Err         int    `json:"err"`
		Salt        string `json:"salt"`
		Iterations  int    `json:"iterations"`
		ServerNonce string `json:"servernonce"`
		CSRFParam   string `json:"csrf_param"`
		CSRFToken   string `json:"csrf_token"`
	}
	if err := json.Unmarshal(challengeData, &challenge); err != nil {
		return err
	}
	if challenge.Err != 0 || challenge.Salt == "" || challenge.ServerNonce == "" {
		return fmt.Errorf("challenge rejected: %s", string(challengeData))
	}
	salt, err := hex.DecodeString(challenge.Salt)
	if err != nil {
		return err
	}
	derived := pbkdf2([]byte(d.cfg.Password), salt, challenge.Iterations, 32, newHash)
	// The Huawei SCRAM helper calls HMAC(message, key), the reverse of the
	// usual HMAC(key, message) order.
	clientKey := hmacHash(newHash, []byte("Client Key"), derived)
	storedHash := newHash()
	_, _ = storedHash.Write(clientKey)
	storedKey := storedHash.Sum(nil)
	authMessage := first + "," + challenge.ServerNonce + "," + challenge.ServerNonce
	clientSignature := hmacHash(newHash, []byte(authMessage), storedKey)
	proof := make([]byte, len(clientKey))
	for i := range clientKey {
		proof[i] = clientKey[i] ^ clientSignature[i]
	}
	proofCSRF := map[string]string{
		"csrf_param": challenge.CSRFParam,
		"csrf_token": challenge.CSRFToken,
	}
	if proofCSRF["csrf_param"] == "" || proofCSRF["csrf_token"] == "" {
		proofCSRF, err = d.csrf(ctx)
		if err != nil {
			return err
		}
	}
	proofData, err := d.postJSONWithCSRF(ctx, "/api/system/user_login_proof", map[string]string{
		"clientproof": hex.EncodeToString(proof),
		"finalnonce":  challenge.ServerNonce,
	}, proofCSRF)
	if err != nil {
		return err
	}
	var result struct {
		Err int `json:"err"`
	}
	if err := json.Unmarshal(proofData, &result); err != nil {
		return fmt.Errorf("proof response: %s", string(proofData))
	}
	if result.Err != 0 {
		return fmt.Errorf("proof rejected: %s", string(proofData))
	}
	return nil
}
