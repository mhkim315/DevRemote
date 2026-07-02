const term = new Terminal({
    cursorBlink: true,
    macOptionIsMeta: true,
    scrollback: 1000,
    // Mac Terminal.app appearance settings
    theme: {
        background: '#000000',        // Pure black like Mac Terminal
        foreground: '#ffffff',        // White text
        cursor: '#ffffff',            // White cursor
        cursorAccent: '#000000',      // Black cursor accent
        selection: '#5c5c5c',         // Mac selection color
        // Mac Terminal color palette
        black: '#000000',
        red: '#c23621',
        green: '#25bc24',
        yellow: '#adad27',
        blue: '#492ee1',
        magenta: '#d338d3',
        cyan: '#33bbc8',
        white: '#cbcccd',
        brightBlack: '#818383',
        brightRed: '#fc391f',
        brightGreen: '#31e722',
        brightYellow: '#eaec23',
        brightBlue: '#5833ff',
        brightMagenta: '#f935f8',
        brightCyan: '#14f0f0',
        brightWhite: '#e9ebeb'
    },
    fontFamily: '"SF Mono", Monaco, Menlo, "Ubuntu Mono", monospace', // Mac system fonts
    fontSize: 11,                     // Mac Terminal default size
    lineHeight: 1.2,                  // Mac Terminal line spacing
    letterSpacing: 0,                 // Tight character spacing like Mac
    allowTransparency: false,         // Solid background
    convertEol: true,                 // Convert line endings properly
    cols: 120,                        // Match agent terminal width
    rows: 30                          // Match agent terminal height
});
const fitAddon = new FitAddon.FitAddon();
term.loadAddon(fitAddon);

const connectContainer = document.getElementById('connect-container');
const terminalContainer = document.getElementById('terminal-container');

let ws;
let peerConnection;
let dataChannel;
let user;
let AGENT_ID;
let CLIENT_ID;
let SELECTED_AGENT; // Store full agent data including WebSocket URL
let usingDirectConnection = false; // Flag to prevent handler overwrite
let directProbeAborted = false; // Set when the direct-connection phase is over; late probe successes must not take over
const DIRECT_PHASE_DEADLINE_MS = 5000; // Global budget for the direct phase on https before WebRTC fallback

// Session management
let currentSession = null;
let availableSessions = [];
let requestedSessionId = null; // For connecting to specific session from URL

// Connection status messaging
let connectionStatusMessage = 'Connecting to agent...';
let connectionTimeoutWarning = null;

// Chunk reassembly for large messages
const chunkAssembler = {
    activeChunks: new Map(),
    
    handleChunkedMessage(message) {
        const { type, chunkId } = message;
        
        switch (type) {
            case 'chunk_start':
                console.log(`[CLIENT] 📦 Starting chunk reassembly: ${chunkId} (${message.totalChunks} chunks, ${message.totalSize} bytes)`);
                this.activeChunks.set(chunkId, {
                    originalType: message.originalType,
                    totalChunks: message.totalChunks,
                    totalSize: message.totalSize,
                    receivedChunks: new Map(),
                    startTime: Date.now()
                });
                return true;
                
            case 'chunk_data':
                const chunkInfo = this.activeChunks.get(chunkId);
                if (!chunkInfo) {
                    console.error(`[CLIENT] ❌ Received chunk data for unknown chunk ID: ${chunkId}`);
                    return true;
                }
                
                chunkInfo.receivedChunks.set(message.chunkIndex, message.data);
                console.log(`[CLIENT] 📦 Received chunk ${message.chunkIndex + 1}/${chunkInfo.totalChunks}`);
                return true;
                
            case 'chunk_end':
                return this.reassembleChunks(chunkId);
                
            default:
                return false; // Not a chunk message
        }
    },
    
    reassembleChunks(chunkId) {
        const chunkInfo = this.activeChunks.get(chunkId);
        if (!chunkInfo) {
            console.error(`[CLIENT] ❌ Cannot reassemble unknown chunk: ${chunkId}`);
            return true;
        }
        
        try {
            // Check if we have all chunks
            if (chunkInfo.receivedChunks.size !== chunkInfo.totalChunks) {
                console.error(`[CLIENT] ❌ Missing chunks: expected ${chunkInfo.totalChunks}, got ${chunkInfo.receivedChunks.size}`);
                return true;
            }
            
            // Reassemble chunks in order
            let reassembledData = '';
            for (let i = 0; i < chunkInfo.totalChunks; i++) {
                if (!chunkInfo.receivedChunks.has(i)) {
                    console.error(`[CLIENT] ❌ Missing chunk ${i}`);
                    return true;
                }
                reassembledData += chunkInfo.receivedChunks.get(i);
            }
            
            const elapsed = Date.now() - chunkInfo.startTime;
            console.log(`[CLIENT] ✅ Reassembled ${chunkInfo.totalChunks} chunks in ${elapsed}ms (${reassembledData.length} chars)`);
            
            // Parse and process the reassembled message
            const originalMessage = JSON.parse(reassembledData);
            this.activeChunks.delete(chunkId);
            
            // Process the original message
            if (originalMessage.type === 'output') {
                term.write(originalMessage.data);
            } else {
                handleSessionMessage(originalMessage);
            }
            
            return true;
        } catch (err) {
            console.error(`[CLIENT] ❌ Error reassembling chunks for ${chunkId}:`, err);
            this.activeChunks.delete(chunkId);
            return true;
        }
    },
    
    cleanup() {
        // Clean up old incomplete chunks (older than 30 seconds)
        const now = Date.now();
        for (const [chunkId, chunkInfo] of this.activeChunks.entries()) {
            if (now - chunkInfo.startTime > 30000) {
                console.log(`[CLIENT] 🧹 Cleaning up stale chunk: ${chunkId}`);
                this.activeChunks.delete(chunkId);
            }
        }
    }
};

// Connection status management
function updateConnectionStatus(status) {
    const statusElement = document.getElementById('connection-status');
    if (!statusElement) return;

    statusElement.className = 'connection-status';
    if (status === 'connecting') {
        statusElement.classList.add('connecting');
    } else if (status === 'connected') {
        statusElement.classList.add('connected');
    }
    // else: disconnected (default red)
}

// Set connection status message (shown in tab bar when no sessions)
function setConnectionMessage(message, writeToTerminal = true) {
    connectionStatusMessage = message;
    console.log('[CLIENT] 📢 Connection message:', message);

    // Update the tab bar display
    updateSessionDisplay();

    // Optionally write to terminal for visibility
    if (writeToTerminal && term) {
        term.write(`\r\n\x1b[36m${message}\x1b[0m\r\n`); // Cyan color
    }
}

// Clear connection timeout warnings (called when connection succeeds)
function clearConnectionTimeouts() {
    if (connectionTimeoutWarning) {
        clearTimeout(connectionTimeoutWarning.timeout10s);
        clearTimeout(connectionTimeoutWarning.timeout30s);
        clearTimeout(connectionTimeoutWarning.timeout60s);
        connectionTimeoutWarning = null;
        console.log('[CLIENT] ✅ Connection timeout warnings cleared');
    }
}

// Cleanup timer for chunk assembler
setInterval(() => {
    chunkAssembler.cleanup();
}, 30000); // Clean up every 30 seconds

// Check for agent parameter and connect directly
window.addEventListener('load', () => {
    loadVersionInfo();
    
    // Wait for GA script to load and send page view
    setTimeout(() => {
        console.log('🔍 [TERMINAL DEBUG] Checking Google Analytics setup...');
        console.log('🔍 [TERMINAL DEBUG] gtag function type:', typeof gtag);
        console.log('🔍 [TERMINAL DEBUG] gtagLoaded flag:', window.gtagLoaded);
        
        // Send terminal page view event
        if (typeof sendGAEvent === 'function') {
            sendGAEvent('page_view', {
                page_title: 'Shell Mirror Terminal',
                page_location: window.location.href
            });
        }
    }, 1000);
    
    // Get agent ID and session ID from URL parameters
    const urlParams = new URLSearchParams(window.location.search);
    const agentId = urlParams.get('agent');
    const sessionId = urlParams.get('session');
    
    console.log('[CLIENT] 🔍 DEBUG: URL params - agent:', agentId, 'session:', sessionId);
    console.log('[CLIENT] 🔍 DEBUG: Full URL:', window.location.href);
    
    if (agentId) {
        AGENT_ID = agentId;
        SELECTED_AGENT = { id: agentId, agentId: agentId };
        requestedSessionId = sessionId; // Store for session request
        console.log('[CLIENT] 🔍 DEBUG: Set requestedSessionId to:', requestedSessionId);
        console.log('[CLIENT] 🔗 Connecting to agent:', agentId, sessionId ? `session: ${sessionId}` : '(new session)');
        startConnection();
    } else {
        // No agent specified, redirect to dashboard
        console.log('[CLIENT] ❌ No agent specified, redirecting to dashboard');
        window.location.href = '/app/dashboard.html';
    }
});

// Load version info into dropdown
async function loadVersionInfo() {
    try {
        const response = await fetch('/build-info.json');
        const buildInfo = await response.json();

        if (buildInfo) {
            const buildDateTime = new Date(buildInfo.buildTime).toLocaleString('en-US', {
                month: 'short',
                day: 'numeric',
                year: 'numeric',
                hour: '2-digit',
                minute: '2-digit'
            });

            const versionElement = document.getElementById('version-info-dropdown');
            if (versionElement) {
                versionElement.textContent = `v${buildInfo.version} • Built ${buildDateTime}`;
            }
        }
    } catch (error) {
        console.log('Could not load build info for terminal:', error);
    }
}

// Update connection detail in dropdown
function startConnection() {
    updateConnectionStatus('connecting');
    connectContainer.style.display = 'none';
    terminalContainer.classList.add('show');
    term.open(document.getElementById('terminal'));

    // Show initial connection message
    setConnectionMessage('Connecting to agent...', true);

    // Initialize session display (shows header with connection status even before session exists)
    updateSessionDisplay();

    // Track terminal session start in Google Analytics
    if (typeof sendGAEvent === 'function') {
        sendGAEvent('terminal_session_start', {
            event_category: 'terminal',
            event_label: requestedSessionId ? 'existing_session' : 'new_session',
            agent_id: AGENT_ID,
            session_id: requestedSessionId || 'new'
        });
    }
    
    // Delay fit to ensure proper dimensions after CSS transitions
    setTimeout(() => {
        fitAddon.fit();
        term.focus(); // Ensure cursor is visible even before connection
    }, 100);

    // Set up connection timeout warnings
    const timeout10s = setTimeout(() => {
        if (!currentSession) {
            setConnectionMessage('Taking longer than usual... Please wait', true);
            term.write('\r\n\x1b[33mConnection is taking longer than expected...\x1b[0m\r\n');
        }
    }, 10000);

    const timeout30s = setTimeout(() => {
        if (!currentSession) {
            setConnectionMessage('Connection very slow - Agent may be offline', true);
            term.write('\x1b[33mStill trying to connect. The agent may be offline or unreachable.\x1b[0m\r\n');
            term.write('\x1b[36mTip: Check the agent status on the Dashboard\x1b[0m\r\n');
        }
    }, 30000);

    const timeout60s = setTimeout(() => {
        if (!currentSession) {
            updateConnectionStatus('disconnected');
            setConnectionMessage('Failed to connect - Agent or session unavailable', false);
            term.write('\r\n\r\n\x1b[31mConnection Failed\x1b[0m\r\n');
            term.write('\x1b[33mThe agent or session you requested is not available.\x1b[0m\r\n');
            term.write('\r\n\x1b[36mPossible reasons:\x1b[0m\r\n');
            term.write('  • Agent is offline or shut down\r\n');
            term.write('  • Session was terminated\r\n');
            term.write('  • Network connectivity issues\r\n');
            term.write('\r\n\x1b[36mClick Dashboard to return and try another session\x1b[0m\r\n');

            // Make Dashboard button pulsate calmly to guide user back
            const dashboardBtn = document.querySelector('.dashboard-btn');
            if (dashboardBtn) {
                dashboardBtn.classList.add('pulsate');
            }
        }
    }, 60000);

    // Store timeout IDs so they can be cleared on successful connection
    connectionTimeoutWarning = { timeout10s, timeout30s, timeout60s };

    initialize();
}


async function initialize() {
    console.log('[CLIENT] 🚀 Initializing connection to agent:', AGENT_ID);
    console.log('[CLIENT] 📋 Selected agent data:', SELECTED_AGENT);
    
    // First try direct connection to agent.
    // On https, browsers block ws:// to anything except localhost (mixed content), and
    // black-holed probes on cellular can stall for minutes — so cap the whole direct
    // phase with a single global deadline before falling back to WebRTC signaling.
    // On http (local dev), behavior is unchanged: full sequential probing, no deadline.
    directProbeAborted = false;
    let directConnectionSuccess;
    if (window.location.protocol === 'https:') {
        directConnectionSuccess = await Promise.race([
            tryDirectConnection(),
            new Promise((resolve) => setTimeout(() => resolve(false), DIRECT_PHASE_DEADLINE_MS))
        ]);
        if (!directConnectionSuccess) {
            // Deadline fired (or all probes failed) — close the direct phase so a
            // late-resolving probe can't clobber ws/usingDirectConnection after
            // WebRTC signaling has started. This runs in the microtask continuation
            // of the race, so it always wins over any not-yet-fired socket onopen.
            directProbeAborted = true;
        }
    } else {
        directConnectionSuccess = await tryDirectConnection();
    }

    if (directConnectionSuccess) {
        console.log('[CLIENT] ✅ Direct connection established - no server needed!');
        setConnectionMessage('Connected via local network!', true);
        return;
    }

    console.log('[CLIENT] ⚠️ Direct connection failed, falling back to WebRTC signaling...');
    setConnectionMessage('Attempting WebRTC connection...', true);
    await initializeWebRTCSignaling();
}

async function tryDirectConnection() {
    console.log('[CLIENT] 🔗 Attempting direct connection to agent...');
    updateConnectionStatus('connecting');
    setConnectionMessage('Trying direct connection to local network...', true);

    // Get agent data from API to find local connection details
    try {
        const response = await fetch('/php-backend/api/agents-list.php', {
            credentials: 'include'
        });
        
        const data = await response.json();
        if (!data.success || !data.data.agents) {
            console.log('[CLIENT] ❌ Could not get agent list for direct connection');
            return false;
        }
        
        const agent = data.data.agents.find(a => a.agentId === AGENT_ID);
        if (!agent || !agent.localPort) {
            console.log('[CLIENT] ❌ Agent not found or no local port information');
            return false;
        }
        
        // Try common local IPs for the agent.
        // On https, browsers block ws:// to private LAN IPs (mixed content; only
        // localhost/127.0.0.1 are exempt), so probing the LAN list is pure dead time —
        // restrict to the loopback variants. On http (local dev), keep the full list.
        const possibleIPs = window.location.protocol === 'https:'
            ? ['localhost', '127.0.0.1']
            : [
                'localhost',
                '127.0.0.1',
                // Common private network ranges
                ...generatePrivateIPCandidates()
            ];

        for (const ip of possibleIPs) {
            if (directProbeAborted) {
                console.log('[CLIENT] ⏰ Direct phase deadline reached - stopping remaining probes');
                return false;
            }
            const success = await tryDirectConnectionToIP(ip, agent.localPort);
            if (success) {
                return true;
            }
        }
        
        console.log('[CLIENT] ❌ Direct connection failed to all IP candidates');
        updateConnectionStatus('disconnected');
        return false;
        
    } catch (error) {
        console.log('[CLIENT] ❌ Error during direct connection attempt:', error);
        return false;
    }
}

async function tryDirectConnectionToIP(ip, port) {
    return new Promise((resolve) => {
        console.log(`[CLIENT] 🔍 Trying direct connection to ${ip}:${port}`);
        
        const directWs = new WebSocket(`ws://${ip}:${port}`);
        const timeout = setTimeout(() => {
            console.log(`[CLIENT] ⏰ Connection timeout to ${ip}:${port}`);
            directWs.close();
            resolve(false);
        }, 3000); // 3 second timeout
        
        directWs.onopen = () => {
            clearTimeout(timeout);
            if (directProbeAborted) {
                // Direct phase already lost the race to the global deadline and
                // WebRTC signaling owns the connection now — do NOT set up handlers
                // or clobber ws/usingDirectConnection. Discard this socket.
                console.log(`[CLIENT] ⏰ Late direct connection to ${ip}:${port} ignored (deadline already fired)`);
                directWs.close();
                resolve(false);
                return;
            }
            console.log(`[CLIENT] ✅ Direct connection established to ${ip}:${port}`);

            // Set up the direct connection handlers
            setupDirectConnection(directWs);
            resolve(true);
        };
        
        directWs.onerror = () => {
            clearTimeout(timeout);
            console.log(`[CLIENT] ❌ Connection failed to ${ip}:${port}`);
            resolve(false);
        };
        
        directWs.onclose = () => {
            clearTimeout(timeout);
            resolve(false);
        };
    });
}

function setupDirectConnection(directWs) {
    console.log('[CLIENT] 🔧 Setting up direct connection handlers');

    // Store the WebSocket for global access
    ws = directWs;
    usingDirectConnection = true; // Prevent signaling handler from overwriting

    // Set up message handlers
    directWs.onmessage = (event) => {
        const data = JSON.parse(event.data);
        console.log(`[CLIENT] 📨 Direct message: ${data.type}`);
        
        switch (data.type) {
            case 'pong':
                console.log('[CLIENT] 🏓 Received pong from direct connection');
                break;
                
            case 'authenticated':
                console.log('[CLIENT] ✅ Direct authentication successful');
                // Request session creation
                directWs.send(JSON.stringify({
                    type: 'create_session',
                    sessionId: requestedSessionId,
                    cols: term.cols,
                    rows: term.rows
                }));
                break;
                
            case 'session_created':
                console.log('[CLIENT] ✅ Direct session created:', data.sessionId);

                // Clear connection timeout warnings
                clearConnectionTimeouts();
                setConnectionMessage('Session created successfully!', false);

                // Update current session
                currentSession = {
                    id: data.sessionId,
                    name: data.sessionName || 'Terminal Session'
                };

                // Update available sessions
                if (data.availableSessions) {
                    availableSessions = data.availableSessions;
                } else {
                    // If agent doesn't provide session list, add this session manually
                    if (!availableSessions.find(s => s.id === currentSession.id)) {
                        availableSessions.push(currentSession);
                    }
                }

                // Clear terminal and show success message with session color
                term.clear();
                const sessionColor = getSessionColor(currentSession.id);
                term.write(`\r\n\x1b[38;2;${sessionColor.ansi}m✨ New session created: ${currentSession.name}\x1b[0m\r\n\r\n`);

                // Update URL with session ID so refresh reconnects to same session
                updateUrlWithSession(data.sessionId);

                // Update UI
                updateSessionDisplay();

                // Save to localStorage
                saveSessionToLocalStorage(AGENT_ID, currentSession);
                break;
                
            case 'output':
                // Handle terminal output
                if (data.sessionId === currentSession?.id) {
                    term.write(data.data);
                }
                break;
                
            default:
                console.log('[CLIENT] ❓ Unknown direct message type:', data.type);
        }
    };
    
    directWs.onclose = () => {
        console.log('[CLIENT] ❌ Direct connection closed');
        updateConnectionStatus('disconnected');
    };
    
    directWs.onerror = (error) => {
        console.error('[CLIENT] ❌ Direct connection error:', error);
    };
    
    // Terminal input is handled by the single module-level term.onData
    // registration (see "One-time terminal event wiring" below).

    // Send authentication
    directWs.send(JSON.stringify({
        type: 'authenticate',
        agentId: AGENT_ID
    }));
    
    updateConnectionStatus('connected');
}

function generatePrivateIPCandidates() {
    // Generate most common private network IP candidates
    const candidates = [];
    
    // Most common home router ranges (limit to most popular subnets)
    const commonSubnets = [0, 1, 2, 10, 100];
    for (const subnet of commonSubnets) {
        // Common host IPs: router (1), common DHCP assignments
        const hosts = [1, 2, 10, 100, 101, 150];
        for (const host of hosts) {
            candidates.push(`192.168.${subnet}.${host}`);
        }
    }
    
    // Common corporate/enterprise ranges (just the most common ones)
    candidates.push(
        '10.0.0.1', '10.0.0.2', '10.0.0.100',
        '10.0.1.1', '10.0.1.100',
        '172.16.0.1', '172.16.0.100'
    );
    
    return candidates;
}

async function initializeWebRTCSignaling() {
    console.log('[CLIENT] 🚀 Initializing WebRTC signaling connection to agent:', AGENT_ID);
    
    // Signaling endpoint comes from config.js (single source of truth, so the
    // deployment can move off Heroku without code changes). Empty => cloud
    // signaling disabled; rely on the LAN-direct path instead.
    const signalingUrl = (window.SHELL_MIRROR_CONFIG && window.SHELL_MIRROR_CONFIG.SIGNALING_URL) || '';
    if (!signalingUrl) {
        console.log('[CLIENT] ☁️ Cloud signaling disabled (config SIGNALING_URL is empty) — using LAN-direct only.');
        return;
    }
    console.log('[CLIENT] 🌐 Using signaling server:', signalingUrl);

    ws = new WebSocket(`${signalingUrl}?role=client`);
    
    ws.onopen = () => {
        console.log('[CLIENT] ✅ WebSocket connection to signaling server opened.');
    };
    ws.onmessage = async (message) => {
        const data = JSON.parse(message.data);
        console.log(`[CLIENT] Received message of type: ${data.type}`);
        switch (data.type) {
            case 'server-hello':
                CLIENT_ID = data.id;
                console.log(`[CLIENT] Assigned Client ID: ${CLIENT_ID}`);
                
                // First send a test message to verify communication
                console.log(`[CLIENT] 🧪 Sending test ping message first...`);
                const testSent = sendMessage({ type: 'ping', from: CLIENT_ID, to: AGENT_ID, timestamp: Date.now() });
                
                if (!testSent) {
                    console.error(`[CLIENT] ❌ Failed to send test message - WebSocket connection broken`);
                    return;
                }
                
                // Start polling to connect to the agent
                const intervalId = setInterval(() => {
                    console.log(`[CLIENT] 📞 Sending client-hello to Agent: ${AGENT_ID}`);
                    
                    // Build session request
                    let sessionRequest = null;
                    console.log('[CLIENT] 🔍 DEBUG: Building session request, requestedSessionId:', requestedSessionId);
                    
                    if (requestedSessionId) {
                        sessionRequest = { sessionId: requestedSessionId };
                        console.log(`[CLIENT] 🎯 Requesting existing session: ${requestedSessionId}`);
                    } else {
                        sessionRequest = { newSession: true };
                        console.log(`[CLIENT] 🆕 Requesting new session`);
                    }
                    
                    const sent = sendMessage({ 
                        type: 'client-hello', 
                        from: CLIENT_ID, 
                        to: AGENT_ID,
                        sessionRequest: sessionRequest
                    });
                    if (!sent) {
                        console.error(`[CLIENT] ❌ Failed to send client-hello - stopping attempts`);
                        clearInterval(intervalId);
                    }
                }, 1000);

                // This is a bit of a hack for the message handler.
                // We redefine it to handle the next phase of messages.
                // Skip if using direct connection - don't overwrite its handler!
                if (!usingDirectConnection) {
                    ws.onmessage = async (nextMessage) => {
                    let messageData = nextMessage.data;
                    
                    // Handle Blob messages by converting to text first
                    if (messageData instanceof Blob) {
                        console.log(`[CLIENT] 📄 Received Blob message, converting to text...`);
                        messageData = await messageData.text();
                    }
                    
                    try {
                        const nextData = JSON.parse(messageData);
                        console.log(`[CLIENT] 📨 Received message of type: ${nextData.type}`);
                        
                        if (nextData.type === 'offer') {
                            console.log('[CLIENT] Received offer from agent. Stopping client-hello retries.');
                            clearInterval(intervalId);

                            // Handle session assignment from agent
                            if (nextData.sessionId) {
                                // Clear connection timeout warnings
                                clearConnectionTimeouts();
                                setConnectionMessage('Session connected!', false);

                                currentSession = {
                                    id: nextData.sessionId,
                                    name: nextData.sessionName || 'Terminal Session',
                                    isNewSession: nextData.isNewSession || false
                                };
                                console.log('[CLIENT] 📋 Session assigned:', currentSession);
                                console.log('[CLIENT] 🔍 Agent ID for storage:', AGENT_ID);

                                // Update UI to show session info
                                updateSessionDisplay();
                                
                                // Save session info to localStorage for dashboard
                                saveSessionToLocalStorage(AGENT_ID, currentSession);
                            }
                            
                            if (nextData.availableSessions) {
                                availableSessions = nextData.availableSessions;
                                console.log('[CLIENT] 📚 Available sessions:', availableSessions);

                                // Re-render tabs with updated session list
                                if (currentSession) {
                                    renderTabs();
                                }
                            }

                            console.log('[CLIENT] Received WebRTC offer from agent.');
                            await createPeerConnection();
                            await peerConnection.setRemoteDescription(new RTCSessionDescription(nextData));
                            const answer = await peerConnection.createAnswer();
                            await peerConnection.setLocalDescription(answer);
                            console.log('[CLIENT] Sending WebRTC answer to agent.');
                            sendMessage({ type: 'answer', sdp: answer.sdp, to: AGENT_ID, from: CLIENT_ID });
                            
                            // Force ICE gathering if it hasn't started within 2 seconds
                            console.log('[CLIENT] 🔧 Setting up ICE gathering fallback timer...');
                            setTimeout(() => {
                                if (peerConnection.iceGatheringState === 'new') {
                                    console.log('[CLIENT] ⚠️ ICE gathering hasn\'t started - triggering restart');
                                    try {
                                        peerConnection.restartIce();
                                    } catch (error) {
                                        console.error('[CLIENT] ❌ Failed to restart ICE:', error);
                                    }
                                } else {
                                    console.log('[CLIENT] ✅ ICE gathering is active:', peerConnection.iceGatheringState);
                                }
                            }, 2000);
                        } else if (nextData.type === 'candidate') {
                            console.log('[CLIENT] 🧊 Received ICE candidate from agent:', {
                                candidate: nextData.candidate.candidate,
                                sdpMid: nextData.candidate.sdpMid,
                                sdpMLineIndex: nextData.candidate.sdpMLineIndex
                            });
                            if (peerConnection) {
                                try {
                                    await peerConnection.addIceCandidate(new RTCIceCandidate(nextData.candidate));
                                    console.log('[CLIENT] ✅ ICE candidate added successfully');
                                } catch (error) {
                                    console.error('[CLIENT] ❌ Error adding ICE candidate:', error);
                                }
                            } else {
                                console.error('[CLIENT] ❌ Cannot add ICE candidate - no peer connection');
                            }
                        }
                    } catch (error) {
                        console.error(`[CLIENT] ❌ Error processing WebRTC message:`, error);
                    }
                };
                } // End if (!usingDirectConnection)
                break;
        }
    };

    ws.onclose = (event) => {
        console.log(`[CLIENT] 🔌 Disconnected from signaling server. Code: ${event.code}, Reason: ${event.reason}`);
        term.write('\r\n\r\nConnection to server lost. Please refresh.\r\n');
    };

    ws.onerror = (error) => {
        console.error('[CLIENT] ❌ WebSocket error:', error);
    };
}

async function testSTUNConnectivity() {
    console.log('[CLIENT] 🧪 Testing STUN server connectivity...');
    
    try {
        // Create a test peer connection to check STUN server access
        const testPC = new RTCPeerConnection({
            iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
        });
        
        let candidateReceived = false;
        
        return new Promise((resolve) => {
            const timeout = setTimeout(() => {
                console.log('[CLIENT] ⚠️ STUN connectivity test timed out - may indicate network restrictions');
                testPC.close();
                resolve(false);
            }, 5000);
            
            testPC.onicecandidate = (event) => {
                if (event.candidate && !candidateReceived) {
                    candidateReceived = true;
                    console.log('[CLIENT] ✅ STUN server connectivity confirmed');
                    clearTimeout(timeout);
                    testPC.close();
                    resolve(true);
                }
            };
            
            // Create a dummy data channel to trigger ICE gathering
            testPC.createDataChannel('test');
            testPC.createOffer().then(offer => testPC.setLocalDescription(offer));
        });
    } catch (error) {
        console.error('[CLIENT] ❌ STUN connectivity test failed:', error);
        return false;
    }
}

async function createPeerConnection() {
    console.log('[CLIENT] Creating PeerConnection.');
    
    // Test STUN connectivity first
    const stunWorking = await testSTUNConnectivity();
    if (!stunWorking) {
        console.log('[CLIENT] ⚠️ STUN servers may be blocked - using TURN servers for connectivity');
    }
    
    // Test STUN server connectivity with multiple backup servers
    console.log('[CLIENT] 🌐 Configuring ICE servers with multiple STUN/TURN options...');
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
    
    console.log('[CLIENT] 📋 Configured ICE servers:', iceServers.map(server => server.urls));
    
    // Enhanced WebRTC configuration for better ICE candidate generation
    const rtcConfig = {
        iceServers: iceServers,
        iceCandidatePoolSize: 10,  // Generate more ICE candidates
        iceTransportPolicy: 'all', // Use both STUN and TURN
        bundlePolicy: 'balanced'   // Optimize for connection establishment
    };
    
    console.log('[CLIENT] ⚙️ WebRTC config:', rtcConfig);
    peerConnection = new RTCPeerConnection(rtcConfig);

    // Debug: Verify event handler is being attached
    console.log('[CLIENT] 🔧 Attaching ICE candidate event handler...');
    
    peerConnection.onicecandidate = (event) => {
        console.log('[CLIENT] 🧊 ICE candidate event fired:', event.candidate ? 'candidate found' : 'gathering complete');
        if (event.candidate) {
            console.log('[CLIENT] 📤 ICE candidate details:', {
                candidate: event.candidate.candidate,
                sdpMid: event.candidate.sdpMid,
                sdpMLineIndex: event.candidate.sdpMLineIndex
            });
            console.log('[CLIENT] 📤 Sending ICE candidate to agent...');
            const sent = sendMessage({ type: 'candidate', candidate: event.candidate, to: AGENT_ID, from: CLIENT_ID });
            if (sent) {
                console.log('[CLIENT] ✅ ICE candidate sent successfully');
            } else {
                console.log('[CLIENT] ❌ Failed to send ICE candidate');
            }
        } else {
            console.log('[CLIENT] 🏁 ICE candidate gathering complete.');
        }
    };

    peerConnection.oniceconnectionstatechange = () => {
        console.log(`[CLIENT] 📊 ICE connection state changed: ${peerConnection.iceConnectionState}`);
        console.log(`[CLIENT] 📊 ICE gathering state: ${peerConnection.iceGatheringState}`);
        
        switch (peerConnection.iceConnectionState) {
            case 'new':
                console.log('[CLIENT] 🆕 ICE connection starting...');
                break;
            case 'checking':
                console.log('[CLIENT] 🔍 ICE connection checking candidates...');
                break;
            case 'connected':
                console.log('[CLIENT] ✅ WebRTC connection established!');
                updateConnectionStatus('connected');
                
                // Track successful connection in Google Analytics
                if (typeof sendGAEvent === 'function') {
                    sendGAEvent('terminal_connection_success', {
                        event_category: 'terminal',
                        event_label: 'webrtc_established',
                        agent_id: AGENT_ID
                    });
                }
                
                break;
            case 'completed':
                console.log('[CLIENT] ✅ ICE connection completed successfully!');
                updateConnectionStatus('connected');
                break;
            case 'failed':
                console.log('[CLIENT] ❌ ICE connection failed - no viable candidates');
                console.log('[CLIENT] 💡 Troubleshooting: This may be due to firewall/NAT issues or blocked STUN servers');
                updateConnectionStatus('disconnected');
                setConnectionMessage('Unable to connect - Agent may be offline', false);
                term.write('\r\n\r\nConnection failed: Network connectivity issues\r\n');
                term.write('This may be due to:\r\n');
                term.write('  • Firewall blocking WebRTC traffic\r\n');
                term.write('  • Corporate network restrictions\r\n');
                term.write('  • STUN/TURN servers unreachable\r\n');
                term.write('  • Agent may have crashed or disconnected\r\n');
                term.write('\r\n🔄 Click Dashboard to return and try another agent\r\n');
                break;
            case 'disconnected':
                console.log('[CLIENT] ⚠️ ICE connection disconnected');
                updateConnectionStatus('disconnected');
                break;
            case 'closed':
                console.log('[CLIENT] 🔐 ICE connection closed');
                updateConnectionStatus('disconnected');
                break;
        }
    };

    peerConnection.onconnectionstatechange = () => {
        console.log(`[CLIENT] 📡 Connection state changed: ${peerConnection.connectionState}`);
        
        switch (peerConnection.connectionState) {
            case 'new':
                console.log('[CLIENT] 🆕 Connection starting...');
                break;
            case 'connecting':
                console.log('[CLIENT] 🔄 Connection in progress...');
                break;
            case 'connected':
                console.log('[CLIENT] ✅ Peer connection fully established!');
                break;
            case 'disconnected':
                console.log('[CLIENT] ⚠️ Peer connection disconnected');
                break;
            case 'failed':
                console.log('[CLIENT] ❌ Peer connection failed completely');
                break;
            case 'closed':
                console.log('[CLIENT] 🔐 Peer connection closed');
                break;
        }
    };

    peerConnection.onicegatheringstatechange = () => {
        console.log(`[CLIENT] 🔍 ICE gathering state changed: ${peerConnection.iceGatheringState}`);
        
        switch (peerConnection.iceGatheringState) {
            case 'new':
                console.log('[CLIENT] 🆕 ICE gathering not started');
                break;
            case 'gathering':
                console.log('[CLIENT] 🔍 ICE gathering in progress...');
                break;
            case 'complete':
                console.log('[CLIENT] ✅ ICE gathering completed');
                break;
        }
    };

    // Client waits for data channel from agent
    peerConnection.ondatachannel = (event) => {
        console.log('[CLIENT] 📨 Data channel received from agent!');
        dataChannel = event.channel;
        setupDataChannel();
    };
}

function setupDataChannel() {
    dataChannel.onopen = () => {
        console.log('[CLIENT] ✅ Data channel is open!');
        term.focus();
        fitAddon.fit();
        // Mac-style connection message with proper colors
        term.write('\r\n\x1b[32mConnected to Mac Terminal\x1b[0m\r\n');
    };

    dataChannel.onmessage = (event) => {
        try {
            const message = JSON.parse(event.data);
            
            // Check if this is a chunked message
            if (chunkAssembler.handleChunkedMessage(message)) {
                // Message was handled by chunk assembler
                return;
            }
            
            // Handle normal messages
            if (message.type === 'output') {
                term.write(message.data);
            } else {
                // Handle session-related messages
                handleSessionMessage(message);
            }
        } catch (err) {
            console.error('[CLIENT] Error parsing data channel message:', err);
        }
    };

    dataChannel.onclose = () => {
        console.log('[CLIENT] Data channel closed.');
        updateConnectionStatus('disconnected');
        term.write('\r\n\r\n\x1b[31mTerminal session ended.\x1b[0m\r\n');
        term.write('Click Dashboard to return and start a new session\r\n');
    };

    dataChannel.onerror = (error) => {
        console.error('[CLIENT] Data channel error:', error);
        updateConnectionStatus('disconnected');
        term.write('\r\n\r\n\x1b[31mData channel error occurred.\x1b[0m\r\n');
        term.write('Click Dashboard to return and try again\r\n');
    };

    // Terminal input and resize are handled by the single module-level
    // registrations (see "One-time terminal event wiring" below), so a
    // renegotiated data channel never stacks duplicate handlers.
}

// One-time terminal event wiring. Registered once at script load instead of
// per connection: the client-hello retry loop can make the agent deliver a
// SECOND data channel, and per-channel term.onData registrations stacked
// additively -> every keystroke sent twice. The handlers below read the
// CURRENT directWs/usingDirectConnection/dataChannel state at event time via
// sendTerminalInput, so they stay correct across renegotiation.
term.onData((data) => {
    sendTerminalInput(data);
});

term.onResize(({ cols, rows }) => {
    if (dataChannel && dataChannel.readyState === 'open') {
        dataChannel.send(JSON.stringify({ type: 'resize', cols, rows }));
    }
});

window.addEventListener('resize', scheduleFit);

// Brief red pulse on the connection status indicator when input cannot be delivered.
// Throttled so held/repeated keys don't stack animations.
let lastInputBlockedFlash = 0;
function flashInputBlocked() {
    const now = Date.now();
    if (now - lastInputBlockedFlash < 500) return;
    lastInputBlockedFlash = now;

    const statusElement = document.getElementById('connection-status');
    if (statusElement && statusElement.animate) {
        statusElement.animate(
            [
                { boxShadow: '0 0 0 0 rgba(255, 68, 68, 0.9)', background: '#ff4444' },
                { boxShadow: '0 0 0 8px rgba(255, 68, 68, 0)', background: '#ff4444' }
            ],
            { duration: 250, iterations: 2 }
        );
    }
}

// Single routing point for terminal input (typed keys and FAB keys).
// Message shapes per transport are fixed by the agent's handlers:
//   - direct local WS requires { type: 'input', sessionId, data }
//   - WebRTC data channel expects { type: 'input', data } (session resolved by clientId)
// The Heroku signaling socket has NO 'input' handler, so we NEVER send input on it.
function sendTerminalInput(data) {
    // Direct local-network connection: ws points at the agent's local WebSocket
    if (usingDirectConnection && ws && ws.readyState === WebSocket.OPEN) {
        if (currentSession) {
            ws.send(JSON.stringify({
                type: 'input',
                sessionId: currentSession.id,
                data: data
            }));
            return true;
        }
    } else if (dataChannel && dataChannel.readyState === 'open') {
        // WebRTC data channel: no sessionId in the message
        dataChannel.send(JSON.stringify({ type: 'input', data }));
        return true;
    }

    // No writable transport (signaling-only or fully offline): drop with feedback.
    flashInputBlocked();
    return false;
}

function sendMessage(message) {
    console.log(`[CLIENT] 📤 Attempting to send message:`, message);
    console.log(`[CLIENT] 🔍 WebSocket state: ${ws ? ws.readyState : 'null'} (OPEN=1)`);
    
    if (!ws) {
        console.error('[CLIENT] ❌ WebSocket is null - cannot send message');
        return false;
    }
    
    if (ws.readyState !== 1) { // WebSocket.OPEN = 1
        const states = ['CONNECTING', 'OPEN', 'CLOSING', 'CLOSED'];
        console.error(`[CLIENT] ❌ WebSocket not open (state: ${ws.readyState} = ${states[ws.readyState] || 'UNKNOWN'}) - cannot send message`);
        return false;
    }
    
    try {
        const messageStr = JSON.stringify(message);
        console.log(`[CLIENT] 📨 Sending message: ${messageStr}`);
        ws.send(messageStr);
        console.log(`[CLIENT] ✅ Message sent successfully`);
        return true;
    } catch (error) {
        console.error(`[CLIENT] ❌ Error sending message:`, error);
        return false;
    }
}

// Session Management Functions
function updateSessionDisplay() {
    const sessionHeader = document.getElementById('session-header');
    if (!sessionHeader) {
        console.warn('[CLIENT] ⚠️ session-header element not found');
        return;
    }

    // Always render tabs (they will show appropriate state even if no sessions)
    console.log('[CLIENT] 📋 Rendering tabs, availableSessions:', availableSessions, 'currentSession:', currentSession);
    renderTabs();

    if (currentSession) {
        console.log('[CLIENT] 📋 Session display updated:', currentSession);
    } else {
        console.log('[CLIENT] 📋 No current session - showing connection state only');
    }
}

// Session tab color palette (fixed colors by creation order)
const SESSION_TAB_COLORS = [
    { bg: '#e3f2fd', border: '#2196f3', text: '#1565c0', muted: '#5a9fd4', ansi: '33;150;243' },  // Blue
    { bg: '#e8f5e9', border: '#4caf50', text: '#2e7d32', muted: '#6fbf73', ansi: '76;175;80' },   // Green
    { bg: '#fff3e0', border: '#ff9800', text: '#e65100', muted: '#ffb74d', ansi: '255;152;0' },   // Orange
    { bg: '#f3e5f5', border: '#9c27b0', text: '#6a1b9a', muted: '#ba68c8', ansi: '156;39;176' },  // Purple
    { bg: '#e0f7fa', border: '#00bcd4', text: '#00838f', muted: '#4dd0e1', ansi: '0;188;212' },   // Teal
    { bg: '#fce4ec', border: '#e91e63', text: '#ad1457', muted: '#f06292', ansi: '233;30;99' },   // Pink
];

// Track color assignments by session ID (persists across renders)
const sessionColorMap = {};
let nextColorIndex = 0;

function getSessionColor(sessionId) {
    if (!sessionColorMap[sessionId]) {
        sessionColorMap[sessionId] = nextColorIndex;
        nextColorIndex = (nextColorIndex + 1) % SESSION_TAB_COLORS.length;
    }
    return SESSION_TAB_COLORS[sessionColorMap[sessionId]];
}

function renderTabs() {
    const tabBar = document.getElementById('session-tab-bar');
    if (!tabBar) {
        console.warn('[CLIENT] ⚠️ session-tab-bar element not found');
        return;
    }

    // Ensure we have sessions to display
    let sessionsToRender = [];

    if (availableSessions && availableSessions.length > 0) {
        // Use availableSessions from agent
        sessionsToRender = availableSessions;
    } else if (currentSession) {
        // Fallback: show at least the current session
        sessionsToRender = [currentSession];
    }

    console.log('[CLIENT] 🎨 Rendering tabs:', {
        sessionCount: sessionsToRender.length,
        currentSession: currentSession?.id,
        source: availableSessions?.length > 0 ? 'agent' : 'fallback'
    });

    // Build tabs HTML
    let tabsHTML = '';

    if (sessionsToRender.length > 0) {
        tabsHTML = sessionsToRender.map(session => {
            const isActive = currentSession && session.id === currentSession.id;
            const displayName = session.name || 'Terminal Session';
            const color = getSessionColor(session.id);

            // Active tabs get full color, inactive tabs get muted version of their color
            const tabStyle = isActive
                ? `background: ${color.bg}; border-color: ${color.border}; border-bottom: 3px solid ${color.border};`
                : `background: rgba(255,255,255,0.05); border-color: transparent; border-bottom: 3px solid ${color.muted}40;`;
            const textStyle = isActive
                ? `color: ${color.text}; font-weight: 600;`
                : `color: ${color.muted};`;

            return `
                <div class="session-tab ${isActive ? 'active' : ''}" style="${tabStyle}; cursor: pointer;" title="${displayName}" data-color-index="${sessionColorMap[session.id]}" onclick="switchToSession('${session.id}')">
                    <span class="session-tab-name" style="${textStyle}">${displayName}</span>
                    <button class="session-tab-close" onclick="event.stopPropagation(); closeSession('${session.id}', event)" title="Close session" style="color: ${isActive ? color.text : color.muted}">×</button>
                </div>
            `;
        }).join('');

        // Add new session button only when we have sessions
        tabsHTML += '<button class="session-tab-new" onclick="createNewSession()" title="New Session">+</button>';
    } else {
        // No sessions - show connection status message
        tabsHTML = `<div style="color: #888; font-size: 0.85rem; padding: 6px 12px;">${connectionStatusMessage}</div>`;
    }

    tabBar.innerHTML = tabsHTML;
    console.log('[CLIENT] ✅ Tabs rendered:', sessionsToRender.length, 'tabs');
}

// Update URL with current session ID so refresh reconnects
function updateUrlWithSession(sessionId) {
    const url = new URL(window.location.href);
    url.searchParams.set('session', sessionId);
    window.history.replaceState({}, '', url.toString());
    console.log('[CLIENT] 📍 URL updated with session:', sessionId);
}

// Close a session with confirmation - shows custom modal
function closeSession(sessionId, event) {
    event.stopPropagation(); // Don't trigger tab switch

    const session = availableSessions.find(s => s.id === sessionId);
    const sessionName = session?.name || 'this session';
    const createdAt = session?.createdAt || null;

    // Get session color for modal
    const sessionColor = getSessionColor(sessionId);

    // Show custom modal instead of browser confirm()
    if (typeof showCloseSessionModal === 'function') {
        showCloseSessionModal(sessionId, sessionName, createdAt, sessionColor);
    } else {
        // Fallback to native confirm if modal not available
        if (confirm(`Close "${sessionName}"?\n\nThis will terminate the terminal session.`)) {
            doCloseSession(sessionId);
        }
    }
}

// Actually close the session (called from modal confirmation)
function doCloseSession(sessionId) {
    console.log('[CLIENT] 🗑️ Closing session:', sessionId);

    // Remove from available sessions
    const remainingSessions = availableSessions.filter(s => s.id !== sessionId);
    const isClosingCurrentSession = currentSession && currentSession.id === sessionId;
    const nextSession = isClosingCurrentSession && remainingSessions.length > 0 ? remainingSessions[0] : null;

    // Send close request to agent (with auto-switch if closing active session)
    const closeMessage = {
        type: 'close_session',
        sessionId: sessionId,
        switchToSessionId: nextSession ? nextSession.id : null  // Atomic close + switch
    };

    // Route like sendTerminalInput: ws is only a valid transport when it is the
    // direct local-network socket. The Heroku signaling socket has no
    // 'close_session' handler and would silently drop the request.
    if (dataChannel && dataChannel.readyState === 'open') {
        dataChannel.send(JSON.stringify(closeMessage));
    } else if (usingDirectConnection && ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(closeMessage));
    } else {
        // Signaling-only or fully offline: fail visibly, keep local state intact
        // so tabs don't desync from the agent.
        console.error('[CLIENT] ❌ Cannot close session - no active connection');
        term.write('\r\n\x1b[31mCannot close session - not connected\x1b[0m\r\n');
        flashInputBlocked();
        return;
    }

    // Update local state
    availableSessions = remainingSessions;

    // If closing current session, update UI immediately
    if (isClosingCurrentSession) {
        // Clear terminal IMMEDIATELY to prevent garbage from closed session
        term.clear();

        if (nextSession) {
            // Update currentSession IMMEDIATELY so renderTabs shows correct active tab
            currentSession = {
                id: nextSession.id,
                name: nextSession.name || 'Terminal Session'
            };
            // Update URL
            updateUrlWithSession(nextSession.id);
            // Note: Agent will send session-switched with buffered output
        } else {
            currentSession = null;
            term.write('\r\n\x1b[33mSession closed. Click + to create a new session.\x1b[0m\r\n');
        }
    }

    renderTabs();
}

function getConnectionStatus() {
    // Check direct WebSocket connection. Only counts when ws is the direct
    // local-network socket; an open Heroku signaling socket is NOT a terminal
    // connection and must not be reported as 'connected'.
    if (usingDirectConnection && ws && ws.readyState === WebSocket.OPEN) {
        return 'connected';
    }

    // Check WebRTC data channel connection
    if (dataChannel && dataChannel.readyState === 'open') {
        return 'connected';
    }

    // Check connecting states
    if (ws && ws.readyState === WebSocket.CONNECTING) {
        return 'connecting';
    }

    if (dataChannel && dataChannel.readyState === 'connecting') {
        return 'connecting';
    }

    // Not connected
    return 'disconnected';
}

function formatLastActivity(timestamp) {
    const now = Date.now();
    const diff = now - timestamp;
    const minutes = Math.floor(diff / 60000);
    const hours = Math.floor(diff / 3600000);
    const days = Math.floor(diff / 86400000);
    
    if (minutes < 1) return 'now';
    if (minutes < 60) return `${minutes}m`;
    if (hours < 24) return `${hours}h`;
    return `${days}d`;
}

function switchToSession(sessionId) {
    if (!dataChannel || dataChannel.readyState !== 'open') {
        console.error('[CLIENT] ❌ Cannot switch session - data channel not open');
        return;
    }

    console.log('[CLIENT] 🔄 Switching to session:', sessionId);
    dataChannel.send(JSON.stringify({
        type: 'session-switch',
        sessionId: sessionId
    }));
}

function createNewSession() {
    console.log('[CLIENT] 🆕 Creating new session...');
    console.log('[CLIENT] Debug - ws:', ws ? ws.readyState : 'null',
                'dataChannel:', dataChannel ? dataChannel.readyState : 'null');

    // PRIORITIZE WebRTC data channel - it has proper SessionManager!
    if (dataChannel && dataChannel.readyState === 'open') {
        console.log('[CLIENT] 📤 Sending session create via WebRTC data channel');
        const message = {
            type: 'session-create',
            cols: term.cols,
            rows: term.rows
        };
        console.log('[CLIENT] Message:', JSON.stringify(message));
        dataChannel.send(JSON.stringify(message));
    } else if (usingDirectConnection && ws && ws.readyState === WebSocket.OPEN) {
        // Only when ws is the direct local socket; the Heroku signaling socket
        // has no 'create_session' handler and would silently drop the request.
        console.log('[CLIENT] 📤 Sending session create via Direct WebSocket');
        const message = {
            type: 'create_session',
            cols: term.cols,
            rows: term.rows
        };
        console.log('[CLIENT] Message:', JSON.stringify(message));
        ws.send(JSON.stringify(message));
    } else {
        console.error('[CLIENT] ❌ Cannot create session - no active connection');
        console.error('[CLIENT] ws:', ws ? ws.readyState : 'null',
                     'dataChannel:', dataChannel ? dataChannel.readyState : 'null');
        term.write('\r\n\x1b[31mCannot create session - not connected\x1b[0m\r\n');
    }
}


// Handle session-related data channel messages
function handleSessionMessage(message) {
    switch (message.type) {
        case 'session-created':
            console.log('[CLIENT] ✅ New session created:', message.sessionId);

            // Update current session
            currentSession = {
                id: message.sessionId,
                name: message.sessionName || 'Terminal Session'
            };

            // Update available sessions list
            if (message.availableSessions) {
                availableSessions = message.availableSessions;
            } else {
                // Add to local list if not provided
                availableSessions.push(currentSession);
            }

            // Clear terminal for new session with session color
            term.clear();
            const sessionColor = getSessionColor(currentSession.id);
            term.write(`\r\n\x1b[38;2;${sessionColor.ansi}m✨ New session created: ${currentSession.name}\x1b[0m\r\n\r\n`);

            // Update URL with session ID so refresh reconnects to same session
            updateUrlWithSession(message.sessionId);

            // Update UI
            updateSessionDisplay();

            // Save to localStorage
            saveSessionToLocalStorage(AGENT_ID, currentSession);

            // Focus terminal for keyboard input
            term.focus();
            break;

        case 'session-switched':
            currentSession = {
                id: message.sessionId,
                name: message.sessionName || 'Terminal Session'
            };
            updateSessionDisplay();
            term.clear(); // Clear terminal for new session
            console.log('[CLIENT] ✅ Switched to session:', currentSession);

            // Update URL so refresh reconnects to this session
            updateUrlWithSession(message.sessionId);

            // Save updated session info
            saveSessionToLocalStorage(AGENT_ID, currentSession);

            // Focus terminal for keyboard input
            term.focus();
            break;
        case 'session-ended':
            // Only show if this is for the current session (ignore closed session messages)
            if (!message.sessionId || (currentSession && message.sessionId === currentSession.id)) {
                term.write(`\r\n\x1b[31mSession ended: ${message.reason}\x1b[0m\r\n`);
                if (message.code) {
                    term.write(`Exit code: ${message.code}\r\n`);
                }
            }
            break;
        case 'session-terminated':
            // Only show if this is for the current session (ignore closed session messages)
            if (!message.sessionId || (currentSession && message.sessionId === currentSession.id)) {
                term.write(`\r\n\x1b[31mSession terminated\x1b[0m\r\n`);
                term.write('Click Dashboard to start a new session\r\n');
            }
            break;
        case 'session-closed':
            console.log('[CLIENT] ✅ Session closed confirmed:', message.sessionId);
            // Update available sessions from server response
            if (message.availableSessions) {
                availableSessions = message.availableSessions;
                renderTabs();
            }
            break;
        case 'error':
            term.write(`\r\n\x1b[31mError: ${message.message}\x1b[0m\r\n`);
            break;
    }
}

// Session storage helper
function saveSessionToLocalStorage(agentId, sessionInfo) {
    try {
        console.log('[CLIENT] 🔍 DEBUG: Saving session to localStorage');
        console.log('[CLIENT] 🔍 DEBUG: AgentID:', agentId);
        console.log('[CLIENT] 🔍 DEBUG: SessionInfo:', sessionInfo);

        const storedSessions = localStorage.getItem('shell-mirror-sessions');
        console.log('[CLIENT] 🔍 DEBUG: Current stored sessions:', storedSessions);

        let sessionData = storedSessions ? JSON.parse(storedSessions) : {};

        if (!sessionData[agentId]) {
            sessionData[agentId] = [];
        }

        // Remove existing session with same ID
        sessionData[agentId] = sessionData[agentId].filter(s => s.id !== sessionInfo.id);

        // Add updated session info
        const sessionToStore = {
            id: sessionInfo.id,
            name: sessionInfo.name,
            lastActivity: Date.now(),
            createdAt: sessionInfo.createdAt || Date.now(),
            status: 'active'
        };

        sessionData[agentId].push(sessionToStore);

        localStorage.setItem('shell-mirror-sessions', JSON.stringify(sessionData));
        console.log('[CLIENT] 💾 Session saved to storage:', sessionToStore);

        // Broadcast to dashboard tabs for instant sync
        try {
            const broadcast = new BroadcastChannel('shell-mirror-sessions');
            broadcast.postMessage({
                type: 'session-update',
                agentId: agentId,
                sessions: sessionData[agentId]
            });
            broadcast.close();
            console.log('[CLIENT] 📡 Session update broadcasted to dashboard');
        } catch (e) {
            // BroadcastChannel not supported, localStorage event will handle it
        }
    } catch (error) {
        console.error('[CLIENT] ❌ Error saving session to storage:', error);
    }
}

// ========================================
// Floating Buttons - Mobile CLI Shortcuts
// ========================================

const fabKeyMap = {
    'shift+tab': '\x1b[Z',   // Shift+Tab - MODE SWITCHING (most critical!)
    'tab': '\t',             // Tab - autocomplete + thinking toggle
    'escape': '\x1b',        // Esc - cancel (double = rewind)
    'enter': '\r',           // ⏎ - confirm/submit (menu-driven TUIs)
    'alt+enter': '\x1b\r',   // ⌥⏎ - newline without submit (multiline prompts)
    'up': '\x1b[A',          // ↑ - history up
    'down': '\x1b[B',        // ↓ - history down
    'left': '\x1b[D',        // ← - cursor left
    'right': '\x1b[C',       // → - cursor right
    'ctrl+c': '\x03',        // ^C - interrupt
    'ctrl+d': '\x04',        // ^D - EOF/exit
    'ctrl+l': '\x0c',        // ^L - clear screen
};

function initFloatingButtons() {
    const container = document.getElementById('floating-buttons');
    const toggle = document.getElementById('fabToggle');
    const strip = document.getElementById('fabStrip');
    const scrollLeft = document.getElementById('fabScrollLeft');
    const scrollRight = document.getElementById('fabScrollRight');

    if (!container || !toggle || !strip) {
        console.log('[CLIENT] ⌨️ Floating buttons not found in DOM');
        return;
    }

    console.log('[CLIENT] ⌨️ Initializing floating buttons for mobile CLI shortcuts');

    // Toggle collapse/expand - preserve focus
    const handleToggle = (e) => {
        e.preventDefault();
        container.classList.toggle('collapsed');
        localStorage.setItem('fab-collapsed', container.classList.contains('collapsed'));
        setTimeout(() => term.focus(), 0);
    };
    toggle.addEventListener('touchstart', handleToggle, { passive: false });
    toggle.addEventListener('click', handleToggle);

    // Restore saved collapsed state
    if (localStorage.getItem('fab-collapsed') === 'true') {
        container.classList.add('collapsed');
    }

    // Scroll indicator click handlers - preserve focus
    const scrollAmount = 150; // pixels per click

    if (scrollLeft) {
        const handleScrollLeft = (e) => {
            e.preventDefault();
            strip.scrollBy({ left: -scrollAmount, behavior: 'smooth' });
            setTimeout(() => term.focus(), 0);
        };
        scrollLeft.addEventListener('touchstart', handleScrollLeft, { passive: false });
        scrollLeft.addEventListener('click', handleScrollLeft);
    }

    if (scrollRight) {
        const handleScrollRight = (e) => {
            e.preventDefault();
            strip.scrollBy({ left: scrollAmount, behavior: 'smooth' });
            setTimeout(() => term.focus(), 0);
        };
        scrollRight.addEventListener('touchstart', handleScrollRight, { passive: false });
        scrollRight.addEventListener('click', handleScrollRight);
    }

    // Update scroll indicator visibility based on scroll position
    function updateScrollIndicators() {
        if (!scrollLeft || !scrollRight) return;

        const atStart = strip.scrollLeft <= 5;
        const atEnd = strip.scrollLeft >= strip.scrollWidth - strip.clientWidth - 5;

        scrollLeft.classList.toggle('hidden', atStart);
        scrollRight.classList.toggle('hidden', atEnd);
    }

    strip.addEventListener('scroll', updateScrollIndicators);

    // Button click handlers - send key sequences to terminal
    // Use touchstart with preventDefault to prevent focus loss on mobile
    document.querySelectorAll('.fab-btn[data-keys]:not([data-repeat])').forEach(btn => {
        // Touch handler (mobile) - prevent focus loss
        btn.addEventListener('touchstart', (e) => {
            e.preventDefault(); // Prevents focus shift and keyboard dismiss
            const keys = btn.dataset.keys;
            if (keys && fabKeyMap[keys]) {
                sendFabKey(fabKeyMap[keys]);
            }
            // Re-focus terminal after a tiny delay to ensure keyboard stays up
            setTimeout(() => term.focus(), 0);
        }, { passive: false });

        // Click handler (desktop fallback)
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            const keys = btn.dataset.keys;
            if (keys && fabKeyMap[keys]) {
                sendFabKey(fabKeyMap[keys]);
                term.focus();
            }
        });
    });

    // Paste button - reads the clipboard and routes it through the shared input path.
    // navigator.clipboard.readText() MUST be invoked synchronously inside the
    // user-gesture handler (iOS Safari rejects it otherwise); only the .then/.catch
    // runs later. Failures (permission denied, empty clipboard) show a brief
    // non-blocking notice instead of throwing.
    const pasteBtn = document.getElementById('fabPaste');
    if (pasteBtn) {
        const handlePaste = (e) => {
            e.preventDefault(); // Prevents focus shift and keyboard dismiss
            if (!navigator.clipboard || !navigator.clipboard.readText) {
                flashFabNotice('Clipboard unavailable');
            } else {
                navigator.clipboard.readText()
                    .then(text => {
                        if (text) {
                            sendTerminalInput(text);
                        } else {
                            flashFabNotice('Clipboard is empty');
                        }
                    })
                    .catch(() => {
                        flashFabNotice('Paste not allowed');
                    });
            }
            setTimeout(() => term.focus(), 0);
        };
        pasteBtn.addEventListener('touchstart', handlePaste, { passive: false });
        pasteBtn.addEventListener('click', handlePaste);
    }

    // Long-press repeat for arrow keys
    initFabLongPress();

    // Drive body padding-bottom from the visualViewport keyboard inset so the
    // flex column shrinks when the iOS keyboard opens. The FAB is a flex child
    // and rides up with the column — no fixed-positioning, no transform.
    initKeyboardInsetTracking();

    // Scroll to show primary buttons (⇧Tab visible on left). The container is
    // display:none until startConnection() adds .show, so retry until the strip
    // has layout; position is measured strip-relative so the toggle/chevron
    // widths don't skew it.
    const scrollToPrimaryGroup = (attemptsLeft) => {
        const modeBtn = strip.querySelector('[data-keys="shift+tab"]');
        if (modeBtn && strip.clientWidth > 0) {
            strip.scrollLeft += modeBtn.getBoundingClientRect().left
                - strip.getBoundingClientRect().left - 10;
            updateScrollIndicators();
        } else if (attemptsLeft > 0) {
            setTimeout(() => scrollToPrimaryGroup(attemptsLeft - 1), 100);
        }
    };
    setTimeout(() => scrollToPrimaryGroup(50), 100);

    console.log('[CLIENT] ⌨️ Floating buttons initialized');
}

// rAF-throttled fit so window.resize, visualViewport, and orientationchange
// don't trigger duplicate measurement passes within the same frame.
let fitPending = false;
function scheduleFit() {
    if (fitPending) return;
    fitPending = true;
    requestAnimationFrame(() => {
        fitPending = false;
        try { fitAddon.fit(); } catch (_) { /* container may be 0×0 mid-bootstrap */ }
    });
}

// Track on-screen keyboard size via visualViewport, expose it as
// --keyboard-inset on <html> so body { padding-bottom } shrinks the flex column.
function initKeyboardInsetTracking() {
    const vv = window.visualViewport;
    if (!vv) return; // older browsers — body stays full-height (acceptable on desktop)

    let rafPending = false;
    const update = () => {
        rafPending = false;
        const inset = Math.max(0, window.innerHeight - (vv.height + vv.offsetTop));
        document.documentElement.style.setProperty('--keyboard-inset', `${inset}px`);
        scheduleFit();
    };
    const schedule = () => {
        if (rafPending) return;
        rafPending = true;
        requestAnimationFrame(update);
    };

    vv.addEventListener('resize', schedule);
    vv.addEventListener('scroll', schedule); // QuickType bar slide-in fires scroll on some iOS builds
    window.addEventListener('orientationchange', schedule);
    update(); // initial state
}

// Brief transient notice above the FAB strip (e.g. clipboard paste failures).
let fabNoticeTimer = null;
function flashFabNotice(message) {
    const notice = document.getElementById('fabNotice');
    if (!notice) return;
    notice.textContent = message;
    notice.classList.add('visible');
    if (fabNoticeTimer) clearTimeout(fabNoticeTimer);
    fabNoticeTimer = setTimeout(() => {
        notice.classList.remove('visible');
        fabNoticeTimer = null;
    }, 1600);
}

// Send key sequence through the appropriate connection
function sendFabKey(keySequence) {
    // Routed through the single input path; never falls back to the signaling socket.
    sendTerminalInput(keySequence);
}

// Long-press support for arrow buttons (↑/↓)
function initFabLongPress() {
    document.querySelectorAll('.fab-btn[data-repeat="true"]').forEach(btn => {
        let interval = null;
        let timeout = null;

        const startRepeat = () => {
            const keys = btn.dataset.keys;
            if (!keys || !fabKeyMap[keys]) return;

            // First key immediately
            sendFabKey(fabKeyMap[keys]);

            // Then repeat after delay
            timeout = setTimeout(() => {
                interval = setInterval(() => {
                    sendFabKey(fabKeyMap[keys]);
                }, 100); // Repeat every 100ms
            }, 300); // Start repeating after 300ms hold
        };

        const stopRepeat = () => {
            if (timeout) clearTimeout(timeout);
            if (interval) clearInterval(interval);
            timeout = null;
            interval = null;
            // Re-focus terminal when touch ends
            setTimeout(() => term.focus(), 0);
        };

        // Touch events for mobile - prevent focus loss
        btn.addEventListener('touchstart', (e) => {
            e.preventDefault(); // Prevents focus shift and keyboard dismiss
            startRepeat();
        }, { passive: false });
        btn.addEventListener('touchend', stopRepeat);
        btn.addEventListener('touchcancel', stopRepeat);

        // Mouse events for testing on desktop
        btn.addEventListener('mousedown', startRepeat);
        btn.addEventListener('mouseup', stopRepeat);
        btn.addEventListener('mouseleave', stopRepeat);
    });
}

// Initialize floating buttons when DOM is ready
document.addEventListener('DOMContentLoaded', initFloatingButtons);