const WebSocket = require('ws');
const ws = new WebSocket('ws://localhost:9999/ws');

ws.on('open', function open() {
  console.log("Connected. Sending 'hello' with 100ms delays...");
  const msg = "hello";
  let i = 0;
  const interval = setInterval(() => {
    if (i >= msg.length) {
      clearInterval(interval);
      console.log("Done sending.");
      setTimeout(() => process.exit(0), 2000);
      return;
    }
    ws.send(Buffer.from([msg.charCodeAt(i)]));
    i++;
  }, 100);
});
