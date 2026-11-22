# Logs every request; answers 200 with a 1x1 gif. Port 0 -> prints chosen port.
import sys, http.server, socketserver
log=open(sys.argv[1],'a',buffering=1)
GIF=b'GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;'
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        log.write(f"{self.command} {self.path} UA={self.headers.get('User-Agent')} SFS={self.headers.get('Sec-Fetch-Site')}\n")
        self.send_response(200); self.send_header('Content-Type','image/gif'); self.end_headers(); self.wfile.write(GIF)
    do_POST=do_GET
    def log_message(self,*a): pass
s=socketserver.ThreadingTCPServer(('127.0.0.1',0),H)
print(s.server_address[1],flush=True)
s.serve_forever()
