package webclient

import (
	"testing"
)

func TestXMLMapFlattensNestedTags(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><response><SesInfo>abc</SesInfo><TokInfo>tok123</TokInfo><State>0</State></response>`)
	values := XMLMap(data)
	if values["response.SesInfo"] != "abc" || values["response.TokInfo"] != "tok123" {
		t.Fatalf("unexpected XML map: %v", values)
	}
}

func TestXMLRowsFindsRepeatedElements(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><response><StationInfos>` +
		`<StationInfo><MACAddress>AA:BB</MACAddress><IPAddress>10.0.0.2</IPAddress><SSID>Home</SSID></StationInfo>` +
		`<StationInfo><MACAddress>CC:DD</MACAddress><IPAddress>10.0.0.3</IPAddress><SSID>Guest</SSID></StationInfo>` +
		`</StationInfos></response>`)
	rows := XMLRows(data)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d: %v", len(rows), rows)
	}
	if rows[0]["MACAddress"] != "AA:BB" || rows[1]["SSID"] != "Guest" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

func TestXMLRowsSingleRow(t *testing.T) {
	data := []byte(`<response><HostName>router</HostName><UpTime>1 day</UpTime></response>`)
	rows := XMLRows(data)
	if len(rows) != 1 || rows[0]["HostName"] != "router" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

func TestParseRowsJSONEnvelope(t *testing.T) {
	data := []byte(`{"data":[{"Name":"ether1","Status":"up"},{"Name":"ether2","Status":"down"}]}`)
	rows := ParseRows(data)
	if len(rows) != 2 || rows[1]["Name"] != "ether2" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

func TestParseRowsHTMLTable(t *testing.T) {
	data := []byte(`<html><body><table><tr><th>Port</th><th>Status</th></tr>` +
		`<tr><td>ether1</td><td>up</td></tr><tr><td>ether2</td><td>down</td></tr></table></body></html>`)
	rows := ParseRows(data)
	if len(rows) != 2 || rows[0]["Port"] != "ether1" || rows[1]["Status"] != "down" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

func TestTokenFromBody(t *testing.T) {
	html := `<html><head><meta name="csrf_token" content="tok-abc"/></head></html>`
	if token := TokenFromBody([]byte(html)); token != "tok-abc" {
		t.Fatalf("unexpected token %q", token)
	}
	jsonBody := []byte(`{"TokInfo":"tok-json"}`)
	if token := TokenFromBody(jsonBody); token != "tok-json" {
		t.Fatalf("unexpected token %q", token)
	}
}

func TestXMLRequestEscapesValues(t *testing.T) {
	body := XMLRequest(map[string]string{"Username": "a&b", "Password": "<x>"})
	if body != `<?xml version="1.0" encoding="UTF-8"?><request><Password>&lt;x&gt;</Password><Username>a&amp;b</Username></request>` {
		t.Fatalf("unexpected XML: %s", body)
	}
}
