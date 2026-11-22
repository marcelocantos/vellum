# usage: mcpcall.py <port> <json args file>  -> prints tool result text
import sys, json, urllib.request
port=sys.argv[1]; args=json.load(open(sys.argv[2]))
url=f"http://127.0.0.1:{port}/mcp"
H={"Content-Type":"application/json","Accept":"application/json, text/event-stream"}
def post(body, sid=None):
    h=dict(H)
    if sid: h["Mcp-Session-Id"]=sid
    req=urllib.request.Request(url,data=json.dumps(body).encode(),headers=h,method="POST")
    r=urllib.request.urlopen(req,timeout=300)
    raw=r.read().decode()
    sid=r.headers.get("Mcp-Session-Id") or sid
    msgs=[]
    for line in raw.splitlines():
        if line.startswith("data:"): msgs.append(json.loads(line[5:]))
    if not msgs and raw.strip(): 
        try: msgs=[json.loads(raw)]
        except Exception: pass
    return sid,msgs
sid,_=post({"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}})
post({"jsonrpc":"2.0","method":"notifications/initialized"},sid)
_,m=post({"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"convert","arguments":args}},sid)
for x in m:
    res=x.get("result",x)
    for c in res.get("content",[]): print(c.get("text"))
    if "structuredContent" in res: print(json.dumps(res["structuredContent"])[:3000])
