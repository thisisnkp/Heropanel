package httpapi

// Self-contained assets for the /api/docs viewer. They are served as separate
// same-origin files (not inlined) so the strict `default-src 'self'` CSP allows
// them without an exception: an inline <script>/<style> would be blocked, a
// same-origin <script src>/<link> is not. The page renders the spec fetched
// from /api/v1/openapi.json entirely client-side, so it stays in step with the
// live routing tree with no build step.

const docsHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>HeroPanel API reference</title>
<link rel="stylesheet" href="/api/docs.css">
</head>
<body>
<header>
  <h1>HeroPanel API</h1>
  <input id="q" type="search" placeholder="Filter operations…" autocomplete="off" spellcheck="false">
</header>
<main id="app"><p class="muted">Loading the specification…</p></main>
<script src="/api/docs.js"></script>
</body>
</html>
`

const docsCSS = `:root{--bg:#f7f8fa;--panel:#fff;--fg:#1c2430;--muted:#5b6673;--border:#e4e8ee;--brand:#2563eb}
@media (prefers-color-scheme:dark){:root{--bg:#0f141b;--panel:#161d27;--fg:#e6ebf2;--muted:#93a0b0;--border:#26303c;--brand:#5b8cff}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.5 ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,sans-serif}
header{position:sticky;top:0;display:flex;gap:16px;align-items:center;padding:14px 20px;background:var(--panel);border-bottom:1px solid var(--border);z-index:2}
header h1{font-size:16px;margin:0;font-weight:650}
#q{flex:1;max-width:420px;padding:8px 12px;border:1px solid var(--border);border-radius:8px;background:var(--bg);color:var(--fg);font-size:13px}
main{max-width:960px;margin:0 auto;padding:20px}
.muted{color:var(--muted)}
.tag{margin:26px 0 8px;font-size:13px;font-weight:700;text-transform:uppercase;letter-spacing:.04em;color:var(--muted)}
.op{border:1px solid var(--border);border-radius:10px;background:var(--panel);margin:8px 0;overflow:hidden}
.op summary{display:flex;gap:10px;align-items:center;padding:10px 14px;cursor:pointer;list-style:none}
.op summary::-webkit-details-marker{display:none}
.m{font:600 11px/1 ui-monospace,monospace;padding:4px 7px;border-radius:6px;color:#fff;min-width:52px;text-align:center}
.m.get{background:#2563eb}.m.post{background:#16a34a}.m.put{background:#d97706}.m.delete{background:#dc2626}
.path{font:13px/1 ui-monospace,SFMono-Regular,Menlo,monospace}
.sum{color:var(--muted);margin-left:auto;font-size:12.5px;text-align:right}
.body{padding:4px 14px 14px;border-top:1px solid var(--border)}
.body h4{margin:14px 0 4px;font-size:11px;text-transform:uppercase;letter-spacing:.04em;color:var(--muted)}
.perm{display:inline-block;font:600 11px/1 ui-monospace,monospace;color:var(--brand);border:1px solid var(--border);border-radius:6px;padding:3px 6px;margin-left:6px}
pre{margin:0;padding:12px;background:var(--bg);border:1px solid var(--border);border-radius:8px;overflow:auto;font:12px/1.5 ui-monospace,monospace}
table{width:100%;border-collapse:collapse;font-size:12.5px}
td,th{text-align:left;padding:4px 8px;border-bottom:1px solid var(--border);vertical-align:top}
code{font:12px/1 ui-monospace,monospace;background:var(--bg);padding:1px 4px;border-radius:4px}
`

// docsJS renders the spec fetched from /api/v1/openapi.json. Kept dependency-free
// and small; it builds a grouped, collapsible reference with a live filter.
const docsJS = `(function(){
var app=document.getElementById('app'),q=document.getElementById('q'),spec=null;
fetch('/api/v1/openapi.json').then(function(r){return r.json()}).then(function(s){spec=s;render('')}).catch(function(e){app.innerHTML='<p class="muted">Could not load the specification: '+e+'</p>'});
q.addEventListener('input',function(){render(q.value.toLowerCase())});
function esc(s){return String(s).replace(/[&<>]/g,function(c){return{'&':'&amp;','<':'&lt;','>':'&gt;'}[c]})}
function schema(x){return x?JSON.stringify(x,null,2):''}
function ops(){var out=[];var p=spec.paths||{};Object.keys(p).sort().forEach(function(path){var item=p[path];['get','post','put','delete','patch'].forEach(function(m){if(item[m])out.push({method:m,path:path,op:item[m]})})});return out}
function render(filter){
  if(!spec){return}
  var list=ops().filter(function(o){return !filter||(o.path+' '+o.method+' '+(o.op.summary||'')).toLowerCase().indexOf(filter)>=0});
  var byTag={},order=[];
  list.forEach(function(o){var t=(o.op.tags&&o.op.tags[0])||'Other';if(!byTag[t]){byTag[t]=[];order.push(t)}byTag[t].push(o)});
  order.sort();
  var html='<p class="muted">'+(spec.info?esc(spec.info.title)+' — '+esc(spec.info.version):'')+' · '+list.length+' operations</p>';
  order.forEach(function(t){
    html+='<div class="tag">'+esc(t)+'</div>';
    byTag[t].forEach(function(o){
      var perm=o.op['x-required-permission'];
      html+='<details class="op"><summary>'+
        '<span class="m '+o.method+'">'+o.method.toUpperCase()+'</span>'+
        '<span class="path">'+esc(o.path)+'</span>'+
        '<span class="sum">'+esc(o.op.summary||'')+(perm?'<span class="perm">'+esc(perm)+'</span>':'')+'</span>'+
        '</summary><div class="body">'+detail(o.op)+'</div></details>';
    });
  });
  app.innerHTML=html;
}
function detail(op){
  var h='';
  if(op.description)h+='<p class="muted">'+esc(op.description)+'</p>';
  if(op.parameters&&op.parameters.length){
    h+='<h4>Path parameters</h4><table><tr><th>Name</th><th>Description</th></tr>';
    op.parameters.forEach(function(p){h+='<tr><td><code>'+esc(p.name)+'</code></td><td>'+esc(p.description||'')+'</td></tr>'});
    h+='</table>';
  }
  if(op.requestBody){var rb=op.requestBody.content&&op.requestBody.content['application/json'];if(rb)h+='<h4>Request body</h4><pre>'+esc(schema(rb.schema))+'</pre>'}
  if(op.responses){
    h+='<h4>Responses</h4><table><tr><th>Status</th><th>Description</th></tr>';
    Object.keys(op.responses).sort().forEach(function(code){h+='<tr><td><code>'+esc(code)+'</code></td><td>'+esc(op.responses[code].description||'')+'</td></tr>'});
    h+='</table>';
  }
  return h;
}
})();
`
