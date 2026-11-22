package main

import (
	"encoding/json"
	"html/template"
	"strings"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

const pageShell = `<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>netdev-mcp - dispositivos</title>
<style>
:root{
  --bg:#111214;
  --surface:#181a1d;
  --surface-2:#202327;
  --border:#2c3137;
  --text:#e8eaed;
  --muted:#9aa0a6;
  --primary:#2563eb;
  --on-primary:#ffffff;
  --success:#34d399;
  --danger:#f87171;
  --space-1:4px;
  --space-2:8px;
  --space-3:12px;
  --space-4:16px;
  --space-6:24px;
  --radius-control:6px;
  --radius-card:12px;
}
*{box-sizing:border-box}
html{background:var(--bg)}
body{margin:0;min-height:100vh;background:var(--bg);color:var(--text);font-family:system-ui,-apple-system,"Segoe UI",sans-serif;display:grid;place-items:center;padding:var(--space-6) var(--space-4)}
main{width:100%;max-width:980px}
header{display:flex;align-items:flex-start;justify-content:space-between;gap:var(--space-4);margin-bottom:var(--space-6)}
header > div{max-width:560px}
h1{font-size:20px;font-weight:500;margin:0;text-wrap:balance;line-height:1.3}
.sub{color:var(--muted);font-size:13px;line-height:1.5;margin:var(--space-2) 0 0;text-wrap:pretty}
.card{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius-card);padding:var(--space-6)}
@supports (corner-shape:squircle){.card{corner-shape:squircle}}
.card-title{font-size:14px;font-weight:500;margin:0 0 var(--space-4)}
.columns{display:grid;grid-template-columns:1fr 1fr;gap:var(--space-4);align-items:stretch}
@media(max-width:820px){.columns{grid-template-columns:1fr}}
label{display:block;font-size:13px;color:var(--muted);margin:0 0 var(--space-2)}
form{display:grid;row-gap:var(--space-3)}
input,select{width:100%;background:var(--surface-2);border:1px solid var(--border);border-radius:var(--radius-control);color:var(--text);padding:var(--space-3);font:inherit;font-size:14px}
input::placeholder{color:var(--muted)}
input:focus-visible,select:focus-visible{outline:2px solid var(--primary);outline-offset:1px;border-color:var(--primary)}
.check{display:flex;align-items:center;gap:var(--space-2)}
.check input[type=checkbox]{width:16px;height:16px;padding:0;margin:0;flex:none}
.check label{margin:0;color:var(--text)}
.password-row{display:flex;gap:var(--space-2)}
.password-row input{flex:1}
.reveal{flex:none;padding:var(--space-3) var(--space-2)}
.actions{display:flex;gap:var(--space-2);margin-top:var(--space-4)}
.actions button,.actions .button{flex:1}
button,.button{border:1px solid transparent;border-radius:var(--radius-control);padding:var(--space-3) var(--space-4);font:inherit;font-size:14px;cursor:pointer;text-decoration:none;display:inline-flex;align-items:center;justify-content:center;line-height:1.2}
button:hover,.button:hover{opacity:.88}
:focus-visible{outline:2px solid var(--primary);outline-offset:2px}
.primary{background:var(--primary);color:var(--on-primary)}
.secondary{background:var(--surface-2);border-color:var(--border);color:var(--text)}
.quiet{background:transparent;color:var(--muted);padding:var(--space-2);font-size:13px}
.list{list-style:none;margin:0;padding:0;display:grid;gap:var(--space-2)}
.item{display:flex;align-items:center;justify-content:space-between;gap:var(--space-3);background:var(--surface-2);border:1px solid var(--border);border-radius:var(--radius-control);padding:var(--space-3)}
.item .name{font-size:14px}
.item .meta{color:var(--muted);font-size:13px;margin-left:var(--space-2)}
.item .meta .vendor{color:var(--text)}
.remove{background:transparent;color:var(--muted);padding:var(--space-1) var(--space-2);font-size:14px}
.empty{color:var(--muted);font-size:13px;margin:0;padding:var(--space-4);text-align:center;border:1px dashed var(--border);border-radius:var(--radius-control)}
.result{margin-bottom:var(--space-4)}
.status{display:grid;gap:var(--space-2)}
.status-row{display:flex;align-items:flex-start;gap:var(--space-3);background:var(--surface-2);border:1px solid var(--border);border-radius:var(--radius-control);padding:var(--space-3)}
.dot{width:8px;height:8px;border-radius:50%;flex:none;margin-top:5px;background:var(--muted)}
.ok .dot{background:var(--success)}
.fail .dot{background:var(--danger)}
.status-row .name{font-size:14px;padding-left:var(--space-2)}
.status-row .type{color:var(--muted);font-size:12px;margin-left:var(--space-2)}
.status-row .reason{color:var(--muted);font-size:13px;line-height:1.5;margin-top:var(--space-1);text-wrap:pretty;padding-left:var(--space-2)}
.detail{margin-top:var(--space-2)}
.detail summary{cursor:pointer;color:var(--muted);font-size:12px;list-style:none;padding-left:var(--space-2)}
.detail summary::-webkit-details-marker{display:none}
.detail .reason{margin-top:var(--space-2)}
@media(prefers-reduced-motion:reduce){*{transition:none!important}}
</style>
</head>
<body>
<main>
{{CONTENT}}
</main>
</body>
</html>`

const workspaceContent = `<header>
<div>
<h1>Painel de rede</h1>
<p class="sub">Gerencie o inventário e adicione novos roteadores e switches. O servidor expira em 60 minutos.</p>
</div>
<a class="button quiet" href="/done?token={{TOKEN}}">Encerrar</a>
</header>
{{RESULT}}
<div class="columns">
<div class="card">
<h2 class="card-title">Inventário</h2>
{{INVENTORY}}
</div>
<div class="card">
<h2 class="card-title">Adicionar dispositivo</h2>
<form method="post" action="/save?token={{TOKEN}}" onsubmit="return buildPayload()">
<input type="hidden" name="token" value="{{TOKEN}}">
<label for="vendor">Tipo</label>
<select id="vendor">
<option value="auto"{{SEL_AUTO}}>Auto (detectar)</option>
<option value="mikrotik"{{SEL_MIKROTIK}}>MikroTik RouterOS</option>
<option value="zte"{{SEL_ZTE}}>ZTE ZXHN H199A</option>
<option value="huawei"{{SEL_HUAWEI}}>Huawei WiFi BE3</option>
<option value="ssh"{{SEL_SSH}}>Switch SSH</option>
</select>
<label for="f_name">Nome</label><input id="f_name" placeholder="roteador-sala" autocomplete="off">
<label for="f_host">IP ou host</label><input id="f_host" placeholder="192.168.88.1" autocomplete="off">
<label for="f_user">Usuário</label><input id="f_user" value="admin" autocomplete="off">
<label for="f_password">Senha</label>
<div class="password-row">
<input id="f_password" type="password" autocomplete="new-password">
<button type="button" id="reveal" class="secondary reveal" aria-label="Mostrar senha" onclick="togglePassword()">Mostrar</button>
</div>
<div class="check"><input id="f_tls" type="checkbox"><label for="f_tls">Usar TLS</label></div>
<div class="actions"><button type="button" class="secondary" onclick="addDevice()">Adicionar à lista</button></div>
<ul id="list" class="list"></ul>
<p id="empty" class="empty">Nenhum dispositivo na lista.</p>
<div id="payload"></div>
<div class="actions"><button type="submit" class="primary">Testar e salvar</button></div>
</form>
</div>
</div>
<script>
var devices={{DEVICES}};
var selected='{{SELECTED}}';
function render(){
  var list=document.getElementById('list');
  list.innerHTML='';
  document.getElementById('empty').style.display=devices.length?'none':'block';
  devices.forEach(function(d,i){
    var li=document.createElement('li');
    li.className='item';
    var text=document.createElement('span');
    var name=document.createElement('span');
    name.className='name';
    name.textContent=d.name;
    var meta=document.createElement('span');
    meta.className='meta';
    meta.textContent=d.vendor+' · '+d.host;
    text.appendChild(name);
    text.appendChild(meta);
    var edit=document.createElement('button');
    edit.type='button';
    edit.className='remove';
    edit.textContent='Editar';
    edit.onclick=(function(index){return function(){editDevice(index)}})(i);
    var remove=document.createElement('button');
    remove.type='button';
    remove.className='remove';
    remove.textContent='Remover';
    remove.onclick=(function(index){return function(){devices.splice(index,1);render()}})(i);
    li.appendChild(text);
    li.appendChild(edit);
    li.appendChild(remove);
    list.appendChild(li);
  });
}
function editDevice(index){
  var d=devices[index];
  document.getElementById('f_name').value=d.name;
  document.getElementById('f_host').value=d.host;
  document.getElementById('f_user').value=d.username;
  document.getElementById('f_password').value=d.password;
  document.getElementById('f_tls').checked=!!d.tls;
  selected=d.vendor;
  document.getElementById('vendor').value=d.vendor;
  devices.splice(index,1);
  render();
  document.getElementById('f_host').focus();
}
function togglePassword(){
  var input=document.getElementById('f_password');
  var button=document.getElementById('reveal');
  if(input.type==='password'){
    input.type='text';
    button.textContent='Ocultar';
    button.setAttribute('aria-label','Ocultar senha');
  }else{
    input.type='password';
    button.textContent='Mostrar';
    button.setAttribute('aria-label','Mostrar senha');
  }
}
function addDevice(){
  var name=document.getElementById('f_name').value.trim();
  var host=document.getElementById('f_host').value.trim();
  if(host===''){document.getElementById('f_host').focus();return}
  if(name===''){name=selected+'-'+(devices.length+1)}
  devices.push({name:name,vendor:selected,host:host,username:document.getElementById('f_user').value,password:document.getElementById('f_password').value,tls:document.getElementById('f_tls').checked});
  render();
  document.getElementById('f_name').value='';
  document.getElementById('f_host').value='';
  document.getElementById('f_password').value='';
  document.getElementById('f_tls').checked=false;
  document.getElementById('f_name').focus();
}
function buildPayload(){
  if(devices.length===0){document.getElementById('f_host').focus();return false}
  var box=document.getElementById('payload');
  box.innerHTML='';
  devices.forEach(function(d){
    ['name','vendor','host','username','password','tls'].forEach(function(key){
      var input=document.createElement('input');
      input.type='hidden';
      input.name=key;
      input.value=(key==='tls')?(d.tls?'on':''):d[key];
      box.appendChild(input);
    });
  });
  return true;
}
document.getElementById('vendor').addEventListener('change',function(){selected=this.value});
render();
</script>`

const resultPanelContent = `<section class="card result">
<h2 class="card-title">{{SUMMARY}}</h2>
<div class="status">{{ROWS}}</div>
<div class="actions"><a class="button primary" href="/done?token={{TOKEN}}">Concluir</a></div>
</section>`

const doneContent = `<header>
<div>
<h1>Pronto</h1>
<p class="sub">O servidor temporário foi encerrado.</p>
</div>
</header>
<div class="card">
<p class="sub">Você pode voltar a usar o MCP normalmente.</p>
<div class="actions">
<button type="button" class="primary" onclick="closePage()">Fechar página</button>
</div>
</div>
<script>
function closePage(){
  window.close();
  document.body.innerHTML='<p class="sub">Se a aba não fechou, você pode fechar esta janela manualmente.</p>';
}
</script>`

// renderSetupPage builds the full workspace: inventory on the left, add form
// on the right, queued devices preserved and an optional result panel.
func renderSetupPage(token string, queued, inventory []device.Config, results []deviceResult, summary string) string {
	items := make([]map[string]string, 0, len(queued))
	selected := "auto"
	for _, cfg := range queued {
		items = append(items, map[string]string{
			"name": cfg.Name, "vendor": string(cfg.Vendor), "host": cfg.Host,
			"username": cfg.Username, "password": cfg.Password, "tls": boolText(cfg.TLS),
		})
	}
	if len(queued) > 0 {
		selected = string(queued[len(queued)-1].Vendor)
	}
	data, _ := json.Marshal(items)
	devicesJSON := strings.ReplaceAll(string(data), "<", `\u003c`)

	var rows strings.Builder
	for _, cfg := range inventory {
		rows.WriteString(`<li class="item"><span><span class="name">`)
		rows.WriteString(template.HTMLEscapeString(cfg.Name))
		rows.WriteString(`</span><span class="meta"><span class="vendor">`)
		rows.WriteString(template.HTMLEscapeString(vendorName(cfg.Vendor)))
		rows.WriteString(`</span> · `)
		rows.WriteString(template.HTMLEscapeString(cfg.Host))
		rows.WriteString(`</span></span><span class="dot" style="background:var(--success)"></span></li>`)
	}
	inventoryHTML := `<p class="empty">Nenhum dispositivo salvo ainda.</p>`
	if rows.Len() > 0 {
		inventoryHTML = `<ul class="list">` + rows.String() + `</ul>`
	}

	resultHTML := ""
	if len(results) > 0 {
		resultHTML = replaceAll(resultPanelContent, map[string]string{
			"{{SUMMARY}}": summary,
			"{{ROWS}}":    resultRows(results),
			"{{TOKEN}}":   template.HTMLEscapeString(token),
		})
	}

	selects := map[string]string{
		"{{SEL_AUTO}}": "", "{{SEL_MIKROTIK}}": "", "{{SEL_ZTE}}": "",
		"{{SEL_HUAWEI}}": "", "{{SEL_SSH}}": "",
	}
	switch selected {
	case "mikrotik":
		selects["{{SEL_MIKROTIK}}"] = " selected"
	case "zte":
		selects["{{SEL_ZTE}}"] = " selected"
	case "huawei", "huawei_json":
		selects["{{SEL_HUAWEI}}"] = " selected"
	case "ssh":
		selects["{{SEL_SSH}}"] = " selected"
	default:
		selects["{{SEL_AUTO}}"] = " selected"
	}
	replacements := map[string]string{
		"{{TOKEN}}":     template.HTMLEscapeString(token),
		"{{DEVICES}}":   devicesJSON,
		"{{SELECTED}}":  template.JSEscapeString(selected),
		"{{INVENTORY}}": inventoryHTML,
		"{{RESULT}}":    resultHTML,
	}
	for key, value := range selects {
		replacements[key] = value
	}
	content := replaceAll(workspaceContent, replacements)
	return strings.Replace(pageShell, "{{CONTENT}}", content, 1)
}

// resultRows renders one compact status row per submitted device.
func resultRows(results []deviceResult) string {
	var b strings.Builder
	for _, result := range results {
		class := "status-row fail"
		if result.OK {
			class = "status-row ok"
		}
		b.WriteString(`<div class="` + class + `"><span class="dot"></span><div>`)
		b.WriteString(`<div class="name">` + template.HTMLEscapeString(result.Name))
		if result.Vendor != "" {
			b.WriteString(`<span class="type">` + template.HTMLEscapeString(vendorName(result.Vendor)) + `</span>`)
		}
		b.WriteString(`</div>`)
		b.WriteString(`<div class="reason">` + template.HTMLEscapeString(result.Message) + `</div>`)
		if result.Detail != "" {
			b.WriteString(`<details class="detail"><summary>Detalhes</summary>`)
			b.WriteString(`<div class="reason">` + template.HTMLEscapeString(result.Detail) + `</div></details>`)
		}
		b.WriteString(`</div></div>`)
	}
	return b.String()
}

// renderDonePage returns the closing screen with a button that closes the tab.
func renderDonePage() string {
	return strings.Replace(pageShell, "{{CONTENT}}", doneContent, 1)
}

// replaceAll applies every placeholder replacement in one pass.
func replaceAll(text string, replacements map[string]string) string {
	for key, value := range replacements {
		text = strings.ReplaceAll(text, key, value)
	}
	return text
}

// boolText converts a boolean to the string used by the form payload.
func boolText(value bool) string {
	if value {
		return "on"
	}
	return ""
}

// vendorName maps vendor ids to the names used in the form.
func vendorName(vendor device.Vendor) string {
	switch vendor {
	case device.VendorMikroTik:
		return "MikroTik"
	case device.VendorZTE:
		return "ZTE"
	case device.VendorHuawei, device.VendorHuaweiJSON:
		return "Huawei"
	case device.VendorSSH:
		return "SSH"
	default:
		return "Auto"
	}
}
