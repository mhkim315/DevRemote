const fs = require('fs');
process.stdin.setRawMode(true);
process.stdin.on('data', (d) => {
  console.log("RECEIVED:", d);
  if (d.toString() === 'q') process.exit(0);
});
console.log("WAITING FOR INPUT...");
