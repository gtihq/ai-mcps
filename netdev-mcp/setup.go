package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gtihq/ai-mcps/internal/omap"
	"github.com/gtihq/ai-mcps/netdev-mcp/device"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/autodetect"
)

const setupTTL = 60 * time.Minute

// setupServer is a short lived web form that collects device credentials,
// validates them against the real devices and writes the inventory file.
type setupServer struct {
	registry    *Registry
	token       string
	server      *http.Server
	listener    net.Listener
	mu          sync.Mutex
	lastMsg     string
	lastQueued  []device.Config
	lastResults []deviceResult
	lastRemoved *device.Config
	closeOnce   sync.Once
	done        chan struct{}
}

// deviceResult is one compact line on the result page.
type deviceResult struct {
	Name    string
	Vendor  device.Vendor
	OK      bool
	Message string
	Detail  string
}

var (
	activeSetupMu sync.Mutex
	activeSetup   *setupServer
)

// startSetup binds a temporary listener on all interfaces. Binding to
// 0.0.0.0 makes the page reachable through Tailscale as well as localhost.
func startSetup(reg *Registry) (*setupServer, []string, error) {
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, nil, err
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		_ = listener.Close()
		return nil, nil, err
	}
	setup := &setupServer{
		registry: reg,
		token:    hex.EncodeToString(tokenBytes),
		listener: listener,
		done:     make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", setup.handleRoot)
	mux.HandleFunc("/save", setup.handleSave)
	mux.HandleFunc("/done", setup.handleDone)
	mux.HandleFunc("/remove", setup.handleRemove)
	mux.HandleFunc("/undo", setup.handleUndo)
	setup.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		if err := setup.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			setup.shutdown()
		}
	}()
	go func() {
		select {
		case <-time.After(setupTTL):
			setup.shutdown()
		case <-setup.done:
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	return setup, setupURLs(port, setup.token), nil
}

// setupURLs returns the page URL for every local address, including the
// Tailscale interface, plus localhost.
func setupURLs(port int, token string) []string {
	seen := make(map[string]bool)
	urls := make([]string, 0, 4)
	add := func(host string) {
		host = strings.TrimSpace(host)
		if host == "" || seen[host] {
			return
		}
		seen[host] = true
		urls = append(urls, fmt.Sprintf("http://%s:%d/setup?token=%s", host, port, token))
	}
	add("localhost")
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addresses, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, address := range addresses {
				if ipNet, ok := address.(*net.IPNet); ok && ipNet.IP.To4() != nil {
					add(ipNet.IP.String())
				}
			}
		}
	}
	return urls
}

// shutdown closes the listener exactly once and signals waiters.
func (s *setupServer) shutdown() {
	s.closeOnce.Do(func() {
		_ = s.server.Close()
		close(s.done)
	})
}

// validToken checks the one time token from the query string or hidden field.
func (s *setupServer) validToken(r *http.Request) bool {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.FormValue("token")
	}
	return token != "" && token == s.token
}

// handleRoot serves the onboarding form or the result page after saving.
func (s *setupServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if !s.validToken(r) {
		http.Error(w, "invalid or expired token", http.StatusForbidden)
		return
	}
	if r.URL.Path != "/setup" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	queued := append([]device.Config(nil), s.lastQueued...)
	results := append([]deviceResult(nil), s.lastResults...)
	message := s.lastMsg
	s.mu.Unlock()
	inventory, err := loadInventory("")
	if err != nil {
		inventory = nil
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, renderSetupPage(s.token, queued, inventory, results, message, false))
}

// handleSave validates every submitted device concurrently and persists the
// ones that authenticated successfully.
func (s *setupServer) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.validToken(r) {
		http.Error(w, "invalid request or expired token", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	names := r.PostForm["name"]
	vendors := r.PostForm["vendor"]
	hosts := r.PostForm["host"]
	users := r.PostForm["username"]
	passwords := r.PostForm["password"]
	tlsValues := r.PostForm["tls"]
	usedNames := make(map[string]int)

	type submission struct {
		config device.Config
		ok     bool
		skip   bool
		msg    string
		detail string
	}
	items := make([]submission, 0, len(names))
	queued := make([]device.Config, 0, len(names))
	for i := range names {
		name := strings.TrimSpace(names[i])
		host := strings.TrimSpace(hosts[i])
		vendor := device.Vendor(strings.TrimSpace(vendors[i]))
		if name == "" && host == "" {
			continue
		}
		if host == "" {
			items = append(items, submission{skip: true, msg: "Enter an IP address or host."})
			continue
		}
		if name == "" {
			name = string(vendor) + "-" + strings.ReplaceAll(host, ".", "-")
		}
		lower := strings.ToLower(name)
		usedNames[lower]++
		if usedNames[lower] > 1 {
			name = fmt.Sprintf("%s-%d", name, usedNames[lower])
		}
		tls := false
		if i < len(tlsValues) {
			tls = strings.EqualFold(strings.TrimSpace(tlsValues[i]), "on")
		}
		cfg := device.Config{
			Name: name, Vendor: vendor, Host: host,
			Username: strings.TrimSpace(users[i]), Password: passwords[i],
			TLS: tls,
		}
		queued = append(queued, cfg)
		if err := cfg.Validate(); err != nil {
			items = append(items, submission{config: cfg, skip: true, msg: "Check the type and address."})
			continue
		}
		items = append(items, submission{config: cfg})
	}

	testingOnly := r.PostFormValue("test_inventory") == "1"
	if len(items) == 0 && testingOnly {
		configs, err := loadInventory("")
		if err == nil {
			for _, cfg := range configs {
				items = append(items, submission{config: cfg})
			}
		}
	}

	var wg sync.WaitGroup
	for i := range items {
		if items[i].skip || items[i].config.Name == "" {
			continue
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			cfg := items[index].config
			preferred := cfg.Vendor
			if preferred == device.VendorAuto {
				preferred = ""
			}
			driver, err := autodetect.New(cfg, preferred)
			if err != nil {
				explanation := autodetect.Explain(err, cfg.Vendor)
				items[index].msg = explanation.Headline
				items[index].detail = explanation.Detail
				return
			}
			defer func() {
				_ = driver.Close()
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			info, err := driver.(device.SystemDriver).SystemInfo(ctx)
			if err != nil {
				explanation := autodetect.Explain(err, cfg.Vendor)
				items[index].msg = explanation.Headline
				items[index].detail = explanation.Detail
				return
			}
			items[index].ok = true
			items[index].config.Vendor = driver.Vendor()
			items[index].msg = fmt.Sprintf("%s · %s %s", friendlyVendor(driver.Vendor()), info.Hostname, info.Version)
		}(i)
	}
	wg.Wait()

	saved := make([]device.Config, 0, len(items))
	results := make([]deviceResult, 0, len(items))
	connected := 0
	for _, item := range items {
		if item.ok {
			saved = append(saved, item.config)
			connected++
		}
		results = append(results, deviceResult{
			Name: item.config.Name, Vendor: item.config.Vendor,
			OK: item.ok, Message: item.msg, Detail: item.detail,
		})
	}
	message := connectionSummary(connected, len(items))
	if testingOnly {
		message += " No changes were saved."
	} else if len(saved) > 0 {
		editingName := strings.TrimSpace(r.PostFormValue("editing_name"))
		var err error
		if editingName != "" {
			err = replaceInventoryDevice(editingName, saved)
		} else {
			err = saveDevices(saved)
		}
		if err != nil {
			message += " Could not save the inventory: " + err.Error()
		} else {
			configs, loadErr := loadInventory("")
			if loadErr == nil {
				s.registry.Reload(configs)
			}
		}
	}
	s.mu.Lock()
	s.lastQueued = queued
	s.lastResults = results
	s.lastMsg = message
	s.mu.Unlock()
	inventory, err := loadInventory("")
	if err != nil {
		inventory = nil
	}

	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, renderSetupPage(s.token, queued, inventory, results, message, false))
}

// friendlyVendor is the name shown in the result message.
func friendlyVendor(vendor device.Vendor) string {
	switch vendor {
	case device.VendorHuaweiJSON:
		return "Huawei (API JSON)"
	case device.VendorHuawei:
		return "Huawei"
	case device.VendorMikroTik:
		return "MikroTik"
	case device.VendorZTE:
		return "ZTE"
	case device.VendorSSH:
		return "SSH"
	default:
		return string(vendor)
	}
}

// connectionSummary builds the result title with correct pluralization.
func connectionSummary(connected, total int) string {
	switch {
	case total == 0:
		return "No devices submitted."
	case connected == 0:
		return "No devices connected."
	case connected == 1:
		return "1 device connected."
	default:
		return fmt.Sprintf("%d devices connected.", connected)
	}
}

// handleDone shuts the temporary server down after the result page loads.
func (s *setupServer) handleDone(w http.ResponseWriter, r *http.Request) {
	if !s.validToken(r) {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, renderDonePage())
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.shutdown()
	}()
}

// handleRemove deletes one inventory entry and offers an undo action.
func (s *setupServer) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.validToken(r) {
		http.Error(w, "invalid request or expired token", http.StatusForbidden)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	configs, err := loadInventory("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	kept := make([]device.Config, 0, len(configs))
	var removed *device.Config
	for _, cfg := range configs {
		if strings.EqualFold(cfg.Name, name) {
			copyCfg := cfg
			removed = &copyCfg
			continue
		}
		kept = append(kept, cfg)
	}
	message := "Device not found."
	results := []deviceResult{}
	undo := false
	if removed != nil {
		if err := writeInventory(kept); err != nil {
			message = "Could not remove: " + err.Error()
		} else {
			s.mu.Lock()
			s.lastRemoved = removed
			s.mu.Unlock()
			message = name + " removed from the inventory."
			results = append(results, deviceResult{Name: name, OK: true, Message: message})
			undo = true
			if refreshed, loadErr := loadInventory(""); loadErr == nil {
				s.registry.Reload(refreshed)
			}
		}
	}
	s.renderInventoryView(w, message, results, undo)
}

// handleUndo restores the most recently removed device.
func (s *setupServer) handleUndo(w http.ResponseWriter, r *http.Request) {
	if !s.validToken(r) {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	removed := s.lastRemoved
	s.lastRemoved = nil
	s.mu.Unlock()
	message := "Nothing to undo."
	results := []deviceResult{}
	if removed != nil {
		if err := saveDevices([]device.Config{*removed}); err == nil {
			message = removed.Name + " restored."
			results = append(results, deviceResult{Name: removed.Name, OK: true, Message: message})
			if refreshed, loadErr := loadInventory(""); loadErr == nil {
				s.registry.Reload(refreshed)
			}
		} else {
			message = "Could not restore: " + err.Error()
		}
	}
	s.renderInventoryView(w, message, results, false)
}

// renderInventoryView renders the workspace after an inventory-only action.
func (s *setupServer) renderInventoryView(w http.ResponseWriter, message string, results []deviceResult, undo bool) {
	s.mu.Lock()
	queued := append([]device.Config(nil), s.lastQueued...)
	s.mu.Unlock()
	inventory, err := loadInventory("")
	if err != nil {
		inventory = nil
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, renderSetupPage(s.token, queued, inventory, results, message, undo))
}

// saveDevices merges the supplied devices into the existing inventory file.
func saveDevices(incoming []device.Config) error {
	path := resolveInventoryPath("")
	document := struct {
		Devices []device.Config `json:"devices"`
	}{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &document)
	}
	for _, cfg := range incoming {
		replaced := false
		for i, existing := range document.Devices {
			if strings.EqualFold(existing.Name, cfg.Name) {
				document.Devices[i] = cfg
				replaced = true
				break
			}
		}
		if !replaced {
			document.Devices = append(document.Devices, cfg)
		}
	}
	return writeInventory(document.Devices)
}

// replaceInventoryDevice removes the old name before saving the edited entry,
// so renaming a device does not leave a duplicate behind.
func replaceInventoryDevice(oldName string, incoming []device.Config) error {
	path := resolveInventoryPath("")
	document := struct {
		Devices []device.Config `json:"devices"`
	}{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &document)
	}
	kept := make([]device.Config, 0, len(document.Devices))
	for _, cfg := range document.Devices {
		if !strings.EqualFold(cfg.Name, oldName) {
			kept = append(kept, cfg)
		}
	}
	for _, cfg := range incoming {
		replaced := false
		for i, existing := range kept {
			if strings.EqualFold(existing.Name, cfg.Name) {
				kept[i] = cfg
				replaced = true
				break
			}
		}
		if !replaced {
			kept = append(kept, cfg)
		}
	}
	return writeInventory(kept)
}

// writeInventory persists the full device list with owner-only permissions.
func writeInventory(configs []device.Config) error {
	path := resolveInventoryPath("")
	document := struct {
		Devices []device.Config `json:"devices"`
	}{Devices: configs}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// ErrAlreadyAuthenticated tells the tool layer that the inventory already
// has credentials, so the onboarding page must not be opened.
var ErrAlreadyAuthenticated = errors.New("devices are already authenticated in the inventory")

// inventoryAuthenticated reports whether at least one saved device has both
// username and password. The setup screen is only needed when this is false.
func inventoryAuthenticated() bool {
	configs, err := loadInventory("")
	if err != nil || len(configs) == 0 {
		return false
	}
	for _, cfg := range configs {
		if strings.TrimSpace(cfg.Username) != "" && strings.TrimSpace(cfg.Password) != "" {
			return true
		}
	}
	return false
}

// getOrStartSetup returns the active setup server or starts a new one.
func getOrStartSetup(reg *Registry) (*setupServer, []string, error) {
	if inventoryAuthenticated() {
		return nil, nil, ErrAlreadyAuthenticated
	}
	activeSetupMu.Lock()
	defer activeSetupMu.Unlock()
	if activeSetup != nil {
		select {
		case <-activeSetup.done:
			activeSetup = nil
		default:
			return activeSetup, setupURLs(activeSetup.listener.Addr().(*net.TCPAddr).Port, activeSetup.token), nil
		}
	}
	setup, urls, err := startSetup(reg)
	if err != nil {
		return nil, nil, err
	}
	activeSetup = setup
	go func() {
		<-setup.done
		activeSetupMu.Lock()
		if activeSetup == setup {
			activeSetup = nil
		}
		activeSetupMu.Unlock()
	}()
	return setup, urls, nil
}

// setupStatusMap reports the onboarding server state for the MCP tool.
func setupStatusMap() *omap.M {
	activeSetupMu.Lock()
	setup := activeSetup
	activeSetupMu.Unlock()
	if setup == nil {
		return omap.From("running", false, "message", "No setup server is running. Call netdev_setup to open one.")
	}
	setup.mu.Lock()
	message := setup.lastMsg
	setup.mu.Unlock()
	return omap.From("running", true, "message", device.StringOr(message, "Waiting for the form to be filled in."))
}
