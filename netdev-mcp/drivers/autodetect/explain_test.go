package autodetect

import (
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

func TestExplainFocusesPreferredVendor(t *testing.T) {
	err := &DetectError{Reasons: []Reason{
		{Vendor: device.VendorMikroTik, Message: "HTTP 404", Detail: "routeros rest: HTTP 404"},
		{Vendor: device.VendorZTE, Message: "login não encontrado neste firmware", Detail: "zte: login failed: /login.lua resposta de erro"},
	}}
	explanation := Explain(err, device.VendorZTE)
	if !strings.Contains(explanation.Headline, "ZTE") || !strings.Contains(explanation.Headline, "login") {
		t.Fatalf("unexpected headline: %q", explanation.Headline)
	}
	if !strings.Contains(explanation.Detail, "/login.lua") {
		t.Fatalf("detail should carry the raw attempt: %q", explanation.Detail)
	}
	if strings.Contains(explanation.Headline, "MikroTik") {
		t.Fatalf("headline should not list other vendors: %q", explanation.Headline)
	}
}
