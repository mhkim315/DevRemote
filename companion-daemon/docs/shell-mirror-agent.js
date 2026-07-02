const path = require('path');
require('dotenv').config({ path: path.resolve(__dirname, '../.env') });

const WebSocket = require('ws');
const pty = require('node-pty');
const os = require('os');
const { v4: uuidv4 } = require('uuid');
const fs = require('fs');
const https = require('https');
const { exec } = require('child_process');

// Enhanced logging to file
const LOG_FILE = path.join(__dirname, 'agent-debug.log');

function logToFile(message) {
  const timestamp = new Date().toISOString();
  const logEntry = `[${timestamp}] ${message}\n`;
  console.log(logEntry.trim()); // Also log to console
  fs.appendFileSync(LOG_FILE, logEntry);
}

// Clear previous log
fs.writeFileSync(LOG_FILE, `=== Mac Agent Debug Log Started ${new Date().toISOString()} ===\n`);

// Use @koush/wrtc package for Node.js WebRTC support
let wrtc;
try {
  wrtc = require('@koush/wrtc');
  logToFile('✅ @koush/wrtc package loaded successfully');
} catch (err) {
  logToFile('❌ Failed to load @koush/wrtc package. Install with: npm install @koush/wrtc');
  process.exit(1);
}

// Generate stable agent ID from machine identity (prevents duplicate registrations)
function generateStableAgentId() {
    const hostname = os.hostname();
    const username = os.userInfo().username;
    return `mac-${username}-${hostname}`.replace(/[^a-zA-Z0-9-]/g, '-');
}

const AGENT_ID = process.env.AGENT_ID || generateStableAgentId();
logToFile(`🆔 Agent ID: ${AGENT_ID}`);

// Cloud signaling is OFF by default. The public relay does not authenticate
// callers, so exposing the agent on it lets anyone who knows/guesses the agent id
// open a shell. Only dial a remote wss:// relay when explicitly enabled, and
// ideally pair it with CONNECT_SECRET (checked in the client-hello handler).
const ENABLE_CLOUD_WEBRTC = process.env.ENABLE_CLOUD_WEBRTC === 'true';
const CONNECT_SECRET = process.env.CONNECT_SECRET || '';

// Determine signaling server URL - prefer WEBSOCKET_URL for cloud connections
let SIGNALING_SERVER_URL;
if (process.env.WEBSOCKET_URL) {
    SIGNALING_SERVER_URL = process.env.WEBSOCKET_URL;
    logToFile(`🌐 Using cloud WebSocket URL: ${SIGNALING_SERVER_URL}`);
} else {
    // Fallback to local server configuration
    let connectHost = process.env.HOST || 'localhost';
    if (connectHost === '0.0.0.0') {
        logToFile('[AGENT] Host is 0.0.0.0, connecting to localhost instead.');
        connectHost = 'localhost';
    }
    const PORT = process.env.PORT || 3000;
    SIGNALING_SERVER_URL = `ws://${connectHost}:${PORT}`;
    logToFile(`🌐 Using local WebSocket URL: ${SIGNALING_SERVER_URL}`);
}

const shell = os.platform() === 'win32' ? 'powershell.exe' : 'bash';
logToFile(`🐚 Shell: ${shell}`);

// Enable case-insensitive tab completion for shells
function enableCaseInsensitiveCompletion(terminal, shellType) {
  setTimeout(() => {
    if (shellType === '/bin/zsh' || shellType.includes('zsh')) {
      // For zsh: configure case-insensitive completion matcher
      terminal.write('autoload -Uz compinit 2>/dev/null; compinit -i 2>/dev/null; zstyle \':completion:*\' matcher-list \'m:{a-zA-Z}={A-Za-z}\'; clear\n');
    } else if (shellType.includes('bash')) {
      // For bash: set completion-ignore-case
      terminal.write('bind \'set completion-ignore-case on\' 2>/dev/null; clear\n');
    }
  }, 600); // After login scripts have run
}

// Circular buffer for session output persistence
class CircularBuffer {
  constructor(size = 10000, maxTotalSize = 512 * 1024) { // 512KB max total size
    this.size = size;
    this.maxTotalSize = maxTotalSize;
    this.buffer = [];
    this.index = 0;
    this.full = false;
    this.totalSize = 0;
  }

  add(data) {
    const dataSize = Buffer.byteLength(data, 'utf8');
    
    // Add new data
    const oldData = this.buffer[this.index];
    if (oldData) {
      this.totalSize -= Buffer.byteLength(oldData, 'utf8');
    }
    
    this.buffer[this.index] = data;
    this.totalSize += dataSize;
    this.index = (this.index + 1) % this.size;
    if (this.index === 0) this.full = true;
    
    // If total size exceeds limit, remove older data
    this.enforceMaxSize();
  }
  
  enforceMaxSize() {
    if (this.totalSize <= this.maxTotalSize) return;
    
    // Remove data from the oldest end until under limit
    let removed = 0;
    while (this.totalSize > this.maxTotalSize && this.getTotalItems() > 0) {
      let oldestIndex;
      if (this.full) {
        oldestIndex = this.index; // Oldest item when buffer is full
      } else {
        oldestIndex = 0; // Start from beginning when not full
      }
      
      const oldestData = this.buffer[oldestIndex];
      if (oldestData) {
        this.totalSize -= Buffer.byteLength(oldestData, 'utf8');
        this.buffer[oldestIndex] = '';
        removed++;
        
        if (this.full) {
          this.index = (this.index + 1) % this.size;
          if (this.getTotalItems() === 0) {
            this.full = false;
            this.index = 0;
          }
        } else {
          // Shift array to remove empty spot
          this.buffer.splice(oldestIndex, 1);
          this.buffer.push('');
          this.index = Math.max(0, this.index - 1);
        }
      } else {
        break; // Prevent infinite loop
      }
    }
    
    if (removed > 0) {
      logToFile(`[BUFFER] Enforced max size limit: removed ${removed} old entries, total size now ${this.totalSize} bytes`);
    }
  }
  
  getTotalItems() {
    if (!this.full) {
      return this.buffer.slice(0, this.index).filter(item => item).length;
    }
    return this.buffer.filter(item => item).length;
  }

  getAll() {
    if (!this.full) {
      return this.buffer.slice(0, this.index).join('');
    }
    return this.buffer.slice(this.index).concat(this.buffer.slice(0, this.index)).join('');
  }

  clear() {
    this.buffer = [];
    this.index = 0;
    this.full = false;
    this.totalSize = 0;
  }
  
  getStats() {
    return {
      items: this.getTotalItems(),
      totalSize: this.totalSize,
      maxSize: this.maxTotalSize,
      utilizationPercent: Math.round((this.totalSize / this.maxTotalSize) * 100)
    };
  }
}

// Session Manager for multiple persistent terminal sessions
class SessionManager {
  constructor() {
    this.sessions = {};
    this.maxSessions = 10;
    this.defaultSessionTimeout = 24 * 60 * 60 * 1000; // 24 hours
    this.clientSessions = {}; // Maps clientId to sessionId
    this.sessionCounter = 0; // Incrementing counter for unique session names (never resets)
  }

  createSession(sessionName = null, clientId = null) {
    const sessionId = `ses_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
    // Use incrementing counter for unique names (doesn't reuse after deletion)
    this.sessionCounter++;
    const name = sessionName || `Session ${this.sessionCounter}`;

    logToFile(`[SESSION] Creating new session: ${sessionId} (${name})`);

    // Check session limit
    if (Object.keys(this.sessions).length >= this.maxSessions) {
      logToFile(`[SESSION] ❌ Maximum sessions (${this.maxSessions}) reached`);
      return null;
    }

    const macShell = os.platform() === 'darwin' ? '/bin/zsh' : shell;
    const terminalEnv = {
      ...process.env,
      TERM: 'xterm-256color',
      COLORTERM: 'truecolor',
      LANG: 'en_US.UTF-8',
      LC_ALL: 'en_US.UTF-8',
      SHELL: macShell,
      TERM_PROGRAM: 'Terminal',
      TERM_PROGRAM_VERSION: '2.12.7'
    };

    const terminal = pty.spawn(macShell, ['--login'], {
      name: 'xterm-256color',
      cols: 120,
      rows: 30,
      cwd: process.env.HOME,
      env: terminalEnv,
      encoding: 'utf8'
    });

    const session = {
      id: sessionId,
      name: name,
      terminal: terminal,
      buffer: new CircularBuffer(10000),
      connectedClients: [],
      createdAt: Date.now(),
      lastActivity: Date.now(),
      status: 'active'
    };

    // Set up terminal event handlers
    terminal.on('data', (data) => {
      session.buffer.add(data);
      session.lastActivity = Date.now();
      
      // Send to all connected clients for this session
      session.connectedClients.forEach(clientId => {
        this.sendToClient(clientId, { type: 'output', data: data });
      });
    });

    // Send initial prompt after terminal is ready
    setTimeout(() => {
      // Send a newline to trigger the shell prompt
      terminal.write('\n');
    }, 500);

    // Enable case-insensitive tab completion
    enableCaseInsensitiveCompletion(terminal, macShell);

    terminal.on('exit', (code) => {
      logToFile(`[SESSION] Terminal process exited for session ${sessionId} with code ${code}`);
      session.status = 'crashed';
      // Notify connected clients
      session.connectedClients.forEach(clientId => {
        this.sendToClient(clientId, { 
          type: 'session-ended', 
          sessionId: sessionId,
          reason: 'terminal-exit',
          code: code 
        });
      });
    });

    this.sessions[sessionId] = session;
    
    // Associate with client if provided
    if (clientId) {
      this.clientSessions[clientId] = sessionId;
      session.connectedClients.push(clientId);
    }

    logToFile(`[SESSION] ✅ Session created: ${sessionId} (PID: ${terminal.pid})`);
    return sessionId;
  }

  getSession(sessionId) {
    return this.sessions[sessionId] || null;
  }

  connectClientToSession(clientId, sessionId) {
    const session = this.sessions[sessionId];
    if (!session) {
      logToFile(`[SESSION] ❌ Cannot connect client ${clientId} - session ${sessionId} not found`);
      return false;
    }

    // Disconnect client from any existing session
    this.disconnectClient(clientId);

    // Connect to new session
    this.clientSessions[clientId] = sessionId;
    if (!session.connectedClients.includes(clientId)) {
      session.connectedClients.push(clientId);
    }
    session.lastActivity = Date.now();

    logToFile(`[SESSION] ✅ Client ${clientId} connected to session ${sessionId}`);
    logToFile(`[SESSION] ℹ️ Buffered output will be sent when WebRTC data channel opens`);

    return true;
  }

  disconnectClient(clientId) {
    const sessionId = this.clientSessions[clientId];
    if (sessionId && this.sessions[sessionId]) {
      const session = this.sessions[sessionId];
      session.connectedClients = session.connectedClients.filter(id => id !== clientId);
      logToFile(`[SESSION] Client ${clientId} disconnected from session ${sessionId}`);
    }
    delete this.clientSessions[clientId];
  }

  getClientSession(clientId) {
    const sessionId = this.clientSessions[clientId];
    return sessionId ? this.sessions[sessionId] : null;
  }

  getAllSessions() {
    return Object.values(this.sessions).map(session => ({
      id: session.id,
      name: session.name,
      lastActivity: session.lastActivity,
      createdAt: session.createdAt,
      status: session.status,
      connectedClients: session.connectedClients.length
    }));
  }

  terminateSession(sessionId) {
    const session = this.sessions[sessionId];
    if (!session) return false;

    logToFile(`[SESSION] Terminating session: ${sessionId}`);

    // Notify connected clients
    session.connectedClients.forEach(clientId => {
      this.sendToClient(clientId, { 
        type: 'session-terminated', 
        sessionId: sessionId 
      });
      delete this.clientSessions[clientId];
    });

    // Kill terminal process
    if (session.terminal) {
      session.terminal.kill();
    }

    delete this.sessions[sessionId];
    logToFile(`[SESSION] ✅ Session terminated: ${sessionId}`);
    return true;
  }

  sendToClient(clientId, message) {
    // This will be connected to the WebRTC data channel sending logic
    // For now, we'll use a global dataChannel reference
    // In a full implementation, this would use a clientId-to-dataChannel mapping
    if (typeof dataChannel !== 'undefined' && dataChannel && dataChannel.readyState === 'open') {
      const success = sendLargeMessage(dataChannel, message, '[SESSION]');
      if (!success) {
        logToFile(`[SESSION] ❌ Failed to send message to client ${clientId}`);
      }
    } else {
      logToFile(`[SESSION] ⚠️ Cannot send to client ${clientId} - data channel not available`);
    }
  }

  writeToSession(sessionId, data) {
    const session = this.sessions[sessionId];
    if (session && session.terminal) {
      session.terminal.write(data);
      session.lastActivity = Date.now();
      return true;
    }
    return false;
  }

  resizeSession(sessionId, cols, rows) {
    const session = this.sessions[sessionId];
    if (session && session.terminal) {
      session.terminal.resize(cols, rows);
      session.lastActivity = Date.now();
      return true;
    }
    return false;
  }

  cleanupIdleSessions() {
    const now = Date.now();
    Object.keys(this.sessions).forEach(sessionId => {
      const session = this.sessions[sessionId];
      const idleTime = now - session.lastActivity;
      
      if (idleTime > this.defaultSessionTimeout && session.connectedClients.length === 0) {
        logToFile(`[SESSION] Auto-cleanup idle session: ${sessionId} (idle for ${Math.floor(idleTime / 60000)} minutes)`);
        this.terminateSession(sessionId);
      }
    });
  }
}

// Initialize session manager
const sessionManager = new SessionManager();

// Cleanup idle sessions every 30 minutes
setInterval(() => {
  sessionManager.cleanupIdleSessions();
}, 30 * 60 * 1000);

let ws;
let peerConnection;
let dataChannel;

const iceServers = [
  // Google STUN servers (primary)
  { urls: 'stun:stun.l.google.com:19302' },
  { urls: 'stun:stun1.l.google.com:19302' },
  // Cloudflare STUN servers (backup)
  { urls: 'stun:stun.cloudflare.com:3478' },
  // Mozilla STUN servers (backup)  
  { urls: 'stun:stun.services.mozilla.com:3478' },
  // OpenRelay free TURN server (for NAT traversal)
  {
    urls: 'turn:openrelay.metered.ca:80',
    username: 'openrelayproject',
    credential: 'openrelayproject'
  },
  // Alternative TURN server
  {
    urls: 'turn:openrelay.metered.ca:443',
    username: 'openrelayproject',
    credential: 'openrelayproject'
  }
];

// --- Heartbeat System ---
let heartbeatInterval;

async function sendHeartbeat() {
  try {
    // Get full session list for dashboard display
    const sessionList = sessionManager.getAllSessions().map(session => ({
      id: session.id,
      name: session.name,
      lastActivity: session.lastActivity,
      createdAt: session.createdAt,
      status: session.status
    }));

    const heartbeatData = JSON.stringify({
      agentId: AGENT_ID,
      timestamp: Date.now(),
      activeSessions: sessionList.length,
      sessions: sessionList, // Full session list for dashboard
      localPort: process.env.LOCAL_PORT || 8080,
      capabilities: ['webrtc', 'direct_websocket']
    });

    const options = {
      hostname: 'shellmirror.app',
      port: 443,
      path: '/php-backend/api/agent-heartbeat.php',
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(heartbeatData),
        'X-Agent-Secret': 'mac-agent-secret-2024',
        'X-Agent-ID': AGENT_ID
      }
    };

    const req = https.request(options, (res) => {
      let responseData = '';
      res.on('data', (chunk) => {
        responseData += chunk;
      });
      res.on('end', () => {
        if (res.statusCode === 200) {
          try {
            const result = JSON.parse(responseData);
            if (result.success) {
              logToFile(`💓 Heartbeat sent successfully`);
            } else {
              logToFile(`⚠️ Heartbeat failed: ${result.message}`);
            }
          } catch (error) {
            logToFile(`⚠️ Heartbeat response parse error: ${error.message}`);
          }
        } else {
          logToFile(`⚠️ Heartbeat HTTP error: ${res.statusCode}`);
        }
      });
    });

    req.on('error', (error) => {
      logToFile(`❌ Heartbeat request failed: ${error.message}`);
    });

    req.write(heartbeatData);
    req.end();

  } catch (error) {
    logToFile(`❌ Heartbeat error: ${error.message}`);
  }
}

function startHeartbeatSystem() {
  logToFile('💓 Starting heartbeat system (60 second interval)');
  
  // Send initial heartbeat immediately
  sendHeartbeat();
  
  // Set up recurring heartbeat
  heartbeatInterval = setInterval(sendHeartbeat, 60000); // 60 seconds
}

function stopHeartbeatSystem() {
  if (heartbeatInterval) {
    clearInterval(heartbeatInterval);
    heartbeatInterval = null;
    logToFile('💓 Heartbeat system stopped');
  }
}

// Open the web dashboard in the default browser (best-effort).
function openDashboardInBrowser() {
  const dashboardUrl = 'https://shellmirror.app/app/dashboard.html';
  logToFile(`🌐 Opening dashboard in browser: ${dashboardUrl}`);
  exec(`open "${dashboardUrl}"`, (error) => {
    if (error) {
      logToFile(`⚠️ Could not open browser automatically: ${error.message}`);
      logToFile(`💡 Please open ${dashboardUrl} manually in your browser`);
    } else {
      logToFile('✅ Dashboard opened in browser');
    }
  });
}

function connectToSignalingServer() {
  // Refuse to expose this agent on a remote (wss://) relay unless cloud signaling
  // is explicitly enabled. Local/LAN signaling (ws://) is still allowed for dev.
  const isRemoteRelay = /^wss:\/\//i.test(SIGNALING_SERVER_URL);
  if (isRemoteRelay && !ENABLE_CLOUD_WEBRTC) {
    logToFile(`⛔ Cloud WebRTC disabled (ENABLE_CLOUD_WEBRTC!=true): not connecting to remote relay ${SIGNALING_SERVER_URL}.`);
    logToFile(`   Use LAN-direct, the HTTP agent, or set ENABLE_CLOUD_WEBRTC=true + CONNECT_SECRET to re-enable cloud access.`);
    return;
  }
  logToFile(`🔌 Connecting to signaling server at ${SIGNALING_SERVER_URL}?role=agent&agentId=${AGENT_ID}`);
  ws = new WebSocket(`${SIGNALING_SERVER_URL}?role=agent&agentId=${AGENT_ID}`);

  ws.on('open', () => {
    logToFile('✅ Connected to signaling server (cloud relay).');
    // Heartbeat + dashboard are started at agent startup (independent of the relay)
    // so PHP registration and the LAN-direct path work even with cloud signaling off.
  });

  ws.on('message', async (message) => {
    try {
      const data = JSON.parse(message);
      logToFile(`📨 Received message of type: ${data.type} from: ${data.from} to: ${data.to}`);

      switch (data.type) {
        case 'client-hello':
          logToFile(`🔄 Received client-hello from ${data.from}. Processing session request.`);
          // Defense-in-depth: if a connect secret is configured, the caller must
          // present it before we ever spawn a shell. Stops anyone who can reach the
          // relay from opening a session just by knowing the agent id.
          if (CONNECT_SECRET && data.connectSecret !== CONNECT_SECRET) {
            logToFile(`⛔ Rejected client-hello from ${data.from}: missing/invalid connect secret`);
            sendMessage({ type: 'error', message: 'Not authorized', to: data.from, from: AGENT_ID });
            break;
          }
          try {
            let sessionId;
            let isNewSession = false;

            // Handle session request from client
            if (data.sessionRequest) {
              if (data.sessionRequest.sessionId) {
                // Connect to existing session
                sessionId = data.sessionRequest.sessionId;
                logToFile(`[SESSION] Client requesting existing session: ${sessionId}`);
                if (!sessionManager.getSession(sessionId)) {
                  logToFile(`[SESSION] ⚠️ Requested session ${sessionId} not found, creating new session`);
                  sessionId = sessionManager.createSession(data.sessionRequest.sessionName, data.from);
                  isNewSession = true;
                }
              } else if (data.sessionRequest.newSession) {
                // Create new session
                sessionId = sessionManager.createSession(data.sessionRequest.sessionName, data.from);
                isNewSession = true;
                logToFile(`[SESSION] Client requesting new session: ${sessionId}`);
              } else {
                // Default: create new session if no specific request
                sessionId = sessionManager.createSession(null, data.from);
                isNewSession = true;
              }
            } else {
              // Backward compatibility: no session request means create default session
              sessionId = sessionManager.createSession(null, data.from);
              isNewSession = true;
            }

            if (!sessionId) {
              logToFile(`[SESSION] ❌ Failed to create/connect to session`);
              sendMessage({ 
                type: 'error', 
                message: 'Failed to create session - maximum sessions reached', 
                to: data.from, 
                from: AGENT_ID 
              });
              break;
            }

            // Connect client to session
            sessionManager.connectClientToSession(data.from, sessionId);

            await createPeerConnection(data.from);
            logToFile('📡 PeerConnection created, generating offer...');
            const offer = await peerConnection.createOffer();
            logToFile(`📋 Offer created: ${offer.type}`);
            await peerConnection.setLocalDescription(offer);
            
            // Send WebRTC offer with session assignment
            // Get availableSessions AFTER session creation so new session is included
            sendMessage({
              type: 'offer',
              sdp: offer.sdp,
              to: data.from,
              from: AGENT_ID,
              sessionId: sessionId,
              sessionName: sessionManager.getSession(sessionId).name,
              isNewSession: isNewSession,
              availableSessions: sessionManager.getAllSessions()
            });
            logToFile('✅ WebRTC offer sent with session assignment');
            
            // Force ICE gathering if it hasn't started within 2 seconds
            logToFile('[AGENT] 🔧 Setting up ICE gathering fallback timer...');
            setTimeout(() => {
              if (!peerConnection) {
                logToFile('[AGENT] ⚠️ ICE gathering timer fired but peerConnection is null (connection already closed)');
                return;
              }
              
              if (peerConnection.iceGatheringState === 'new') {
                logToFile('[AGENT] ⚠️ ICE gathering hasn\'t started - checking peer connection state');
                logToFile(`[AGENT] Current ICE gathering state: ${peerConnection.iceGatheringState}`);
                logToFile(`[AGENT] Current ICE connection state: ${peerConnection.iceConnectionState}`);
                try {
                  peerConnection.restartIce();
                  logToFile('[AGENT] 🔄 ICE restart triggered');
                } catch (error) {
                  logToFile(`[AGENT] ❌ Failed to restart ICE: ${error.message}`);
                }
              } else {
                logToFile(`[AGENT] ✅ ICE gathering is active: ${peerConnection.iceGatheringState}`);
              }
            }, 2000);
          } catch (error) {
            logToFile(`❌ Error handling client-hello: ${error.message} Stack: ${error.stack}`);
          }
          break;
        case 'answer':
          logToFile('[AGENT] 📥 Received WebRTC answer from client.');
          try {
            await peerConnection.setRemoteDescription(new wrtc.RTCSessionDescription({ type: 'answer', sdp: data.sdp }));
            logToFile('[AGENT] ✅ WebRTC answer processed successfully');
          } catch (error) {
            logToFile(`[AGENT] ❌ Error processing answer: ${error.message}`);
          }
          break;
        case 'candidate':
          logToFile('[AGENT] 🧊 Received ICE candidate from client.');
          try {
            if (data.candidate) {
              await peerConnection.addIceCandidate(new wrtc.RTCIceCandidate(data.candidate));
              logToFile('[AGENT] ✅ ICE candidate added successfully');
            }
          } catch (error) {
            logToFile(`[AGENT] ❌ Error adding ICE candidate: ${error.message}`);
          }
          break;
        default:
          logToFile(`[AGENT] ❓ Unknown message type: ${data.type}`);
      }
    } catch (error) {
      console.error('[AGENT] ❌ Error parsing message:', error, 'Raw message:', message);
    }
  });

  ws.on('close', () => {
    console.log('[AGENT] Disconnected from signaling server. Reconnecting...');
    setTimeout(connectToSignalingServer, 5000);
  });

  ws.on('error', (err) => {
    console.error('WebSocket error:', err.message);
  });
}

async function createPeerConnection(clientId) {
  logToFile('Creating new PeerConnection');
  logToFile(`🌐 Configuring ICE servers: ${iceServers.map(server => server.urls).join(', ')}`);
  
  // Enhanced WebRTC configuration for better ICE candidate generation
  const rtcConfig = {
    iceServers: iceServers,
    iceCandidatePoolSize: 10,  // Generate more ICE candidates
    iceTransportPolicy: 'all', // Use both STUN and TURN
    bundlePolicy: 'balanced'   // Optimize for connection establishment
  };
  
  logToFile(`⚙️ WebRTC config: ${JSON.stringify(rtcConfig)}`);
  peerConnection = new wrtc.RTCPeerConnection(rtcConfig);

  // Debug: Verify event handler is being attached
  logToFile('[AGENT] 🔧 Attaching ICE candidate event handler...');
  
  peerConnection.onicecandidate = (event) => {
    logToFile(`[AGENT] 🧊 ICE candidate event fired: ${event.candidate ? 'candidate found' : 'gathering complete'}`);
    if (event.candidate) {
      logToFile(`[AGENT] 📤 ICE candidate details: ${JSON.stringify({
        candidate: event.candidate.candidate,
        sdpMid: event.candidate.sdpMid,
        sdpMLineIndex: event.candidate.sdpMLineIndex
      })}`);
      logToFile('[AGENT] 📤 Sending ICE candidate to client...');
      sendMessage({ type: 'candidate', candidate: event.candidate, to: clientId, from: AGENT_ID });
      logToFile('[AGENT] ✅ ICE candidate sent successfully');
    } else {
      logToFile('[AGENT] 🏁 ICE candidate gathering complete.');
    }
  };

  // Agent creates the data channel (not client)
  logToFile('[AGENT] Creating data channel...');
  dataChannel = peerConnection.createDataChannel('terminal', {
    ordered: true
  });
  setupDataChannel(clientId);
  
  peerConnection.ondatachannel = (event) => {
    logToFile('[AGENT] Additional data channel received (this should not happen)');
  };

  peerConnection.oniceconnectionstatechange = () => {
    if (!peerConnection) {
      logToFile('[AGENT] ⚠️ ICE connection state change after peerConnection was closed');
      return;
    }
    
    logToFile(`[AGENT] 📊 ICE connection state changed: ${peerConnection.iceConnectionState}`);
    logToFile(`[AGENT] 📊 ICE gathering state: ${peerConnection.iceGatheringState}`);
    
    switch (peerConnection.iceConnectionState) {
      case 'new':
        logToFile('[AGENT] 🆕 ICE connection starting...');
        break;
      case 'checking':
        logToFile('[AGENT] 🔍 ICE connection checking candidates...');
        break;
      case 'connected':
        logToFile('[AGENT] ✅ WebRTC connection established!');
        break;
      case 'completed':
        logToFile('[AGENT] ✅ ICE connection completed successfully!');
        break;
      case 'failed':
        logToFile('[AGENT] ❌ ICE connection failed - no viable candidates');
        cleanup(clientId);
        break;
      case 'disconnected':
        logToFile('[AGENT] ⚠️ ICE connection disconnected');
        cleanup(clientId);
        break;
      case 'closed':
        logToFile('[AGENT] 🔐 ICE connection closed');
        break;
    }
  };

  peerConnection.onconnectionstatechange = () => {
    if (!peerConnection) {
      logToFile('[AGENT] ⚠️ Connection state change after peerConnection was closed');
      return;
    }
    
    logToFile(`[AGENT] 📡 Connection state changed: ${peerConnection.connectionState}`);
    
    switch (peerConnection.connectionState) {
      case 'new':
        logToFile('[AGENT] 🆕 Connection starting...');
        break;
      case 'connecting':
        logToFile('[AGENT] 🔄 Connection in progress...');
        break;
      case 'connected':
        logToFile('[AGENT] ✅ Peer connection fully established!');
        break;
      case 'disconnected':
        logToFile('[AGENT] ⚠️ Peer connection disconnected');
        break;
      case 'failed':
        logToFile('[AGENT] ❌ Peer connection failed completely');
        break;
      case 'closed':
        logToFile('[AGENT] 🔐 Peer connection closed');
        break;
    }
  };

  peerConnection.onicegatheringstatechange = () => {
    if (!peerConnection) {
      logToFile('[AGENT] ⚠️ ICE gathering state change after peerConnection was closed');
      return;
    }
    
    logToFile(`[AGENT] 🔍 ICE gathering state changed: ${peerConnection.iceGatheringState}`);
    
    switch (peerConnection.iceGatheringState) {
      case 'new':
        logToFile('[AGENT] 🆕 ICE gathering not started');
        break;
      case 'gathering':
        logToFile('[AGENT] 🔍 ICE gathering in progress...');
        break;
      case 'complete':
        logToFile('[AGENT] ✅ ICE gathering completed');
        break;
    }
  };
}

async function cleanup(clientId = null) {
  // Disconnect client from session manager
  if (clientId) {
    sessionManager.disconnectClient(clientId);
  } else {
    // Full agent shutdown - send final heartbeat with empty sessions BEFORE stopping heartbeat
    logToFile('[AGENT] Sending final heartbeat with empty sessions before shutdown...');

    if (heartbeatInterval) {
      const finalHeartbeatData = JSON.stringify({
        agentId: AGENT_ID,
        timestamp: Date.now(),
        activeSessions: 0,
        sessions: [], // Empty session list to clear dashboard
        localPort: process.env.LOCAL_PORT || 8080,
        capabilities: ['webrtc', 'direct_websocket'],
        status: 'shutting_down'
      });

      const options = {
        hostname: 'shellmirror.app',
        port: 443,
        path: '/php-backend/api/agent-heartbeat.php',
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Content-Length': Buffer.byteLength(finalHeartbeatData),
          'X-Agent-Secret': 'mac-agent-secret-2024',
          'X-Agent-ID': AGENT_ID
        }
      };

      try {
        await new Promise((resolve, reject) => {
          const req = https.request(options, (res) => {
            let responseData = '';
            res.on('data', (chunk) => {
              responseData += chunk;
            });
            res.on('end', () => {
              if (res.statusCode === 200) {
                logToFile('[AGENT] ✅ Sent final heartbeat with empty sessions');
                resolve();
              } else {
                logToFile(`[AGENT] ⚠️ Final heartbeat HTTP error: ${res.statusCode}`);
                resolve(); // Continue shutdown even if heartbeat fails
              }
            });
          });

          req.on('error', (error) => {
            logToFile(`[AGENT] ❌ Failed to send final heartbeat: ${error.message}`);
            resolve(); // Continue shutdown even if heartbeat fails
          });

          req.setTimeout(5000, () => {
            req.destroy();
            logToFile('[AGENT] ⚠️ Final heartbeat timed out after 5s');
            resolve(); // Continue shutdown even if heartbeat times out
          });

          req.write(finalHeartbeatData);
          req.end();
        });
      } catch (error) {
        logToFile(`[AGENT] ❌ Error sending final heartbeat: ${error.message}`);
      }
    }

    // Now stop heartbeat system
    stopHeartbeatSystem();
  }

  if (dataChannel) {
    dataChannel.close();
    dataChannel = null;
  }
  if (peerConnection) {
    peerConnection.close();
    peerConnection = null;
  }
}

// WebRTC data channel message size limits and chunking
const MAX_WEBRTC_MESSAGE_SIZE = 32 * 1024; // 32KB - conservative limit for compatibility
const CHUNK_TYPE_START = 'chunk_start';
const CHUNK_TYPE_DATA = 'chunk_data';  
const CHUNK_TYPE_END = 'chunk_end';

function sendLargeMessage(dataChannel, message, logPrefix = '[AGENT]') {
  try {
    const messageStr = JSON.stringify(message);
    const messageBytes = Buffer.byteLength(messageStr, 'utf8');
    
    if (messageBytes <= MAX_WEBRTC_MESSAGE_SIZE) {
      // Small message, send directly
      dataChannel.send(messageStr);
      logToFile(`${logPrefix} ✅ Sent small message (${messageBytes} bytes)`);
      return true;
    }
    
    // Large message, chunk it
    logToFile(`${logPrefix} 📦 Chunking large message (${messageBytes} bytes) into ${MAX_WEBRTC_MESSAGE_SIZE} byte chunks`);
    
    const chunkId = Date.now().toString() + Math.random().toString(36).substr(2, 9);
    const chunks = [];
    
    // Split message string into chunks
    for (let i = 0; i < messageStr.length; i += MAX_WEBRTC_MESSAGE_SIZE - 200) { // Reserve 200 bytes for chunk metadata
      chunks.push(messageStr.slice(i, i + MAX_WEBRTC_MESSAGE_SIZE - 200));
    }
    
    logToFile(`${logPrefix} 📦 Split into ${chunks.length} chunks`);
    
    // Send chunk start notification
    dataChannel.send(JSON.stringify({
      type: CHUNK_TYPE_START,
      chunkId: chunkId,
      totalChunks: chunks.length,
      totalSize: messageBytes,
      originalType: message.type
    }));
    
    // Send each chunk with a small delay to prevent overwhelming
    chunks.forEach((chunk, index) => {
      setTimeout(() => {
        try {
          dataChannel.send(JSON.stringify({
            type: CHUNK_TYPE_DATA,
            chunkId: chunkId,
            chunkIndex: index,
            data: chunk
          }));
          
          // Send end notification after last chunk
          if (index === chunks.length - 1) {
            setTimeout(() => {
              dataChannel.send(JSON.stringify({
                type: CHUNK_TYPE_END,
                chunkId: chunkId
              }));
              logToFile(`${logPrefix} ✅ Large message sent successfully (${chunks.length} chunks)`);
            }, 10);
          }
        } catch (err) {
          logToFile(`${logPrefix} ❌ Error sending chunk ${index}: ${err.message}`);
        }
      }, index * 10); // 10ms delay between chunks
    });
    
    return true;
  } catch (err) {
    logToFile(`${logPrefix} ❌ Error in sendLargeMessage: ${err.message}`);
    return false;
  }
}

function setupDataChannel(clientId) {
  dataChannel.onopen = () => {
    logToFile('[AGENT] ✅ Data channel is open!');
    
    // Send buffered output for existing session when data channel opens
    const session = sessionManager.getClientSession(clientId);
    if (session) {
      const bufferedOutput = session.buffer.getAll();
      if (bufferedOutput) {
        logToFile(`[AGENT] 📤 Sending buffered output to client (${bufferedOutput.length} chars)`);
        const success = sendLargeMessage(dataChannel, { type: 'output', data: bufferedOutput }, '[AGENT]');
        if (!success) {
          logToFile('[AGENT] ❌ Failed to send buffered output');
        }
      } else {
        logToFile('[AGENT] ℹ️ No buffered output to send for this session');
      }
    }
  };

  dataChannel.onmessage = (event) => {
    try {
      const message = JSON.parse(event.data);
      const session = sessionManager.getClientSession(clientId);
      
      if (!session) {
        logToFile(`[AGENT] ⚠️ No session found for client ${clientId}`);
        return;
      }

      if (message.type === 'input') {
        sessionManager.writeToSession(session.id, message.data);
      } else if (message.type === 'resize') {
        logToFile(`[AGENT] Resizing session ${session.id} to ${message.cols}x${message.rows}`);
        sessionManager.resizeSession(session.id, message.cols, message.rows);
      } else if (message.type === 'session-switch') {
        // Handle session switching
        logToFile(`[AGENT] Client ${clientId} switching to session ${message.sessionId}`);
        if (sessionManager.connectClientToSession(clientId, message.sessionId)) {
          // Send confirmation and buffered output
          const newSession = sessionManager.getSession(message.sessionId);
          dataChannel.send(JSON.stringify({
            type: 'session-switched',
            sessionId: message.sessionId,
            sessionName: newSession.name
          }));

          // Send buffered output for this session
          const bufferedOutput = newSession.buffer.getAll();
          if (bufferedOutput) {
            logToFile(`[AGENT] 📤 Sending ${bufferedOutput.length} chars of buffered output for session switch`);
            const success = sendLargeMessage(dataChannel, {
              type: 'output',
              data: bufferedOutput
            }, '[AGENT]');
            if (!success) {
              logToFile('[AGENT] ❌ Failed to send buffered output');
            }
          } else {
            logToFile('[AGENT] ℹ️ No buffered output for switched session');
          }
        } else {
          dataChannel.send(JSON.stringify({
            type: 'error',
            message: `Session ${message.sessionId} not found`
          }));
        }
      } else if (message.type === 'session-create') {
        // Handle new session creation via data channel
        logToFile(`[AGENT] Client ${clientId} creating new session`);

        const newSessionId = sessionManager.createSession(null, clientId);

        if (newSessionId) {
          const newSession = sessionManager.getSession(newSessionId);

          // Send confirmation with updated session list
          dataChannel.send(JSON.stringify({
            type: 'session-created',
            sessionId: newSessionId,
            sessionName: newSession.name,
            availableSessions: sessionManager.getAllSessions()
          }));

          logToFile(`[AGENT] ✅ New session created: ${newSessionId}`);
        } else {
          dataChannel.send(JSON.stringify({
            type: 'error',
            message: 'Failed to create session - maximum sessions reached'
          }));
          logToFile(`[AGENT] ❌ Failed to create session for client ${clientId}`);
        }
      } else if (message.type === 'close_session') {
        // Handle session closure request from client
        logToFile(`[AGENT] Client ${clientId} closing session ${message.sessionId}`);

        // Get remaining sessions BEFORE termination
        const closingSessionId = message.sessionId;
        sessionManager.terminateSession(closingSessionId);

        const remainingSessions = sessionManager.getAllSessions();

        // Send confirmation with updated session list
        dataChannel.send(JSON.stringify({
          type: 'session-closed',
          sessionId: closingSessionId,
          availableSessions: remainingSessions
        }));

        // If client requested auto-switch to next session, do it atomically
        if (message.switchToSessionId && remainingSessions.find(s => s.id === message.switchToSessionId)) {
          logToFile(`[AGENT] Auto-switching client to session ${message.switchToSessionId}`);
          if (sessionManager.connectClientToSession(clientId, message.switchToSessionId)) {
            const newSession = sessionManager.getSession(message.switchToSessionId);
            dataChannel.send(JSON.stringify({
              type: 'session-switched',
              sessionId: message.switchToSessionId,
              sessionName: newSession.name
            }));
            // Send buffered output
            const bufferedOutput = newSession.buffer.getAll();
            if (bufferedOutput) {
              sendLargeMessage(dataChannel, { type: 'output', data: bufferedOutput }, '[AGENT]');
            }
          }
        }

        // Send immediate heartbeat to update dashboard
        sendHeartbeat();

        logToFile(`[AGENT] ✅ Session closed: ${closingSessionId}`);
      }
    } catch (err) {
      logToFile(`[AGENT] Error parsing data channel message: ${err.message}`);
    }
  };

  dataChannel.onclose = () => {
    logToFile('[AGENT] Data channel closed.');
    cleanup(clientId);
  };

  dataChannel.onerror = (error) => {
    logToFile(`[AGENT] Data channel error: ${error.message}`);
  };
}


function sendMessage(message) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    logToFile(`[AGENT] Sending message: ${message.type}`);
    ws.send(JSON.stringify(message));
  } else {
    logToFile('[AGENT] Cannot send message - WebSocket not connected');
  }
}

// Graceful shutdown handling
process.on('SIGINT', async () => {
  console.log('\n[AGENT] Shutting down gracefully...');
  await cleanup();
  if (ws) ws.close();
  if (localServer) localServer.close();
  process.exit(0);
});

process.on('SIGTERM', async () => {
  console.log('\n[AGENT] Received SIGTERM, shutting down...');
  await cleanup();
  if (ws) ws.close();
  if (localServer) localServer.close();
  process.exit(0);
});

// --- Local WebSocket Server for Direct Connections ---
// Sessions storage for direct WebSocket connections
const directSessions = {};

function startLocalServer() {
  const localPort = process.env.LOCAL_PORT || 8080;
  const localServer = require('ws').Server;
  const wss = new localServer({ port: localPort });

  logToFile(`🏠 Starting local WebSocket server on port ${localPort}`);

  wss.on('connection', (localWs, request) => {
    const clientIp = request.socket.remoteAddress;
    logToFile(`🔗 Direct connection from ${clientIp}`);

    // Handle direct browser connections
    localWs.on('message', (data) => {
      try {
        const message = JSON.parse(data);
        logToFile(`[LOCAL] Received direct message: ${message.type}`);

        switch (message.type) {
          case 'ping':
            localWs.send(JSON.stringify({ type: 'pong', timestamp: Date.now() }));
            break;
            
          case 'authenticate':
            // For direct connections, we can implement simpler auth
            localWs.send(JSON.stringify({ 
              type: 'authenticated', 
              agentId: AGENT_ID,
              timestamp: Date.now()
            }));
            break;

          case 'create_session':
            // Check if client requested an existing session
            let sessionId;
            let isNewSession = false;

            if (message.sessionId && directSessions[message.sessionId]) {
              // Reconnect to existing session
              sessionId = message.sessionId;
              logToFile(`[LOCAL] Reconnecting to existing session: ${sessionId}`);

              // Update activity timestamp
              directSessions[sessionId].lastActivity = Date.now();

              // Re-attach output handler for this connection
              directSessions[sessionId].pty.onData((data) => {
                if (localWs.readyState === WebSocket.OPEN) {
                  localWs.send(JSON.stringify({
                    type: 'output',
                    sessionId,
                    data
                  }));
                }
              });

              // Send buffered output if available
              const bufferedOutput = directSessions[sessionId].buffer.getAll();
              if (bufferedOutput.length > 0) {
                localWs.send(JSON.stringify({
                  type: 'output',
                  sessionId,
                  data: bufferedOutput.join('')
                }));
              }
            } else {
              // Create new terminal session
              sessionId = uuidv4();
              isNewSession = true;

              const ptyProcess = pty.spawn(shell, [], {
                name: 'xterm-color',
                cols: message.cols || 120,
                rows: message.rows || 30,
                cwd: process.env.HOME,
                env: process.env
              });

              // Store session
              directSessions[sessionId] = {
                pty: ptyProcess,
                buffer: new CircularBuffer(),
                lastActivity: Date.now()
              };

              // Send session output to direct connection
              ptyProcess.onData((data) => {
                if (localWs.readyState === WebSocket.OPEN) {
                  localWs.send(JSON.stringify({
                    type: 'output',
                    sessionId,
                    data
                  }));
                }
                // Store in buffer for reconnection
                directSessions[sessionId].buffer.add(data);
              });

              logToFile(`[LOCAL] Created new direct session: ${sessionId}`);

              // Enable case-insensitive tab completion for direct sessions
              enableCaseInsensitiveCompletion(ptyProcess, shell);
            }

            localWs.send(JSON.stringify({
              type: 'session_created',
              sessionId,
              sessionName: `Session ${sessionId.slice(0, 8)}`,
              isNewSession: isNewSession,
              cols: message.cols || 120,
              rows: message.rows || 30
            }));
            break;

          case 'input':
            // Handle terminal input for direct connection
            if (directSessions[message.sessionId]) {
              directSessions[message.sessionId].pty.write(message.data);
              directSessions[message.sessionId].lastActivity = Date.now();
            }
            break;

          case 'resize':
            // Handle terminal resize for direct connection
            if (directSessions[message.sessionId]) {
              directSessions[message.sessionId].pty.resize(message.cols, message.rows);
            }
            break;

          default:
            logToFile(`[LOCAL] Unknown message type: ${message.type}`);
        }
      } catch (err) {
        logToFile(`[LOCAL] Error parsing message: ${err.message}`);
      }
    });

    localWs.on('close', () => {
      logToFile(`[LOCAL] Direct connection from ${clientIp} closed`);
    });

    localWs.on('error', (error) => {
      logToFile(`[LOCAL] Direct connection error: ${error.message}`);
    });
  });

  logToFile(`✅ Local WebSocket server started on port ${localPort}`);
  return wss;
}

// --- Start the agent ---
console.log(`[AGENT] Starting Mac Agent with ID: ${AGENT_ID}`);

// Start local server for direct connections (LAN-direct path; no cloud needed)
const localServer = startLocalServer();

// Register with the PHP backend and open the dashboard regardless of cloud
// signaling, so the agent stays usable over LAN-direct without Heroku.
startHeartbeatSystem();
openDashboardInBrowser();

console.log(`[AGENT] Connecting to signaling server at: ${SIGNALING_SERVER_URL}`);
connectToSignalingServer();
