export const terminalHtml = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no">
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/>
  <script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script>
  <style>
    * { margin: 0; padding: 0; }
    html, body { width: 100%; height: 100%; background: #000; overflow: hidden; }
    #t { width: 100%; height: 100%; }
  </style>
</head>
<body>
<div id="t"></div>
<script>
  // 1. Initialize Terminal
  var term = new Terminal({
    fontSize: 12,
    fontFamily: 'Menlo, Monaco, "Courier New", monospace',
    theme: { background: "#000", foreground: "#ccc" }
  });
  term.open(document.getElementById("t"));
  term.writeln("Waiting for WebRTC connection code...");

  var pc = null;
  var dc = null;
  var signalingURL = "http://10.0.2.2:9171";
  var code = null;
  var lastSeq = 0;
  var pollTimer = null;

  // React Native will inject the connection code by calling connectWebRTC(code)
  window.connectWebRTC = function(connectionCode) {
    code = connectionCode;
    term.writeln("Connecting to daemon (code: " + code + ")...");
    
    // 2. Join Signaling
    fetch(signalingURL + "/join", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code: code, role: "client", key: "mobile-key" })
    }).then(function() {
      // 3. Setup WebRTC PeerConnection
      pc = new RTCPeerConnection({
        iceServers: [{ urls: "stun:stun.l.google.com:19302" }]
      });

      pc.ondatachannel = function(event) {
        dc = event.channel;
        dc.onopen = function() {
          term.writeln("
CONNECTED via WebRTC P2P!
");
          // Re-route terminal input to WebRTC DataChannel
          term.onData(function(d) {
            if (dc.readyState === "open") dc.send(d);
          });
          // Expose to RN for cmd injection
          window.dc = dc; 
        };
        dc.onmessage = function(e) {
          term.write(e.data);
        };
      };

      pc.onicecandidate = function(e) {
        if (!e.candidate) return;
        var c = e.candidate.toJSON();
        fetch(signalingURL + "/send", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            code: code, role: "client",
            msg: { type: "ice", candidate: c.candidate, sdpMid: c.sdpMid, sdpMLineIndex: c.sdpMLineIndex }
          })
        });
      };

      pollTimer = setInterval(pollSignaling, 500);
      
      // AI Spy Polling (Local Emulator Debugging)
      setInterval(function(){
        var s="";
        for(var i=0;i<term.rows;i++){
          var l=term.buffer.active.getLine(i);
          if(l) s+=l.translateToString(true)+"
";
        }
        fetch("http://10.0.2.2:9171/debug/dump", {method:"POST", body:s}).catch(function(){});
        fetch("http://10.0.2.2:9171/debug/cmd")
          .then(function(r){return r.text()})
          .then(function(t){if(t && window.dc && window.dc.readyState==="open") window.dc.send(t+"
")})
          .catch(function(){});
      }, 2000);

    }).catch(function(e) {
      term.writeln("Signaling join failed: " + e.message);
    });
  };

  function pollSignaling() {
    fetch(signalingURL + "/poll?code=" + code + "&role=client&since=" + lastSeq)
      .then(function(res) { return res.json(); })
      .then(function(data) {
        lastSeq = data.since;
        data.messages.forEach(function(m) {
          var msg = JSON.parse(m.msg);
          if (msg.type === "sdp") {
            pc.setRemoteDescription(new RTCSessionDescription({
              type: msg.sdpType === "offer" ? "offer" : "answer",
              sdp: msg.sdp
            })).then(function() {
              if (pc.remoteDescription.type === "offer") {
                pc.createAnswer().then(function(answer) {
                  return pc.setLocalDescription(answer);
                }).then(function() {
                  fetch(signalingURL + "/send", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({
                      code: code, role: "client",
                      msg: { type: "sdp", sdpType: "answer", sdp: pc.localDescription.sdp }
                    })
                  });
                });
              }
            });
          } else if (msg.type === "ice") {
            pc.addIceCandidate(new RTCIceCandidate({
              candidate: msg.candidate, sdpMid: msg.sdpMid, sdpMLineIndex: msg.sdpMLineIndex
            }));
          }
        });
      });
  }

  setTimeout(function() { term.focus() }, 500);
</script>
</body>
</html>`;
